package audio

import "sync"

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

	a := &AsyncSource{
		inner:       inner,
		channels:    channels,
		blockFrames: blockFrames,
		ring:        make([]float32, capacityFrames*channels),
		scratch:     make([]float32, blockFrames*channels),
		done:        make(chan struct{}),
	}
	a.notFull = sync.NewCond(&a.mu)

	// Pre-fill the ring with silence so the device callback has something to
	// play immediately and, crucially, the producer is not pulled until the ring
	// drains. Pulling the synth to fill the ring at startup would drain the
	// upstream mixer buffers before any haptic data exists — a burst of underruns
	// heard as clicks on the first audio. The ring is already zeroed by make.
	a.count = len(a.ring)

	go a.produce()

	return a
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
		for a.freeFrames() < a.blockFrames && !a.closed {
			a.notFull.Wait()
		}

		if a.closed {
			a.mu.Unlock()

			return
		}
		a.mu.Unlock()

		n, ok := a.inner.ReadInterleaved(a.scratch, a.channels)

		// The consumer only removes samples between the unlock above and the
		// lock below, so the free space measured above is a safe lower bound and
		// the synthesised block always fits.
		a.mu.Lock()
		a.writeRing(a.scratch[:n*a.channels])
		closed := a.closed
		a.mu.Unlock()

		if !ok || closed {
			return
		}
	}
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

// ReadInterleaved copies buffered samples to out, padding with silence on
// underrun. It performs no synthesis or allocation and never blocks, so it is
// safe on a realtime audio callback.
func (a *AsyncSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	a.mu.Lock()

	n := len(out)
	if n > a.count {
		n = a.count
	}

	for i := 0; i < n; {
		c := copy(out[i:n], a.ring[a.rpos:])
		a.rpos = (a.rpos + c) % len(a.ring)
		i += c
	}

	a.count -= n
	a.mu.Unlock()

	a.notFull.Signal()

	for i := n; i < len(out); i++ {
		out[i] = 0
	}

	return len(out) / channels, true
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
