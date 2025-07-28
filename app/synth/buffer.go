package synth

import (
	"sync"

	"github.com/rs/zerolog"
)

type Buffer struct {
	buffer     []float64
	bufferSize int
	log        zerolog.Logger
	mixer      *Mixer
	mu         sync.Mutex
	slots      int
	slotSize   int
}

func NewBuffer(slotSize int, slots int, mixer *Mixer, logger zerolog.Logger) *Buffer {
	bufferSize := slotSize * slots * 2
	buffer := &Buffer{
		buffer:     make([]float64, bufferSize),
		bufferSize: bufferSize,
		log:        logger,
		mixer:      mixer,
		slots:      slots,
		slotSize:   slotSize,
	}

	buffer.ClearBuffer()

	return buffer
}

func (b *Buffer) ClearBuffer() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range b.bufferSize {
		b.buffer[i] = 0
	}
}

func (b *Buffer) GetLength() int {
	return b.bufferSize
}

func (b *Buffer) Write(channel string, samples []float64) {
	volume, err := b.mixer.GetChannelVolume(channel)
	if err != nil {
		b.log.Error().Err(err).Str("channel", channel).Msg("failed to get channel volume")

		return
	}

	if channel == "gear" {
		b.log.Debug().Float64("volume", volume).Str("channel", channel).Msg("writing sample to channel")
	}

	outSamples := b.mixSamples(samples, volume)
	copy(b.buffer, outSamples)
}

func (b *Buffer) WriteWithVolumePercent(channel string, percent int, samples []float64) {
	volume, err := b.mixer.GetChannelVolume(channel)
	if err != nil {
		b.log.Error().Err(err).Str("channel", channel).Msg("failed to get channel volume")

		return
	}

	volume = float64(percent) / 100.0 * volume

	outSamples := b.mixSamples(samples, volume)
	copy(b.buffer, outSamples)
}

func (b *Buffer) mixSamples(inSamples []float64, volume float64) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	peak := 0.0
	outSamples := make([]float64, len(inSamples))

	for i := range inSamples {
		inputSample := inSamples[i] * volume
		bufferSample := b.buffer[i]

		outSamples[i] = b.mixer.MixSample(inputSample, bufferSample, &peak)
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
		b.log.Debug().Float64("peak", peak).Msg("AGC applied")
	}

	return outSamples
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

func (b *Buffer) ShiftBuffer(samples int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	bufferMax := b.bufferSize - samples

	for i := range bufferMax {
		b.buffer[i] = b.buffer[i+samples]
	}
}
