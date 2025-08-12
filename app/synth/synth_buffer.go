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
	magnitude, err := b.mixer.GetChannelPowerRatio(channel)
	if err != nil {
		b.log.Error().Err(err).Str("channel", channel).Msg("get channel power ratio")

		return
	}

	// if channel == "engine" {
	// 	gain, _ := b.mixer.GetChannelGain(channel)
	// 	b.log.Info().Float64("gain", gain).Float64("magnitude", magnitude).Str("channel", channel).Msg("write sample to channel")
	// }

	outSamples := b.mixSamples(samples, magnitude)
	copy(b.buffer, outSamples)
}

func (b *Buffer) WriteWithMagnitude(channel string, magnitude float64, samples []float64) {
	channelMagnitude, err := b.mixer.GetChannelPowerRatio(channel)
	if err != nil {
		b.log.Error().Err(err).Str("channel", channel).Msg("get channel power ratio")

		return
	}

	outSamples := b.mixSamples(samples, magnitude*channelMagnitude)
	copy(b.buffer, outSamples)
}

func (b *Buffer) mixSamples(inSamples []float64, magnitude float64) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	peak := 0.0
	outSamples := make([]float64, len(inSamples))

	for i := range inSamples {
		inputSample := inSamples[i] * magnitude
		bufferSample := b.buffer[i]

		outSamples[i] = b.mixer.MixSample(inputSample, bufferSample, &peak)
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
		b.log.Debug().Float64("peak", peak).Float64("magnitude", magnitude).Msg("AGC applied")
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
