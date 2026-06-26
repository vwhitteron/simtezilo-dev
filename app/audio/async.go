package audio

import (
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

	// idle is an optional callback set by SetIdleCheck. When it returns true the
	// producer skips synthesis and emits silence, so it is only read by the
	// producer goroutine.
	idle func() bool

	// Diagnostic counters (atomic — safe to read from any goroutine without mu).
	// gapFills counts how many times ReadInterleaved had to zero-pad because the
	// ring did not contain enough samples. A non-zero value indicates the producer
	// is falling behind the device callback timing.
	gapFills atomic.Int64

	// gapFillSamples accumulates the total number of silence-padded samples.
	gapFillSamples atomic.Int64

	// producerWaits counts how many times the producer blocked because the ring
	// reached its target fill (it is caught up). This is normal in steady state.
	producerWaits atomic.Int64

	// lastGapFill records the time of the most recent gap-fill event for
	// recency awareness. Updated atomically; zero means never.
	lastGapFill atomic.Int64 // Unix nanoseconds

	// framesRead accumulates the total frames the device callback has pulled
	// (including silence-padded frames on underrun). Because the callback runs on
	// the soundcard clock, this is the playback-time reference the drift monitor
	// compares against the telemetry sequence clock.
	framesRead atomic.Int64
}

// NewAsyncSource wraps inner with a background producer and returns a source
// suitable for handing to a realtime Sink. capacityFrames is the ring depth
// (jitter headroom), targetFrames is the steady-state fill the producer maintains
// (the latency added), and blockFrames is how many frames the producer
// synthesises per iteration. targetFrames is clamped to leave a block of room
// below capacity; a non-positive value fills as close to capacity as possible
// (the legacy "kept full" behaviour). The producer goroutine starts immediately.
func NewAsyncSource(inner SampleSource, channels, capacityFrames, targetFrames, blockFrames int) *AsyncSource {
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
	}
	source.notFull = sync.NewCond(&source.mu)

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
	a.mu.Unlock()

	a.notFull.Signal()

	// Gap-fill: ring was shallow so the consumer had to emit silence.
	// Record diagnostics atomically — no allocation, no lock on the hot path.
	if sampleCount < len(out) {
		gaps := int64(len(out) - sampleCount)

		a.gapFills.Add(1)
		a.gapFillSamples.Add(gaps)
		a.lastGapFill.Store(time.Now().UnixNano())
	}

	for i := sampleCount; i < len(out); i++ {
		out[i] = 0
	}

	frames := len(out) / channels
	a.framesRead.Add(int64(frames))

	return frames, true
}

// produce runs in its own goroutine, synthesising blocks off-lock and copying
// them into the ring, blocking only once the ring has reached its target fill.
func (a *AsyncSource) produce() {
	defer close(a.done)

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

// HealthMetrics holds a snapshot of async source diagnostic counters.
type HealthMetrics struct {
	GapFills        int64   // number of callback invocations that had to zero-pad
	GapFillSamples  int64   // total silence-padded samples produced
	ProducerWaits   int64   // number of times the producer blocked on a full ring
	RingCapacity    int     // total sample slots in the ring
	RingUsed        int     // sample slots currently filled
	FillRatio       float64 // RingUsed / RingCapacity (0..1)
	FramesRead      int64   // total frames pulled by the device callback (soundcard-clock reference)
	LastGapFillTime time.Time
}

// Health returns a snapshot of diagnostic counters. Safe to call from any
// goroutine; does not hold mu while reading the atomics so values are eventually
// consistent but the call never blocks the realtime path.
func (a *AsyncSource) Health() HealthMetrics {
	a.mu.Lock()
	used := a.count
	capacity := len(a.ring)
	a.mu.Unlock()

	lastNS := a.lastGapFill.Load()

	return HealthMetrics{
		GapFills:       a.gapFills.Load(),
		GapFillSamples: a.gapFillSamples.Load(),
		ProducerWaits:  a.producerWaits.Load(),
		RingCapacity:   capacity,
		RingUsed:       used,
		FillRatio:      float64(used) / float64(capacity),
		FramesRead:     a.framesRead.Load(),
		LastGapFillTime: func() time.Time {
			if lastNS == 0 {
				return time.Time{}
			}

			return time.Unix(0, lastNS)
		}(),
	}
}
