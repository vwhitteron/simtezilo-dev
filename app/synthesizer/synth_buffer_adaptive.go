package synthesizer

import (
	"math"
	"sync"
	"time"
)

// AdaptiveBuffer implements an adaptive buffer that can handle varying read/write patterns
// It combines ring buffer efficiency with overflow protection.
type AdaptiveBuffer struct {
	buffer   []float64
	mu       sync.RWMutex
	writePos int // current write position
	readPos  int // current read position
	capacity int // buffer size
	used     int // number of samples currently in buffer

	// Adaptive features
	overflows       int       // count of buffer overflows
	underruns       int       // count of buffer underruns
	lastAccess      time.Time // last time buffer was accessed
	readDelay       int       // delay between buffer write and read
	lastOverflow    time.Time // timestamp of the most recent overflow event (zero if never)
	lastUnderrun    time.Time // timestamp of the most recent underrun event (zero if never)
	overflowSamples int       // cumulative samples dropped due to overflow

	// scaleScratch holds the magnitude-scaled copy used by WriteScaled so the
	// caller's slice is never mutated.
	scaleScratch []float64

	// Underrun concealment. On a short read the tail is not zeroed abruptly;
	// instead the last delivered sample is faded to zero over declickLen samples
	// with a raised-cosine ramp, so a starved channel eases out instead of
	// stepping to silence (which clicks). declickLeft is the remaining ramp
	// budget, recharged whenever real samples are read; lastSample is the value
	// the ramp starts from. The ramp can span multiple reads, so a gap longer
	// than one block still fades over exactly declickLen samples.
	declickLen  int
	declickLeft int
	lastSample  float64
}

// defaultBufferCushionMs is the read-delay cushion used by NewAdaptiveBuffer
// when no explicit cushion is supplied.
const defaultBufferCushionMs = 24

// declickRampMs is the duration of the underrun fade-to-zero ramp. A few
// milliseconds is long enough to be click-free yet short enough not to smear a
// transient's tail.
const declickRampMs = 3

// NewAdaptiveBuffer creates a new adaptive buffer with the default 24 ms cushion.
// bufferDuration: duration of audio the buffer should hold
// sampleRateHz: sample rate in Hz to calculate buffer size in samples.
func NewAdaptiveBuffer(length time.Duration, sampleRateHz int) *AdaptiveBuffer {
	return NewAdaptiveBufferCushion(length, sampleRateHz, defaultBufferCushionMs)
}

// NewAdaptiveBufferCushion creates a new adaptive buffer with an explicit
// read-delay cushion. The cushion is the amount of audio the buffer holds in
// reserve; it must comfortably exceed the consumer's per-read pull size so a
// briefly late writer does not force a short read (which a consumer zero-pads
// into an audible click). A non-positive cushion falls back to the default.
func NewAdaptiveBufferCushion(length time.Duration, sampleRateHz, cushionMs int) *AdaptiveBuffer {
	if cushionMs <= 0 {
		cushionMs = defaultBufferCushionMs
	}

	capacity := int(length.Seconds() * float64(sampleRateHz))

	// Calculate read delay from the cushion, but cap to 25% of capacity to
	// ensure there's always room for actual content.
	readDelay := (sampleRateHz / 1000) * cushionMs

	maxReadDelay := capacity / 4
	if readDelay > maxReadDelay {
		readDelay = maxReadDelay
	}

	declickLen := (sampleRateHz / 1000) * declickRampMs
	if declickLen < 1 {
		declickLen = 1
	}

	buffer := &AdaptiveBuffer{
		buffer:     make([]float64, capacity),
		writePos:   readDelay,
		readPos:    0,
		capacity:   capacity,
		used:       readDelay,
		readDelay:  readDelay,
		declickLen: declickLen,
	}

	buffer.updateLastAccess()

	buffer.Clear()

	return buffer
}

