package synthesizer

import (
	"github.com/gopxl/beep"
	"github.com/rs/zerolog"
)

type OutputDevice struct {
	samples map[string]*beep.Buffer
	log     zerolog.Logger
}

type SynthOutDeviceOpts struct {
	OutputFile string
	Log        zerolog.Logger
}

func NewOutputDevice(opts SynthOutDeviceOpts) (*OutputDevice, error) {
	buffers := map[string]*beep.Buffer{}

	return &OutputDevice{
		samples: buffers,
		log:     opts.Log,
	}, nil
}

type HapticStream struct {
	streamer beep.Streamer
}

func NewHapticStream(synth *Synthesizer, outputSampleRate beep.SampleRate) *HapticStream {
	// Create the internal streamer that works at the synthesizer's native sample rate (8kHz)
	internalStreamer := &Streamer{synth: synth}

	var streamer beep.Streamer

	internalSampleRate := beep.SampleRate(synth.GetSampleRate())
	if internalSampleRate == outputSampleRate {
		// No resampling needed, return the internal streamer directly
		streamer = internalStreamer
	} else {
		// Create a streamer from 8kHz to the output sample rate (32kHz)
		// Quality level 4 provides good performance for real-time resampling with good quality
		streamer = beep.Resample(4, beep.SampleRate(synth.GetSampleRate()), outputSampleRate, internalStreamer)
	}

	return &HapticStream{
		streamer: streamer,
	}
}

// Streamer handles streaming at the synthesizer's native sample rate (8kHz)
type Streamer struct {
	synth *Synthesizer
}

func (s *Streamer) Stream(samples [][2]float64) (n int, ok bool) {
	buffer := s.synth.ReadBuffer(len(samples))

	for i := range samples {
		samples[i][0] = s.synth.ApplyMasterGain(buffer[i])
		samples[i][1] = s.synth.ApplyMasterGain(buffer[i])
	}

	return len(samples), true
}

func (s *Streamer) Err() error {
	return nil
}

func (b *HapticStream) Stream(samples [][2]float64) (n int, ok bool) {
	// Stream the resampled audio using the persistent resampler
	return b.streamer.Stream(samples)
}

func (b *HapticStream) Err() error {
	return b.streamer.Err()
}
