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
	capacity int // buffer size
	used     int // number of samples currently in buffer

	// Adaptive features
	overflows  int       // count of buffer overflows
	underruns  int       // count of buffer underruns
	lastAccess time.Time // last time buffer was accessed
	readDelay  int       // delay between buffer write and read
}

// NewAdaptiveBuffer creates a new adaptive buffer
// bufferDuration: duration of audio the buffer should hold
// sampleRateHz: sample rate in Hz to calculate buffer size in samples
func NewAdaptiveBuffer(length time.Duration, sampleRateHz int) *AdaptiveBuffer {
	capacity := int(length.Seconds() * float64(sampleRateHz))
	readDelay := (sampleRateHz / 1000) * 24

	buffer := &AdaptiveBuffer{
		buffer:    make([]float64, capacity),
		writePos:  readDelay,
		readPos:   0,
		capacity:  capacity,
		used:      readDelay,
		readDelay: readDelay,
	}

	buffer.updateLastAccess()

	buffer.Clear()

	return buffer
}

// Clear zeros out the entire buffer and resets state
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

	b.mu.Unlock()

	b.updateLastAccess()
}

// Length returns the total buffer capacity
func (b *AdaptiveBuffer) Length() int {
	return b.capacity
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
	return b.capacity - b.used
}

// Health returns buffer health metrics
func (b *AdaptiveBuffer) Health() (overflows, underruns int, fillRatio float64) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	fillRatio = float64(b.used) / float64(b.capacity)
	return b.overflows, b.underruns, fillRatio
}

// Inspect returns a copy of samples from the current buffer write position without consuming them.
// If length exceeds available samples, returns only available samples.
func (b *AdaptiveBuffer) Inspect(length int) []float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if length <= 0 {
		return nil
	}

	if length > b.used {
		length = b.used
	}

	result := make([]float64, length)

	end := b.writePos + length
	if end <= b.capacity {
		copy(result, b.buffer[b.writePos:end])
	} else {
		firstPart := b.capacity - b.writePos
		copy(result[:firstPart], b.buffer[b.writePos:])
		copy(result[firstPart:], b.buffer[:end-b.capacity])
	}

	return result
}

// Read returns samples and consumes them
func (b *AdaptiveBuffer) Read(length int) []float64 {
	return b.readFromBuffer(length, true)
}

// Write adds samples to the buffer with overflow protection
func (b *AdaptiveBuffer) Write(samples []float64, offset int, overwrite bool) {
	if len(samples) == 0 {
		return
	}

	b.updateLastAccess()

	b.mu.Lock()
	defer b.mu.Unlock()

	if offset != 0 {
		b.writePos = (b.writePos + offset) % b.capacity
	}

	// Check for potential overflow
	if len(samples) > (b.capacity-b.used) && overwrite {
		b.overflows++

		// If overwrite is false and we're mixing, we need to be more careful
		if !overwrite {
			// Drop oldest samples to make room, but try to preserve audio continuity
			samplesToDrop := len(samples) - (b.capacity - b.used)
			b.dropOldestSamples(samplesToDrop)
		}
	}

	if overwrite {
		// Overwrite mode: just write samples to buffer
		for _, sample := range samples {
			b.buffer[b.writePos] = sample
			b.writePos = (b.writePos + 1) % b.capacity

			if b.used < b.capacity {
				b.used++
			} else {
				// Buffer full, advance read pointer (circular overwrite)
				b.readPos = (b.readPos + 1) % b.capacity
			}
		}
	} else {
		// Mix mode: mix with existing buffer content starting from read position
		peak := 0.0
		for i, inputSample := range samples {
			// Calculate position to mix at (starting from read position)
			mixPos := (b.readPos + i) % b.capacity

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
				pos := (b.readPos + i) % b.capacity
				b.buffer[pos] /= peak
			}
		}
	}
}

// TODO: REMOVE
// WriteAtZeroCrossover writes samples starting from a zero crossing or polarity change
// in the current buffer. If no zero crossing is found within the sample range, it overwrites anyway.
// The entire new sample is written from the zero crossover point.
// func (b *AdaptiveBuffer) WriteAtZeroCrossover(samples []float64) {
// 	if len(samples) == 0 {
// 		return
// 	}

// 	b.updateLastAccess()

// 	b.mu.Lock()
// 	defer b.mu.Unlock()

// 	zeroCrossingPos := FindSampleZeroCrossing(&samples)

// 	// // Find zero crossing or polarity change within the current buffer content
// 	// zeroCrossingPos := -1
// 	// searchRange := min(len(samples), b.used)

// 	// if searchRange > 1 {
// 	// 	// Look for zero crossings in the current buffer content
// 	// 	for i := 0; i < searchRange-1; i++ {
// 	// 		currentPos := (b.readPos + i) % b.capacity
// 	// 		nextPos := (b.readPos + i + 1) % b.capacity

// 	// 		currentSample := b.buffer[currentPos]
// 	// 		nextSample := b.buffer[nextPos]

// 	// 		// Check for exact zero
// 	// 		if currentSample == 0.0 {
// 	// 			zeroCrossingPos = i
// 	// 			break
// 	// 		}

// 	// 		// Check for polarity change (zero crossing)
// 	// 		if (currentSample > 0 && nextSample < 0) || (currentSample < 0 && nextSample > 0) {
// 	// 			zeroCrossingPos = i + 1 // Start writing from the next position
// 	// 			break
// 	// 		}
// 	// 	}
// 	// }

// 	// // If no zero crossing found, start writing from the beginning
// 	// if zeroCrossingPos == -1 {
// 	// 	zeroCrossingPos = 0
// 	// }

// 	// Write the entire new sample starting from the zero crossover point
// 	writeStartPos := (b.readPos + zeroCrossingPos) % b.capacity

// 	for i, sample := range samples {
// 		writePos := (writeStartPos + i) % b.capacity
// 		b.buffer[writePos] = sample

// 		// Update buffer state
// 		samplesFromRead := zeroCrossingPos + i + 1
// 		if samplesFromRead > b.used {
// 			b.used = samplesFromRead
// 		}

// 		// Ensure we don't exceed buffer capacity
// 		if b.used > b.capacity {
// 			// Advance read position to prevent overflow
// 			samplesOverflow := b.used - b.capacity
// 			b.readPos = (b.readPos + samplesOverflow) % b.capacity
// 			b.used = b.capacity
// 			b.overflows++
// 		}
// 	}
// }

// readFromBuffer internal implementation for reading
func (b *AdaptiveBuffer) readFromBuffer(length int, consume bool) []float64 {
	b.updateLastAccess()

	b.mu.Lock()
	defer b.mu.Unlock()

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

		b.readPos = (b.readPos + 1) % b.capacity
	}

	return samples
}

// dropOldestSamples removes oldest samples to prevent overflow
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

// IsStarved returns true if buffer is running low
func (b *AdaptiveBuffer) IsStarved() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used <= b.readDelay
}

// IsOverfull returns true if buffer is too full
func (b *AdaptiveBuffer) IsOverfull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.used > (b.capacity * 3 / 4)
}

// GetOptimalReadSize suggests optimal read size based on current state
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
