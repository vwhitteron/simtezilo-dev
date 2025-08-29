package synth

import (
	"sync"
)

// RingBuffer implements a ring buffer for audio samples
type RingBuffer struct {
	buffer []float64
	mu     sync.RWMutex

	// Ring buffer state
	writePos int // current write position
	readPos  int // current read position
	size     int // buffer size
	used     int // number of samples currently in buffer
}

// NewRingBuffer creates a new ring buffer that can hold the specified number of samples
func NewRingBuffer(size int) *RingBuffer {
	buffer := &RingBuffer{
		buffer:   make([]float64, size),
		writePos: 0,
		readPos:  0,
		size:     size,
		used:     0,
	}

	buffer.Clear()

	return buffer
}

// Clear zeros out the entire buffer and resets associated pointers
func (b *RingBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.size {
		b.buffer[i] = 0
	}

	b.writePos = 0
	b.readPos = 0
	b.used = 0
}

// Used returns the total number of samples the buffer can hold
func (b *RingBuffer) Length() int {
	return b.size
}

// Used returns the number of samples currently in the buffer
func (b *RingBuffer) Used() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used
}

// Available returns the number of samples that can be written before the buffer is full
func (b *RingBuffer) Available() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.size - b.used
}

// IsFull returns true if the buffer is at capacity
func (b *RingBuffer) IsFull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used >= b.size
}

// Inspect returns a copy of the requested number of samples from the buffer
// The samples stored in the buffer are not modified and remain in place
func (b *RingBuffer) Inspect(length int) []float64 {
	return b.readFromBuffer(length, false)
}

// Read returns the requested number of samples from the buffer
// The samples stored in the buffer are zeroed out
func (b *RingBuffer) Read(length int) []float64 {
	return b.readFromBuffer(length, true)
}

// Write adds the given samples to the buffer
// The overwrite parameter determines whether to overwrite or mix the samples with the existing buffer content
func (b *RingBuffer) Write(samples []float64, overwrite bool) {
	if len(samples) == 0 {
		return
	}

	if !overwrite {
		// Mix mode: mix directly into the buffer at readPos like adaptive buffer
		b.mixIntoBuffer(samples)
	} else {
		// Overwrite mode: write to writePos
		b.writeToBuffer(samples, true)
	}
}

// readFromBuffer reads samples from the ring buffer
// When scrub is true, the samples are zeroed out in the buffer
func (b *RingBuffer) readFromBuffer(length int, scrub bool) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if length > b.used {
		length = b.used
	}

	samples := make([]float64, length)

	for i := range length {
		samples[i] = b.buffer[b.readPos]

		if scrub {
			b.buffer[b.readPos] = 0
			b.used--
		}

		b.readPos = (b.readPos + 1) % b.size
	}

	return samples
}

// writeToBuffer writes samples to the ring buffer
// The advance parameter determines whether to move the write position forward
func (b *RingBuffer) writeToBuffer(samples []float64, advance bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, sample := range samples {
		b.buffer[b.writePos] = sample
		if advance {
			b.writePos = (b.writePos + 1) % b.size
			if b.used < b.size {
				b.used++
			} else {
				// Buffer is full, we need to drop the oldest sample
				// This maintains a consistent buffer size but may cause dropouts
				// A better approach might be to return an error or block
				b.readPos = (b.readPos + 1) % b.size
			}
		}
	}
}

// mixIntoBuffer mixes samples directly into the buffer at the correct positions
// This is similar to the adaptive buffer's approach to avoid position mismatches
func (b *RingBuffer) mixIntoBuffer(samples []float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	peak := 0.0

	for i, inputSample := range samples {
		// Mix at position relative to readPos, like adaptive buffer
		mixPos := (b.readPos + i) % b.size

		var existingSample float64
		if i < b.used {
			existingSample = b.buffer[mixPos]
		}

		mixedSample := mixSampleSum(inputSample, existingSample, &peak)
		b.buffer[mixPos] = mixedSample
	}

	// Update used count if we mixed beyond existing content
	if len(samples) > b.used {
		b.used = len(samples)
		// Update writePos to account for new content
		b.writePos = (b.readPos + b.used) % b.size
	}

	// Apply peak limiting if necessary
	if peak > 1.0 {
		for i := 0; i < len(samples); i++ {
			pos := (b.readPos + i) % b.size
			b.buffer[pos] /= peak
		}
	}
}

// mixSamples mixes the input samples with the existing samples in the buffer
func (b *RingBuffer) mixSamples(inSamples []float64) []float64 {
	outSamples := make([]float64, len(inSamples))
	peak := 0.0

	b.mu.RLock()
	defer b.mu.RUnlock()

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
