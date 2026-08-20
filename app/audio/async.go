package audio

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// AsyncSource decouples a heavy SampleSource (such as the synthesizer, whose
// ReadInterleaved runs a full mix and allocates) from a realtime device
// callback. A background goroutine pulls from the inner source into a ring
// buffer; the device callback (ReadInterleaved) only copies already-produced
// samples out, never running synthesis, allocating, or blocking. When the ring
// underruns the callback emits silence instead of stalling, so it can never miss
// its period deadline and glitch.
//
// The ring's capacity bounds the jitter the producer can absorb; a separate,
// smaller target fill bounds the steady-state latency added. The producer tops
// the ring up to the target and no further, so the spare capacity above the
// target absorbs bursts without that headroom turning into permanent latency.
type AsyncSource struct {
	inner       SampleSource
	channels    int
	blockFrames int
	target      int // steady-state fill cap in samples; producer tops up to here, not capacity

	mu      sync.Mutex
	notFull *sync.Cond // producer waits here for the consumer to free space
	ring    []float32
	rpos    int // read offset into ring (sample index)
	count   int // samples currently buffered
	closed  bool

	scratch []float32     // producer synthesis buffer (not shared with consumer)
	done    chan struct{} // closed when the producer goroutine has exited

	// rtReady is closed once the producer has finished its scheduling request,
	// whether that request succeeded, failed, or was disabled. It lets the
	// caller report the outcome at startup instead of guessing when the
	// producer goroutine has got that far.
	rtReady chan struct{}

	// rt is the scheduling request applied by the producer to its own thread.
	// Read only by the producer goroutine, and only before its first block.
	rt RealtimeConfig

	// idle is an optional callback set by SetIdleCheck. When it returns true the
	// producer skips synthesis and emits silence, so it is only read by the
	// producer goroutine.
	idle func() bool

	// Diagnostic counters (atomic — safe to read from any goroutine without mu).
	// underruns counts how many times ReadInterleaved had to zero-pad because the
	// ring did not contain enough samples. A non-zero value indicates the producer
	// is falling behind the device callback timing.
	underruns atomic.Int64

	// underrunSamples accumulates the total number of silence-padded samples.
	underrunSamples atomic.Int64

	// producerWaits counts how many times the producer blocked because the ring
	// reached its target fill (it is caught up). This is normal in steady state.
	producerWaits atomic.Int64

	// lastUnderrun records the time of the most recent underrun event for
	// recency awareness. Updated atomically; zero means never.
	lastUnderrun atomic.Int64 // Unix nanoseconds

	// framesRead accumulates the total frames the device callback has pulled
	// (including silence-padded frames on underrun). Because the callback runs on
	// the soundcard clock, this is the playback-time reference the drift monitor
	// compares against the telemetry sequence clock.
	framesRead atomic.Int64

	// minFill records the shallowest the ring was ever left after a callback, in
	// samples. This is the safety margin before an underrun. Unlike the underrun
	// counter it is sampled on every callback and moves long before any audio is
	// lost, so it is the metric to compare when tuning producer scheduling.
	// Seeded to the ring capacity by NewAsyncSource.
	minFill atomic.Int64

	// fillBuckets is a histogram of post-callback fill ratio in eighths, so
	// bucket 0 counts callbacks that left the ring under 1/8 full. It gives the
	// shape of the margin distribution, not only the single worst point.
	fillBuckets [fillBucketCount]atomic.Int64

	// realtime records the outcome of the producer thread's scheduling request.
	// Written once by the producer before its first block, read by any goroutine.
	rtApplied  atomic.Bool
	rtPriority atomic.Int64
	rtErr      atomic.Value // string; empty when applied or when disabled

	// rtPinnedCPU is the CPU the producer pinned itself to, or -1 when it runs
	// on every core. A refused pin is normal on an unprovisioned machine.
	rtPinnedCPU atomic.Int64
	rtPinNote   atomic.Value // string; why the pin was refused, empty otherwise
}

// fillBucketCount is the resolution of the ring fill histogram: eighths.
const fillBucketCount = 8

// errRealtimeUnsupported reports that the platform offers no realtime
// scheduling control. The producer treats this as a quiet, expected outcome
// rather than a fault, so development on macOS and Windows logs nothing.
var errRealtimeUnsupported = errors.New("realtime scheduling is only supported on linux")

// errCPUNotIsolated reports that the kernel did not reserve the requested CPU.
// This is the normal state on a machine without the isolcpus provisioning
// documented in doc/realtime_tuning.md, so it is quiet like
// errRealtimeUnsupported. Pinning to a shared core would cost the producer
// every other core and gain nothing.
var errCPUNotIsolated = errors.New("requested cpu is not isolated by the kernel")

