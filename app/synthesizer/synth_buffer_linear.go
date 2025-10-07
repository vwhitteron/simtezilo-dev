package synthesizer

import (
	"sync"
	"time"
)

// LinearBuffer is a simple linear buffer implementation for audio samples.
type LinearBuffer struct {
	buffer []float64
	mu     sync.Mutex
}

// NewLinearBuffer creates a new linear buffer that can hold the specified duration of audio
// bufferDuration: duration of audio the buffer should hold
// sampleRateHz: sample rate in Hz to calculate buffer size in samples.
func NewLinearBuffer(length time.Duration, sampleRateHz int) *LinearBuffer {
	capacity := int(length.Seconds() * float64(sampleRateHz))

	buffer := &LinearBuffer{
		buffer: make([]float64, capacity),
	}

	buffer.Clear()

	return buffer
}

// Clear zeros out the entire buffer and resets associated pointers.
func (b *LinearBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range len(b.buffer) {
		b.buffer[i] = 0
	}
}

// Length returns the current length of the buffer in samples.
func (b *LinearBuffer) Length() int {
	return len(b.buffer)
}

// Inspect returns a copy of the requested number of samples from the buffer
// offset: position relative to write position (negative values read historical samples)
// The samples stored in the buffer are not modified and remain in place.
func (b *LinearBuffer) Inspect(length int, offset int) []float64 {
	// For linear buffer, we'll implement a simple version that ignores offset for backward compatibility
	// This maintains the existing behavior while satisfying the interface
	_ = offset // Ignore offset parameter for now

	return b.readFromBuffer(length, false)
}

// Read returns the requested number of samples from the buffer
// The samples stored in the buffer are zeroed out.
func (b *LinearBuffer) Read(length int) []float64 {
	return b.readFromBuffer(length, true)
}

// Write adds the given samples to the buffer
// The overwrite parameter determines whether to overwrite or mix the samples with the existing buffer content.
func (b *LinearBuffer) Write(samples []float64, overwrite bool) {
	if len(samples) == 0 {
		return
	}

	if !overwrite {
		samples = b.mixSamples(samples)
	}

	copy(b.buffer, samples)
}

// readFromBuffer reads samples from the ring buffer
// When scrub is true, the samples are zeroed out in the buffer.
func (b *LinearBuffer) readFromBuffer(length int, scrub bool) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	samples := make([]float64, length)
	for i := range length {
		samples[i] = b.buffer[i]

		if scrub {
			b.buffer[i] = 0
		}
	}

	return samples
}

// mixSamples mixes the input samples with the existing samples in the buffer.
func (b *LinearBuffer) mixSamples(inSamples []float64) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	peak := 0.0
	outSamples := make([]float64, len(inSamples))

	for i := range inSamples {
		inputSample := inSamples[i]
		bufferSample := b.buffer[i]

		outSamples[i] = mixSampleSum(inputSample, bufferSample, &peak)
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
	}

	return outSamples
}
