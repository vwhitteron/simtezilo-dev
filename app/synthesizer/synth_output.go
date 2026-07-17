package synthesizer

import (
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

type OutputDevice struct {
	log zerolog.Logger
}

type SynthOutDeviceOpts struct {
	OutputFile string
	Log        zerolog.Logger
}

func NewOutputDevice(opts SynthOutDeviceOpts) (*OutputDevice, error) {
	return &OutputDevice{
		log: opts.Log,
	}, nil
}

// Streamer produces interleaved audio at the synthesizer's native (internal)
// sample rate. It satisfies the audio.SampleSource interface (ReadInterleaved)
// for N-channel output. Resampling to the output device rate is handled by the
// audio package, not here.
//
// A Streamer is pulled by a single producer goroutine, so its scratch buffers
// (bufs, settings) are reused across calls to avoid per-call heap allocation and
// the GC pressure that previously fed back into audio glitches.
type Streamer struct {
	synth    *Synthesizer
	bufs     [][]float64             // reused per-channel output buffers
	settings []OutputChannelSettings // reused per-channel gain/mute scratch
}

// OutputChannelSettings holds the settings for an output channel.
type OutputChannelSettings struct {
	Gain float64
	Mute bool
}

// NewStreamer creates a new Streamer for the given Synthesizer.
func NewStreamer(synth *Synthesizer) *Streamer {
	return &Streamer{
		synth: synth,
	}
}

// ReadInterleaved fills buf with interleaved float32 frames at the internal
// sample rate. It implements audio.SampleSource.
func (s *Streamer) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels

	outputs := s.readOutputBuffers(frames)
	if outputs == nil {
		for i := range buf {
			buf[i] = 0
		}

		return frames, true
	}

	for frameIdx := range frames {
		for channelIdx := range channels {
			var sample float64
			if channelIdx < len(outputs) && frameIdx < len(outputs[channelIdx]) {
				sample = outputs[channelIdx][frameIdx]
			}

			buf[frameIdx*channels+channelIdx] = float32(sample)
		}
	}

	return frames, true
}

// getOutputChannels retrieves the combined gains and mute states for all channels.
// Note: Master mute is handled earlier for efficiency.
func (s *Streamer) getOutputChannels() []OutputChannelSettings {
	masterGainDB, _ := s.synth.mixer.GetChannelGain(ChannelMaster)

	n := s.synth.numOutputChannels
	if cap(s.settings) < n {
		s.settings = make([]OutputChannelSettings, n)
	}

	channelGains := s.settings[:n]

	for ch := range s.synth.numOutputChannels {
		channelName := s.synth.mixer.OutputChannelName(ch)
		channelGainDB, _ := s.synth.mixer.GetChannelGain(channelName)
		channelGains[ch] = OutputChannelSettings{
			Gain: signal.GainToPowerRatio(channelGainDB + masterGainDB),
			Mute: s.synth.GetChannelMute(ch),
		}
	}

	return channelGains
}

// readOutputBuffers triggers mixing and returns per-channel float64 buffers of
// the requested length, with per-channel gain and mute already applied. Returns
// nil when the master channel is muted (caller should emit silence).
func (s *Streamer) readOutputBuffers(length int) [][]float64 {
	// Trigger mixing to all master channels.
	s.synth.mixer.MixToMaster(length)

	// If master is muted, signal the caller to emit silence.
	if s.synth.mixer.GetMasterMute() {
		return nil
	}

	channels := s.synth.numOutputChannels
	outputChannels := s.getOutputChannels()

	buffers := s.ensureBufs(channels, length)

	for channel := range channels {
		buf := buffers[channel]

		// Read directly into the reusable per-channel buffer, then apply gain in
		// place. count is the number actually written (an underrun returns fewer
		// than length); the unwritten tail is zeroed below.
		count := s.synth.mixer.ReadChannel(s.synth.mixer.OutputChannelName(channel), buf)

		gain := outputChannels[channel].Gain
		mute := outputChannels[channel].Mute

		for i := range length {
			switch {
			case mute || i >= count:
				buf[i] = 0
			default:
				buf[i] *= gain
			}
		}
	}

	return buffers
}

// ensureBufs returns the reusable per-channel buffer set sized to channels x
// length, growing the backing slices only when the requested size exceeds their
// current capacity.
func (s *Streamer) ensureBufs(channels, length int) [][]float64 {
	if cap(s.bufs) < channels {
		s.bufs = make([][]float64, channels)
	}

	s.bufs = s.bufs[:channels]

	for ch := range channels {
		if cap(s.bufs[ch]) < length {
			s.bufs[ch] = make([]float64, length)
		}

		s.bufs[ch] = s.bufs[ch][:length]
	}

	return s.bufs
}