// HealthMetrics holds a snapshot of async source diagnostic counters.
type HealthMetrics struct {
	Underruns        int64   // number of callback invocations that had to zero-pad
	UnderrunSamples  int64   // total silence-padded samples produced
	ProducerWaits    int64   // number of times the producer blocked on a full ring
	RingCapacity     int     // total sample slots in the ring
	RingUsed         int     // sample slots currently filled
	FillRatio        float64 // RingUsed / RingCapacity (0..1)
	FramesRead       int64   // total frames pulled by the device callback (soundcard-clock reference)
	LastUnderrunTime time.Time

	// MinFill is the low-water mark in samples since the last ResetPeak. Compare
	// this, not Underruns, when judging a producer scheduling change.
	MinFill int

	// MinFillRatio is MinFill / RingCapacity (0..1).
	MinFillRatio float64

	// FillBuckets counts callbacks by post-callback fill ratio in eighths.
	FillBuckets [fillBucketCount]int64

	// RealtimeApplied reports whether the producer thread holds a realtime
	// scheduling policy. RealtimeError carries the reason when it does not.
	RealtimeApplied  bool
	RealtimePriority int
	RealtimeError    string

	// RealtimePinnedCPU is the CPU the producer is pinned to, or -1 when it is
	// free to run on any core. RealtimePinNote says why a requested pin was
	// refused, which is the normal state without isolcpus.
	RealtimePinnedCPU int
	RealtimePinNote   string
}

// RealtimeConfig requests operating-system scheduling privileges for the
// producer thread. The zero value asks for nothing, which is what every
// non-realtime caller (offline capture, tests) wants.
type RealtimeConfig struct {
	// Priority is the SCHED_FIFO priority to request. Zero leaves the producer
	// at the default policy. Keep this low: the device callback thread and the
	// kernel interrupt threads must both stay above the producer, or the
	// soundcard clock loses to synthesis work.
	Priority int

	// PinCPU restricts the producer thread to one CPU, for use with an isolcpus
	// kernel command line. Zero means no pinning — CPU 0 is never a sensible
	// isolation target because most interrupts land there.
	PinCPU int
}

// NewAsyncSource wraps inner with a background producer and returns a source
// suitable for handing to a realtime Sink. capacityFrames is the ring depth
// (jitter headroom), targetFrames is the steady-state fill the producer maintains
// (the latency added), and blockFrames is how many frames the producer
// synthesises per iteration. targetFrames is clamped to leave a block of room
// below capacity; a non-positive value fills as close to capacity as possible
// (the legacy "kept full" behaviour). rt requests realtime scheduling for the
// producer thread; a failed request is reported through Health, never fatal.
// The producer goroutine starts immediately.
func NewAsyncSource(
	inner SampleSource,
	channels, capacityFrames, targetFrames, blockFrames int,
	realtime RealtimeConfig,
) *AsyncSource {
	if blockFrames <= 0 {
		blockFrames = pullBlockFrames
	}

	if capacityFrames < blockFrames*2 {
		capacityFrames = blockFrames * 2
	}

	// The producer never fills beyond the target, and a whole block must always
	// fit above it, so cap the target a block below capacity.
	maxTarget := capacityFrames - blockFrames
	if targetFrames <= 0 || targetFrames > maxTarget {
		targetFrames = maxTarget
	}

	source := &AsyncSource{
		inner:       inner,
		channels:    channels,
		blockFrames: blockFrames,
		target:      targetFrames * channels,
		ring:        make([]float32, capacityFrames*channels),
		scratch:     make([]float32, blockFrames*channels),
		done:        make(chan struct{}),
		rtReady:     make(chan struct{}),
		rt:          realtime,
	}
	source.notFull = sync.NewCond(&source.mu)

	// Seed the low-water mark at capacity so the first callback can only lower it.
	source.minFill.Store(int64(len(source.ring)))
	source.rtErr.Store("")
	source.rtPinnedCPU.Store(-1)
	source.rtPinNote.Store("")

	// Pre-fill the ring with `target` frames of silence (not the whole ring): the
	// device callback has something to play immediately, and the producer is not
	// pulled until the consumer drains below the target — so synthesis does not
	// start (draining the upstream mixer buffers) before any haptic data exists.
	// Holding only the target, rather than brim-full, is what keeps the
	// steady-state latency low while the spare capacity still absorbs bursts. The
	// ring is already zeroed by make.
	source.count = source.target

	go source.produce()

	return source
}

