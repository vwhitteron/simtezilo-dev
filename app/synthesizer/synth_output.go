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

// Streamer produces interleaved audio at the synthesizer's native (internal)
// sample rate. It satisfies the audio.SampleSource interface (ReadInterleaved)
// for N-channel output, and additionally implements beep's stereo Streamer
// interface (Stream) for compatibility. Resampling to the output device rate is
// handled by the audio package, not here.
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

	for ch := range channels {
		raw := s.synth.mixer.ReadChannel(OutputChannelName(ch), length)
		buf := buffers[ch]

		gain := outputChannels[ch].Gain
		mute := outputChannels[ch].Mute

		for i := range length {
			switch {
			case mute || raw == nil || i >= len(raw):
				buf[i] = 0
			default:
				buf[i] = raw[i] * gain
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

	for f := range frames {
		for c := range channels {
			var v float64
			if c < len(outputs) && f < len(outputs[c]) {
				v = outputs[c][f]
			}

			buf[f*channels+c] = float32(v)
		}
	}

	return frames, true
}

// Stream implements beep's stereo Streamer interface, used by tests and the beep
// backend's stereo path. Channels beyond the first two are dropped; if only one
// output channel exists it is duplicated to both stereo channels.
func (s *Streamer) Stream(samples [][2]float64) (n int, ok bool) {
	outputs := s.readOutputBuffers(len(samples))
	if outputs == nil {
		return zeroFill(samples), true
	}

	for i := range samples {
		var left, right float64

		if len(outputs) > 0 && i < len(outputs[0]) {
			left = outputs[0][i]
		}

		if len(outputs) > 1 {
			if i < len(outputs[1]) {
				right = outputs[1][i]
			}
		} else {
			right = left
		}

		samples[i][0] = left
		samples[i][1] = right
	}

	return len(samples), true
}

func (s *Streamer) Err() error {
	return nil
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
		channelName := OutputChannelName(ch)
		channelGainDB, _ := s.synth.mixer.GetChannelGain(channelName)
		channelGains[ch] = OutputChannelSettings{
			Gain: GainToPowerRatio(channelGainDB + masterGainDB),
			Mute: s.synth.GetChannelMute(ch),
		}
	}

	return channelGains
}

// zeroFill fills all samples with zeros and returns the sample count.
func zeroFill(samples [][2]float64) int {
	for i := range samples {
		samples[i][0] = 0
		samples[i][1] = 0
	}

	return len(samples)
}