// Clear zeros out the entire buffer and resets state.
func (b *AdaptiveBuffer) Clear() {
	b.mu.Lock()

	for i := range b.capacity {
		b.buffer[i] = 0
	}

	b.writePos = b.readDelay
	b.readPos = 0
	b.used = b.readDelay
	b.overflows = 0
	b.underruns = 0
	b.declickLeft = 0
	b.lastSample = 0

	b.mu.Unlock()

	b.updateLastAccess()
}

// Length returns the total buffer capacity.
func (b *AdaptiveBuffer) Length() int {
	return b.capacity
}

// Used returns the number of samples currently in the buffer.
func (b *AdaptiveBuffer) Used() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used
}

// Available returns space available for writing.
func (b *AdaptiveBuffer) Available() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.capacity - b.used
}

// BufferHealth holds a snapshot of adaptive buffer diagnostic metrics.
type BufferHealth struct {
	Overflows       int
	Underruns       int
	FillRatio       float64 // 0..1
	Capacity        int
	Used            int
	Available       int
	ReadDelay       int
	LastOverflow    time.Time // zero if never overflowed
	LastUnderrun    time.Time // zero if never underran
	OverflowSamples int       // cumulative samples dropped due to overflow
}

// Health returns buffer health metrics. Deprecated: use HealthDetailed instead.
func (b *AdaptiveBuffer) Health() (overflows, underruns int, fillRatio float64) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	fillRatio = float64(b.used) / float64(b.capacity)

	return b.overflows, b.underruns, fillRatio
}

// HealthDetailed returns a detailed snapshot of buffer diagnostics including
// recency timestamps and capacity information.
func (b *AdaptiveBuffer) HealthDetailed() BufferHealth {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return BufferHealth{
		Overflows:       b.overflows,
		Underruns:       b.underruns,
		FillRatio:       float64(b.used) / float64(b.capacity),
		Capacity:        b.capacity,
		Used:            b.used,
		Available:       b.capacity - b.used,
		ReadDelay:       b.readDelay,
		LastOverflow:    b.lastOverflow,
		LastUnderrun:    b.lastUnderrun,
		OverflowSamples: b.overflowSamples,
	}
}

// Inspect returns a copy of samples from the buffer without consuming them.
// offset: position relative to write position (negative values read historical samples)
// length: number of samples to read
// If offset + length exceeds available samples, returns only available samples.
func (b *AdaptiveBuffer) Inspect(length int, offset int) []float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if length <= 0 {
		return nil
	}

	// Calculate the actual read position with offset applied
	readPosition := (b.writePos + offset) % b.capacity
	if readPosition < 0 {
		readPosition += b.capacity
	}

	// Determine maximum available samples considering the offset
	var maxAvailable int
	if offset >= 0 {
		// Reading forward from write position
		maxAvailable = b.capacity - offset
		if maxAvailable > (b.capacity - b.used) {
			maxAvailable = b.capacity - b.used
		}
	} else {
		// Reading backward (historical samples)
		maxAvailable = max(b.used+offset, 0)
	}

	if length > maxAvailable {
		length = maxAvailable
	}

	if length <= 0 {
		return nil
	}

	result := make([]float64, length)

	end := readPosition + length
	if end <= b.capacity {
		copy(result, b.buffer[readPosition:end])
	} else {
		firstPart := b.capacity - readPosition
		copy(result[:firstPart], b.buffer[readPosition:])
		copy(result[firstPart:], b.buffer[:end-b.capacity])
	}

	return result
}

// Read copies up to len(dst) consumed samples into dst and returns the number
// actually written. On an underrun fewer than len(dst) samples are written (the
// available count); callers must respect the returned count rather than
// assuming len(dst).
func (b *AdaptiveBuffer) Read(dst []float64) int {
	return b.readIntoBuffer(dst, true)
}

// Write adds samples to the buffer with overflow protection.
func (b *AdaptiveBuffer) Write(samples []float64, offset int, overwrite bool) {
	if len(samples) == 0 {
		return
	}

	b.updateLastAccess()

	b.mu.Lock()
	defer b.mu.Unlock()

	b.applyOffset(offset)

	if overwrite {
		b.writeOverwriteMode(samples)

		return
	}

	b.writeMixMode(samples)
}