// SetIdleCheck registers fn as the idle predicate for the producer. When fn
// returns true the producer skips synthesis and emits silence into the ring,
// eliminating the mix/resample cost while the consumer continues to play
// (silence). Safe to set once before or after Start; fn is only read by the
// producer goroutine.
func (a *AsyncSource) SetIdleCheck(fn func() bool) {
	a.idle = fn
}

// Close stops the producer goroutine and waits for it to exit. It must be called
// after the consuming Sink has stopped pulling.
func (a *AsyncSource) Close() {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	a.notFull.Broadcast()

	<-a.done
}

// ReadInterleaved copies buffered samples to out, padding with silence on
// underrun. It performs no synthesis or allocation and never blocks, so it is
// safe on a realtime audio callback.
func (a *AsyncSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	a.mu.Lock()

	sampleCount := min(len(out), a.count)

	for i := 0; i < sampleCount; {
		c := copy(out[i:sampleCount], a.ring[a.rpos:])
		a.rpos = (a.rpos + c) % len(a.ring)
		i += c
	}

	a.count -= sampleCount
	remaining := a.count
	a.mu.Unlock()

	a.notFull.Signal()

	// Record how much margin was left. This runs off-lock on atomics only, so it
	// adds no contention with the producer.
	a.recordFill(remaining)

	// Underrun: ring was shallow so the consumer had to emit silence.
	// Record diagnostics atomically — no allocation, no lock on the hot path.
	if sampleCount < len(out) {
		gaps := int64(len(out) - sampleCount)

		a.underruns.Add(1)
		a.underrunSamples.Add(gaps)
		a.lastUnderrun.Store(time.Now().UnixNano())
	}

	for i := sampleCount; i < len(out); i++ {
		out[i] = 0
	}

	frames := len(out) / channels
	a.framesRead.Add(int64(frames))

	return frames, true
}

// AwaitRealtime blocks until the producer has finished its scheduling request,
// or until timeout elapses. It reports whether the request completed in time.
// Call it before reading the realtime fields of Health at startup: the producer
// applies the policy on its own thread, so the result is not ready the instant
// NewAsyncSource returns.
func (a *AsyncSource) AwaitRealtime(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-a.rtReady:
		return true
	case <-timer.C:
		return false
	}
}

// ResetPeak clears the low-water mark and the fill histogram so a measurement
// run starts from a clean baseline. It does not clear the cumulative underrun
// counters.
func (a *AsyncSource) ResetPeak() {
	a.minFill.Store(int64(len(a.ring)))

	for i := range a.fillBuckets {
		a.fillBuckets[i].Store(0)
	}
}

// Health returns a snapshot of diagnostic counters. Safe to call from any
// goroutine; does not hold mu while reading the atomics so values are eventually
// consistent but the call never blocks the realtime path.
func (a *AsyncSource) Health() HealthMetrics {
	a.mu.Lock()
	used := a.count
	capacity := len(a.ring)
	a.mu.Unlock()

	lastNS := a.lastUnderrun.Load()
	minFill := int(a.minFill.Load())

	var buckets [fillBucketCount]int64
	for i := range a.fillBuckets {
		buckets[i] = a.fillBuckets[i].Load()
	}

	rtErr, _ := a.rtErr.Load().(string)
	rtPinNote, _ := a.rtPinNote.Load().(string)

	return HealthMetrics{
		Underruns:       a.underruns.Load(),
		UnderrunSamples: a.underrunSamples.Load(),
		ProducerWaits:   a.producerWaits.Load(),
		RingCapacity:    capacity,
		RingUsed:        used,
		FillRatio:       float64(used) / float64(capacity),
		FramesRead:      a.framesRead.Load(),
		LastUnderrunTime: func() time.Time {
			if lastNS == 0 {
				return time.Time{}
			}

			return time.Unix(0, lastNS)
		}(),
		MinFill:           minFill,
		MinFillRatio:      float64(minFill) / float64(capacity),
		FillBuckets:       buckets,
		RealtimeApplied:   a.rtApplied.Load(),
		RealtimePriority:  int(a.rtPriority.Load()),
		RealtimeError:     rtErr,
		RealtimePinnedCPU: int(a.rtPinnedCPU.Load()),
		RealtimePinNote:   rtPinNote,
	}
}

// recordFill updates the low-water mark and the fill histogram from the sample
// count left in the ring after a callback. It touches atomics only, never
// allocates, and never blocks, so it is safe on the realtime callback thread.
func (a *AsyncSource) recordFill(remaining int) {
	fill := int64(remaining)

	// Lower the low-water mark. The loop retries only when another callback
	// lowered it concurrently, which cannot happen on a single device thread.
	for {
		low := a.minFill.Load()
		if fill >= low {
			break
		}

		if a.minFill.CompareAndSwap(low, fill) {
			break
		}
	}

	bucket := int(fill * fillBucketCount / int64(len(a.ring)))
	if bucket >= fillBucketCount {
		bucket = fillBucketCount - 1
	}

	a.fillBuckets[bucket].Add(1)
}

