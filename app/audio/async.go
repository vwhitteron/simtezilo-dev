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
// The ring's capacity bounds both the jitter the producer can absorb and the
// steady-state latency added (the producer keeps the ring full). Size it from
// the device latency so a lower latency setting yields a shallower buffer.
type AsyncSource struct {
	inner       SampleSource
	channels    int
	blockFrames int

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

	// producerWaits counts how many times the producer blocked on a full ring.
	producerWaits atomic.Int64

	// lastGapFill records the time of the most recent gap-fill event for
	// recency awareness. Updated atomically; zero means never.
	lastGapFill atomic.Int64 // Unix nanoseconds
}

// NewAsyncSource wraps inner with a background producer and returns a source
// suitable for handing to a realtime Sink. capacityFrames is the ring depth and
// blockFrames is how many frames the producer synthesises per iteration. The
// producer goroutine starts immediately.
func NewAsyncSource(inner SampleSource, channels, capacityFrames, blockFrames int) *AsyncSource {
	if blockFrames <= 0 {
		blockFrames = pullBlockFrames
	}

	if capacityFrames < blockFrames*2 {
		capacityFrames = blockFrames * 2
	}

	source := &AsyncSource{
		inner:       inner,
		channels:    channels,
		blockFrames: blockFrames,
		ring:        make([]float32, capacityFrames*channels),
		scratch:     make([]float32, blockFrames*channels),
		done:        make(chan struct{}),
	}
	source.notFull = sync.NewCond(&source.mu)

	// Pre-fill the ring with silence so the device callback has something to
	// play immediately and, crucially, the producer is not pulled until the ring
	// drains. Pulling the synth to fill the ring at startup would drain the
	// upstream mixer buffers before any haptic data exists — a burst of underruns
	// heard as clicks on the first audio. The ring is already zeroed by make.
	source.count = len(source.ring)

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

	return len(out) / channels, true
}

// freeFrames reports how many input frames can still be written. Caller holds mu.
func (a *AsyncSource) freeFrames() int {
	return (len(a.ring) - a.count) / a.channels
}

// produce runs in its own goroutine, synthesising blocks off-lock and copying
// them into the ring, blocking only when the ring is full.
func (a *AsyncSource) produce() {
	defer close(a.done)

	for {
		// Wait until there is room for a full block, then synthesise off-lock.
		a.mu.Lock()

		waited := false
		for a.freeFrames() < a.blockFrames && !a.closed {
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

	// The consumer only removes samples between the unlock above and the
	// lock below, so the free space measured above is a safe lower bound and
	// the synthesised block always fits.
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
		LastGapFillTime: func() time.Time {
			if lastNS == 0 {
				return time.Time{}
			}

			return time.Unix(0, lastNS)
		}(),
	}
}