// WriteScaled adds samples to the buffer scaled by magnitude without mutating
// the caller's slice. The scaled copy is staged in a reusable scratch buffer, so
// repeated writes of a shared, cached source slice (e.g. an effect sample) are
// not corrupted in place. A magnitude of 1.0 needs no scaling and delegates to
// Write directly.
func (b *AdaptiveBuffer) WriteScaled(samples []float64, magnitude float64, offset int, overwrite bool) {
	if magnitude == 1.0 {
		b.Write(samples, offset, overwrite)

		return
	}

	if len(samples) == 0 {
		return
	}

	b.updateLastAccess()

	b.mu.Lock()
	defer b.mu.Unlock()

	if cap(b.scaleScratch) < len(samples) {
		b.scaleScratch = make([]float64, len(samples))
	}

	b.scaleScratch = b.scaleScratch[:len(samples)]
	for i, sample := range samples {
		b.scaleScratch[i] = sample * magnitude
	}

	b.applyOffset(offset)

	if overwrite {
		b.writeOverwriteMode(b.scaleScratch)

		return
	}

	b.writeMixMode(b.scaleScratch)
}

// IsStarved returns true if buffer is running low.
func (b *AdaptiveBuffer) IsStarved() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used <= b.readDelay
}

// IsOverfull returns true if buffer is too full.
func (b *AdaptiveBuffer) IsOverfull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used > (b.capacity * 3 / 4)
}

// GetOptimalReadSize suggests optimal read size based on current state.
func (b *AdaptiveBuffer) GetOptimalReadSize(requestedSize int) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// If we're overfull, suggest reading more to drain buffer
	if b.used > (b.capacity * 3 / 4) {
		return min(requestedSize*2, b.used)
	}

	// If we're starved, suggest reading less to preserve buffer
	if b.used < (b.readDelay / 2) {
		return min(requestedSize/2, b.used)
	}

	// Normal case
	return min(requestedSize, b.used)
}

// applyOffset applies the write position offset if specified.
func (b *AdaptiveBuffer) applyOffset(offset int) {
	if offset != 0 {
		b.writePos = (b.writePos + offset) % b.capacity
	}
}

// writeOverwriteMode writes samples in overwrite mode, replacing existing content.
func (b *AdaptiveBuffer) writeOverwriteMode(samples []float64) {
	b.handleOverflow(samples, true)

	for _, sample := range samples {
		b.buffer[b.writePos] = sample
		b.writePos = (b.writePos + 1) % b.capacity

		if b.used < b.capacity {
			b.used++
		} else {
			b.readPos = (b.readPos + 1) % b.capacity
		}
	}
}

// writeMixMode writes samples in mix mode, combining with existing content.
//
// Combining uses a memoryless soft-knee mix (softCombine): overlapping waveforms
// are summed and the sum is passed through a soft-knee limiter that asymptotes
// toward ±1 without reaching it. This bounds the result close to unity without a
// separate peak-limiting pass, while avoiding two failure modes: a retroactive
// per-window peak limiter injects amplitude steps (audible clicks) into longer
// waveforms still in flight past the end of the write, and a hard clamp pins the
// output to the rail (sustained DC that overheats the transducer). The soft knee
// does neither — it is 1-Lipschitz so it never manufactures a step, and it never
// flatlines at the rail.
func (b *AdaptiveBuffer) writeMixMode(samples []float64) {
	for index, inputSample := range samples {
		mixedSample := b.mixSampleAtIndex(index, inputSample)
		mixPos := (b.readPos + index) % b.capacity
		b.buffer[mixPos] = mixedSample
	}

	b.syncBufferState(len(samples))
}

// mixSampleAtIndex mixes an input sample with existing content at the given index.
func (b *AdaptiveBuffer) mixSampleAtIndex(index int, inputSample float64) float64 {
	if index < b.used {
		mixPos := (b.readPos + index) % b.capacity
		existingSample := b.buffer[mixPos]

		return softCombine(inputSample, existingSample)
	}

	return inputSample
}

