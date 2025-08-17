package synth

import (
	"sync"
)

type Buffer struct {
	buffer     []float64
	bufferSize int
	mu         sync.Mutex
	slots      int
	slotSize   int
}

func NewBuffer(slotSize int, slots int) *Buffer {
	bufferSize := slotSize * slots * 2
	buffer := &Buffer{
		buffer:     make([]float64, bufferSize),
		bufferSize: bufferSize,
		slots:      slots,
		slotSize:   slotSize,
	}

	buffer.Clear()

	return buffer
}

func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.bufferSize {
		b.buffer[i] = 0
	}
}

func (b *Buffer) Length() int {
	return b.bufferSize
}

func (b *Buffer) Read(length int) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	samples := make([]float64, length)

	for i := range length {
		samples[i] = b.buffer[i]
	}

	return samples
}

func (b *Buffer) Write(channel string, samples []float64, magnitude float64, overwrite bool) {
	outSamples := make([]float64, len(samples))
	if overwrite { // TODO: need channel buffers as these samples are mixed directly onto the output buffer
		for i := range samples {
			outSamples[i] = samples[i] * magnitude
		}
	} else {
		outSamples = b.mixSamples(samples, magnitude)
	}
	copy(b.buffer, outSamples)
}

func (b *Buffer) Shift(samples int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	bufferMax := b.bufferSize - samples

	for i := range bufferMax {
		b.buffer[i] = b.buffer[i+samples]
	}
}

func (b *Buffer) mixSamples(inSamples []float64, magnitude float64) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	peak := 0.0
	outSamples := make([]float64, len(inSamples))

	for i := range inSamples {
		inputSample := inSamples[i] * magnitude
		bufferSample := b.buffer[i]

		outSamples[i] = mixSampleAGC(inputSample, bufferSample, &peak)
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
	}

	return outSamples
}