// produce runs in its own goroutine, synthesising blocks off-lock and copying
// them into the ring, blocking only once the ring has reached its target fill.
func (a *AsyncSource) produce() {
	defer close(a.done)

	// The scheduling policy and the CPU affinity belong to an OS thread, not to
	// a goroutine, so the producer must own its thread for the whole loop. The
	// cond-var wait below parks the goroutine without releasing the thread, so
	// the policy survives every iteration.
	runtime.LockOSThread()

	defer runtime.UnlockOSThread()

	a.initRealtime()

	for {
		// Wait until the buffered fill drops below the target, then synthesise one
		// block off-lock. The producer tops the ring back up to the target but no
		// further, so the steady-state latency is the target depth, not capacity.
		a.mu.Lock()

		waited := false
		for a.count >= a.target && !a.closed {
			if !waited {
				waited = true

				a.producerWaits.Add(1)
			}

			a.notFull.Wait()
		}

		if a.closed {
			a.mu.Unlock()

			return
		}

		a.mu.Unlock()

		// When the idle predicate is set and returns true, skip synthesis and
		// fill the ring with silence. The consumer continues playing (silence)
		// and the loop stays paced by notFull exactly as in the synthesis path.
		if a.idle != nil && a.idle() {
			if a.produceSilence() {
				return
			}

			continue
		}

		if done := a.produceSynth(); done {
			return
		}
	}
}

// initRealtime applies the configured scheduling request to the producer's own
// thread and records the outcome for Health. It never returns an error: a
// machine without the privilege must still play audio, only at normal priority.
func (a *AsyncSource) initRealtime() {
	// Always signal completion, so a caller waiting on the outcome is never
	// blocked by a disabled or failed request.
	defer close(a.rtReady)

	if a.rt.Priority <= 0 {
		return
	}

	err := applyRealtime(a.rt.Priority)
	if err != nil {
		a.recordRealtimeErr(err)

		return
	}

	a.rtApplied.Store(true)
	a.rtPriority.Store(int64(a.rt.Priority))

	// Pinning is a refinement of the policy, so a failure here leaves the
	// thread realtime but unpinned. Report it without clearing rtApplied.
	if a.rt.PinCPU > 0 {
		err := pinThread(a.rt.PinCPU)
		if err != nil {
			a.rtPinNote.Store(err.Error())
			a.recordRealtimeErr(err)

			return
		}

		a.rtPinnedCPU.Store(int64(a.rt.PinCPU))
	}
}

// recordRealtimeErr stores err for Health, unless it is one of the expected
// outcomes that need no operator attention.
func (a *AsyncSource) recordRealtimeErr(err error) {
	if errors.Is(err, errRealtimeUnsupported) || errors.Is(err, errCPUNotIsolated) {
		return
	}

	a.rtErr.Store(err.Error())
}

// produceSilence writes a block of silence into the ring without calling the
// inner source. It returns true if the producer should exit (ring was closed).
func (a *AsyncSource) produceSilence() bool {
	silence := a.scratch[:a.blockFrames*a.channels]
	for i := range silence {
		silence[i] = 0
	}

	a.mu.Lock()
	a.writeRing(silence)
	closed := a.closed
	a.mu.Unlock()

	return closed
}

// produceSynth pulls one block from the inner source and writes it into the
// ring. It returns true if the producer should exit (source ended or ring was
// closed).
func (a *AsyncSource) produceSynth() bool {
	frames, readOk := a.inner.ReadInterleaved(a.scratch, a.channels)

	// The producer only writes when count was below the target, and capacity keeps
	// a full block of room above the target, so the block always fits; the
	// consumer only frees more space between the unlock above and the lock below.
	a.mu.Lock()
	a.writeRing(a.scratch[:frames*a.channels])
	closed := a.closed
	a.mu.Unlock()

	return !readOk || closed
}

// writeRing copies samples into the ring at the write cursor. Caller holds mu and
// must have ensured the samples fit.
func (a *AsyncSource) writeRing(samples []float32) {
	wpos := (a.rpos + a.count) % len(a.ring)
	for len(samples) > 0 {
		n := copy(a.ring[wpos:], samples)
		samples = samples[n:]
		a.count += n
		wpos = (wpos + n) % len(a.ring)
	}
}