// syncBufferState updates used count and writePos if we wrote beyond existing content.
func (b *AdaptiveBuffer) syncBufferState(samplesWritten int) {
	if samplesWritten > b.used {
		b.used = samplesWritten
		// Update writePos to maintain consistency with used count
		b.writePos = (b.readPos + b.used) % b.capacity
	}
}

// handleOverflow handles buffer overflow situations.
func (b *AdaptiveBuffer) handleOverflow(samples []float64, overwrite bool) {
	if len(samples) > (b.capacity-b.used) && overwrite {
		b.overflows++
		b.lastOverflow = time.Now()

		samplesToDrop := len(samples) - (b.capacity - b.used)
		b.overflowSamples += samplesToDrop

		if !overwrite {
			b.dropOldestSamples(samplesToDrop)
		}
	}
}

// readIntoBuffer is the shared read core. It copies the available samples into
// dst and, on a short read (underrun), conceals the shortfall by fading the last
// delivered sample to zero with a raised-cosine ramp rather than leaving an
// abrupt zero tail. When consume is true the read advances the read position and
// zeroes the consumed samples.
//
// It returns the number of "sounding" samples written — the real samples plus
// any concealment ramp samples emitted this call. dst indices at or beyond that
// count are zero. A fully idle buffer whose ramp has already run out returns 0,
// preserving the caller's ability to detect true silence.
func (b *AdaptiveBuffer) readIntoBuffer(dst []float64, consume bool) int {
	b.updateLastAccess()

	b.mu.Lock()
	defer b.mu.Unlock()

	length := len(dst)

	avail := length
	if avail > b.used {
		avail = b.used

		if consume {
			b.underruns++
			b.lastUnderrun = time.Now()
		}
	}

	for i := range avail {
		dst[i] = b.buffer[b.readPos]

		if consume {
			b.buffer[b.readPos] = 0 // Clear consumed samples
			b.used--
		}

		b.readPos = (b.readPos + 1) % b.capacity
	}

	// Any real samples this block reset the ramp: remember where to fade from and
	// recharge the budget. The budget persists across calls, so a gap spanning
	// several blocks still fades over exactly declickLen samples.
	if avail > 0 {
		b.lastSample = dst[avail-1]
		b.declickLeft = b.declickLen
	}

	// Fully satisfied read: no concealment needed.
	if avail == length {
		return length
	}

	written := avail

	for i := avail; i < length; i++ {
		if b.declickLeft <= 0 {
			dst[i] = 0

			continue
		}

		// Raised-cosine from lastSample (at declickLeft==declickLen) to zero (at
		// declickLeft==0): continuous in value at the join and reaching zero with
		// zero slope.
		phase := math.Pi * float64(b.declickLen-b.declickLeft) / float64(b.declickLen)
		dst[i] = b.lastSample * 0.5 * (1.0 + math.Cos(phase))
		b.declickLeft--
		written++
	}

	return written
}

// dropOldestSamples removes oldest samples to prevent overflow.
func (b *AdaptiveBuffer) dropOldestSamples(count int) {
	for i := 0; i < count && b.used > 0; i++ {
		b.buffer[b.readPos] = 0
		b.readPos = (b.readPos + 1) % b.capacity
		b.used--
	}
}

// mixSamplesInternal mixes input samples with existing buffer content
// func (b *AdaptiveBuffer) mixSamplesInternal(inSamples []float64) []float64 {
// 	outSamples := make([]float64, len(inSamples))
// 	peak := 0.0

// 	for i := range inSamples {
// 		inputSample := inSamples[i]

// 		var bufferSample float64
// 		if i < b.used {
// 			bufferPos := (b.readPos + i) % b.size
// 			bufferSample = b.buffer[bufferPos]
// 		}

// 		outSamples[i] = mixSampleSum(inputSample, bufferSample, &peak)
// 	}

// 	if peak > 1.0 {
// 		scaleSamplesPeak(&outSamples, peak)
// 	}

// 	return outSamples
// }

func (b *AdaptiveBuffer) updateLastAccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastAccess = time.Now()
}
