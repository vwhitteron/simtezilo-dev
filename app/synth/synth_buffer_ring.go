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

// Read returns the requested number of samples from the buffer
// The retrieved samples are not zeroed out and remain in the buffer
func (b *RingBuffer) Read(length int) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if length > b.used {
		length = b.used
	}

	samples := make([]float64, length)

	for i := range length {
		samples[i] = b.buffer[b.readPos]
		b.readPos = (b.readPos + 1) % b.size
	}

	b.used -= length

	return samples
}

// Write adds the given samples to the buffer
// The overwrite parameter determines whether to overwrite or mix the samples with the existing buffer content
func (b *RingBuffer) Write(samples []float64, overwrite bool) {
	if len(samples) == 0 {
		return
	}

	if !overwrite {
		samples = b.mixSamples(samples)
	}

	b.writeToRing(samples, true)
}

// writeToRing writes samples to the ring buffer
func (b *RingBuffer) writeToRing(samples []float64, advance bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, sample := range samples {
		b.buffer[b.writePos] = sample
		if advance {
			b.writePos = (b.writePos + 1) % b.size
			if b.used < b.size {
				b.used++
			} else {
				// Buffer is full, advance read pointer to maintain buffer size
				b.readPos = (b.readPos + 1) % b.size
			}
		}
	}
}

// Advance moves the read position forward by the specified number of samples
// The samples between the start and end positions are zeroed out
func (b *RingBuffer) Advance(samples int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if samples > b.used {
		samples = b.used
	}

	for i := 0; i < samples; i++ {
		b.buffer[b.readPos] = 0
		b.readPos = (b.readPos + 1) % b.size
	}

	b.used -= samples
}

// mixSamples mixes the input samples with the existing samples in the buffer
func (b *RingBuffer) mixSamples(inSamples []float64) []float64 {
	outSamples := make([]float64, len(inSamples))
	peak := 0.0

	b.mu.RLock()
	readPos := b.readPos
	bufferUsed := b.used
	b.mu.RUnlock()

	for i := range inSamples {
		inputSample := inSamples[i]

		var bufferSample float64
		if i < bufferUsed {
			bufferPos := (readPos + i) % b.size
			bufferSample = b.buffer[bufferPos]
		}

		outSamples[i] = mixSampleSum(inputSample, bufferSample, &peak)
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
	}

	return outSamples
}
