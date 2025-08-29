package synth

import (
	"sync"
	"time"
)

// AdaptiveBuffer implements an adaptive buffer that can handle varying read/write patterns
// It combines ring buffer efficiency with overflow protection
type AdaptiveBuffer struct {
	buffer   []float64
	mu       sync.RWMutex
	writePos int // current write position
	readPos  int // current read position
	size     int // buffer size
	used     int // number of samples currently in buffer

	// Adaptive features
	overflows  int       // count of buffer overflows
	underruns  int       // count of buffer underruns
	lastAccess time.Time // last time buffer was accessed
	targetFill int       // target fill level to maintain
}

// NewAdaptiveBuffer creates a new adaptive buffer
func NewAdaptiveBuffer(size int) *AdaptiveBuffer {
	targetFill := size / 4 // Keep buffer 25% full on average

	buffer := &AdaptiveBuffer{
		buffer:     make([]float64, size),
		writePos:   0,
		readPos:    0,
		size:       size,
		used:       0,
		targetFill: targetFill,
		lastAccess: time.Now(),
	}

	buffer.Clear()
	return buffer
}

// Clear zeros out the entire buffer and resets state
func (b *AdaptiveBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.size {
		b.buffer[i] = 0
	}

	b.writePos = 0
	b.readPos = 0
	b.used = 0
	b.overflows = 0
	b.underruns = 0
	b.lastAccess = time.Now()
}

// Length returns the total buffer capacity
func (b *AdaptiveBuffer) Length() int {
	return b.size
}

// Used returns the number of samples currently in the buffer
func (b *AdaptiveBuffer) Used() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.used
}

// Available returns space available for writing
func (b *AdaptiveBuffer) Available() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size - b.used
}

// Health returns buffer health metrics
func (b *AdaptiveBuffer) Health() (overflows, underruns int, fillRatio float64) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	fillRatio = float64(b.used) / float64(b.size)
	return b.overflows, b.underruns, fillRatio
}

// Inspect returns a copy of samples without consuming them
func (b *AdaptiveBuffer) Inspect(length int) []float64 {
	return b.readFromBuffer(length, false)
}

// Read returns samples and consumes them
func (b *AdaptiveBuffer) Read(length int) []float64 {
	return b.readFromBuffer(length, true)
}

// Write adds samples to the buffer with overflow protection
func (b *AdaptiveBuffer) Write(samples []float64, overwrite bool) {
	if len(samples) == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastAccess = time.Now()

	// Check for potential overflow
	if len(samples) > (b.size-b.used) && overwrite {
		b.overflows++

		// If overwrite is false and we're mixing, we need to be more careful
		if !overwrite {
			// Drop oldest samples to make room, but try to preserve audio continuity
			samplesToDrop := len(samples) - (b.size - b.used)
			b.dropOldestSamples(samplesToDrop)
		}
	}

	if overwrite {
		// Overwrite mode: just write samples to buffer
		for _, sample := range samples {
			b.buffer[b.writePos] = sample
			b.writePos = (b.writePos + 1) % b.size

			if b.used < b.size {
				b.used++
			} else {
				// Buffer full, advance read pointer (circular overwrite)
				b.readPos = (b.readPos + 1) % b.size
			}
		}
	} else {
		// Mix mode: mix with existing buffer content starting from read position
		peak := 0.0
		for i, inputSample := range samples {
			// Calculate position to mix at (starting from read position)
			mixPos := (b.readPos + i) % b.size

			// Only mix if we have existing content at this position
			var mixedSample float64
			if i < b.used {
				existingSample := b.buffer[mixPos]
				mixedSample = mixSampleSum(inputSample, existingSample, &peak)
			} else {
				mixedSample = inputSample
			}

			b.buffer[mixPos] = mixedSample
		}

		// Update used count if we wrote beyond existing content
		if len(samples) > b.used {
			b.used = len(samples)
		}

		// Apply peak limiting if necessary
		if peak > 1.0 {
			for i := 0; i < len(samples); i++ {
				pos := (b.readPos + i) % b.size
				b.buffer[pos] /= peak
			}
		}
	}
}

// readFromBuffer internal implementation for reading
func (b *AdaptiveBuffer) readFromBuffer(length int, consume bool) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastAccess = time.Now()

	// Check for underrun
	if length > b.used {
		if consume {
			b.underruns++
		}
		length = b.used
	}

	samples := make([]float64, length)

	for i := range length {
		samples[i] = b.buffer[b.readPos]

		if consume {
			b.buffer[b.readPos] = 0 // Clear consumed samples
			b.used--
		}

		b.readPos = (b.readPos + 1) % b.size
	}

	return samples
}

// dropOldestSamples removes oldest samples to prevent overflow
func (b *AdaptiveBuffer) dropOldestSamples(count int) {
	for i := 0; i < count && b.used > 0; i++ {
		b.buffer[b.readPos] = 0
		b.readPos = (b.readPos + 1) % b.size
		b.used--
	}
}

// mixSamplesInternal mixes input samples with existing buffer content
func (b *AdaptiveBuffer) mixSamplesInternal(inSamples []float64) []float64 {
	outSamples := make([]float64, len(inSamples))
	peak := 0.0

	for i := range inSamples {
		inputSample := inSamples[i]

		var bufferSample float64
		if i < b.used {
			bufferPos := (b.readPos + i) % b.size
			bufferSample = b.buffer[bufferPos]
		}

		outSamples[i] = mixSampleSum(inputSample, bufferSample, &peak)
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
	}

	return outSamples
}

// IsStarved returns true if buffer is running low
func (b *AdaptiveBuffer) IsStarved() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used <= (b.targetFill / 2)
}

// IsOverfull returns true if buffer is too full
func (b *AdaptiveBuffer) IsOverfull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used > (b.size * 3 / 4)
}

// GetOptimalReadSize suggests optimal read size based on current state
func (b *AdaptiveBuffer) GetOptimalReadSize(requestedSize int) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// If we're overfull, suggest reading more to drain buffer
	if b.used > (b.size * 3 / 4) {
		return min(requestedSize*2, b.used)
	}

	// If we're starved, suggest reading less to preserve buffer
	if b.used < (b.targetFill / 2) {
		return min(requestedSize/2, b.used)
	}

	// Normal case
	return min(requestedSize, b.used)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
