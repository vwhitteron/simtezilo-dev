package synth

import (
	"math"
	"sync"

	"github.com/rs/zerolog"
)

type Buffer struct {
	buffer   []float64
	log      zerolog.Logger
	mixer    *Mixer
	mu       sync.Mutex
	slots    int
	slotSize int
}

func NewBuffer(slotSize int, slots int, mixer *Mixer, logger zerolog.Logger) *Buffer {
	buffer := &Buffer{
		buffer:   make([]float64, slotSize*slots),
		log:      logger,
		mixer:    mixer,
		slots:    slots,
		slotSize: slotSize,
	}

	buffer.ClearBuffer()

	return buffer
}

func (b *Buffer) ClearBuffer() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 0; i < len(b.buffer); i++ {
		b.buffer[i] = 0
	}
}

func (b *Buffer) GetLength() int {
	return len(b.buffer)
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

	b.writeAGC(samples, volume)
	// b.writeSimple(samples)
}

func (b *Buffer) WriteWithVolumePercent(channel string, percent int, samples []float64) {
	volume, err := b.mixer.GetChannelVolume(channel)
	if err != nil {
		b.log.Error().Err(err).Str("channel", channel).Msg("failed to get channel volume")

		return
	}

	volume = float64(percent) / 100.0 * volume

	// if channel == "gearchange" {
	// 	b.log.Debug().Float64("volume", volume).Str("channel", channel).Msg("writing sample to channel")
	// }

	b.writeAGC(samples, volume)
}

func (b *Buffer) writeSimple(samples []float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 0; i < len(samples); i++ {
		b.buffer[i] = (b.buffer[i] + samples[i]) * 0.66
	}
}

func (b *Buffer) writeAGC(samples []float64, volume float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	peak := 0.0
	mixedSamples := make([]float64, len(samples))
	for i := 0; i < len(samples); i++ {
		new := b.buffer[i] + (samples[i] * volume)

		newAbs := math.Abs(new)

		if newAbs > peak {
			peak = newAbs
		}

		mixedSamples[i] = new
	}

	scale := 1.0

	if peak > 1.0 {
		scale = 1.0 / peak
	}

	for i := 0; i < len(samples); i++ {
		b.buffer[i] = mixedSamples[i] * scale
	}
}

func (b *Buffer) Read(length int) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	samples := make([]float64, length)

	for i := 0; i < length; i++ {
		samples[i] = b.buffer[i]
	}

	return samples
}

func (b *Buffer) ShiftBuffer(samples int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	bufferMax := len(b.buffer) - samples

	for i := 0; i < bufferMax; i++ {
		b.buffer[i] = b.buffer[i+samples]
	}
}

func (b *Buffer) ShiftBufferSlots(slots int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.shiftBuffer2(slots)
}

func (b *Buffer) shiftBuffer1(slots int) {
	offset := slots * b.slotSize

	for i := 0; i < offset-1; i++ {
		b.buffer[i+offset] = b.buffer[i]
	}
}

func (b *Buffer) shiftBuffer2(slots int) {
	bufferMax := (b.slotSize * b.slots) - 1
	offset := slots * b.slotSize

	for i := bufferMax - offset; i >= 0; i-- {
		b.buffer[i+offset] = b.buffer[i]
	}
}

func (b *Buffer) shiftBuffer3(slots int) {
	bufferMax := (b.slotSize * b.slots) - 1
	offset := slots * b.slotSize

	for i := 0; i <= bufferMax; i++ {
		if i < bufferMax-offset {
			b.buffer[i] = b.buffer[i+offset]
		} else {
			b.buffer[i] = 0
		}
	}
}
