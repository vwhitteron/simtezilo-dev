// //go:build portaudio

package audio

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"
)

func init() {
	registerBackend(BackendPortAudio, func(log zerolog.Logger) (Backend, error) {
		err := portaudio.Initialize()
		if err != nil {
			return nil, fmt.Errorf("portaudio: initialize: %w", err)
		}

		return &portAudioBackend{
			log: log.With().Str("backend", BackendPortAudio).Logger(),
		}, nil
	})
}

// portAudioBackend implements Backend using github.com/gordonklaus/portaudio.
type portAudioBackend struct {
	log zerolog.Logger
}

func (b *portAudioBackend) Name() string { return BackendPortAudio }

func (b *portAudioBackend) ListDevices() ([]Device, error) {
	all, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("portaudio: list devices: %w", err)
	}

	defaultDev, _ := portaudio.DefaultOutputDevice()

	raw := make([]Device, 0, len(all))
	for _, dev := range all {
		if dev.MaxOutputChannels <= 0 {
			continue
		}

		isDefault := defaultDev != nil && dev.Index == defaultDev.Index

		raw = append(raw, Device{
			ID:                strconv.Itoa(dev.Index),
			Name:              dev.Name,
			Backend:           BackendPortAudio,
			MaxChannels:       dev.MaxOutputChannels,
			DefaultSampleRate: int(dev.DefaultSampleRate),
			IsDefault:         isDefault,
		})
	}

	// Collapse the raw ALSA enumeration (one card shows up as hw + several
	// virtual aliases) into one friendly entry per physical output, tagging
	// bluealsa PCMs as Bluetooth. See curate.go.
	return CurateLocal(raw), nil
}

func (b *portAudioBackend) OpenSink(cfg SinkConfig) (Sink, error) {
	var (
		dev *portaudio.DeviceInfo
		err error
	)

	if cfg.DeviceID != "" {
		idx, err := strconv.Atoi(cfg.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("portaudio: invalid device ID %q: %w", cfg.DeviceID, err)
		}

		all, err := portaudio.Devices()
		if err != nil {
			return nil, fmt.Errorf("portaudio: list devices: %w", err)
		}

		for _, d := range all {
			if d.Index == idx {
				dev = d

				break
			}
		}

		if dev == nil {
			return nil, fmt.Errorf("portaudio: device ID %q not found", cfg.DeviceID)
		}
	} else {
		dev, err = portaudio.DefaultOutputDevice()
		if err != nil {
			return nil, fmt.Errorf("portaudio: default output device: %w", err)
		}
	}

	latency := dev.DefaultLowOutputLatency
	if cfg.LatencyMs > 0 {
		latency = time.Duration(cfg.LatencyMs) * time.Millisecond
	}

	params := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   dev,
			Channels: cfg.Channels,
			Latency:  latency,
		},
		SampleRate:      float64(cfg.SampleRate),
		FramesPerBuffer: portaudio.FramesPerBufferUnspecified,
	}

	return &portAudioSink{
		log:      b.log,
		params:   params,
		channels: cfg.Channels,
	}, nil
}

func (b *portAudioBackend) Close() error {
	err := portaudio.Terminate()
	if err != nil {
		return fmt.Errorf("portaudio: terminate: %w", err)
	}

	return nil
}

// portAudioSink is a Sink backed by a portaudio stream.
type portAudioSink struct {
	log      zerolog.Logger
	params   portaudio.StreamParameters
	channels int
	stream   *portaudio.Stream
}

func (s *portAudioSink) Start(src SampleSource) error {
	stream, err := portaudio.OpenStream(s.params, func(out []float32) {
		src.ReadInterleaved(out, s.channels)
	})
	if err != nil {
		return fmt.Errorf("portaudio: open stream: %w", err)
	}

	if err := stream.Start(); err != nil {
		_ = stream.Close()

		return fmt.Errorf("portaudio: start stream: %w", err)
	}

	s.stream = stream

	// Log the latency the host API actually negotiated. The Latency field in
	// StreamParameters is only a hint, and FramesPerBufferUnspecified lets the
	// host API choose the real buffer size, so the requested value (the
	// configured LatencyMs) and the effective device latency often differ. This
	// negotiated value is the device-side contribution to the input->output
	// delay, and reveals whether changing the latency setting has any effect.
	if info := stream.Info(); info != nil {
		s.log.Debug().
			Dur("requested", s.params.Output.Latency).
			Dur("negotiated", info.OutputLatency).
			Float64("sampleRate", info.SampleRate).
			Msg("Portaudio stream latency")
	}

	return nil
}

func (s *portAudioSink) Stop() error {
	if s.stream == nil {
		return nil
	}

	// Abort, not Stop: Pa_StopStream blocks until buffered audio drains, which
	// never completes when the sink feeds the snd-aloop loopback and nothing is
	// consuming the capture side (the Bluetooth bridge is down or the device
	// isn't selected). Pa_AbortStream stops immediately, discarding buffered
	// frames — the right behaviour both at shutdown and when reopening on a
	// device change.
	err := s.stream.Abort()
	if err != nil {
		return fmt.Errorf("portaudio: abort stream: %w", err)
	}

	err = s.stream.Close()
	if err != nil {
		return fmt.Errorf("portaudio: close stream: %w", err)
	}

	s.stream = nil

	return nil
}

func (s *portAudioSink) Channels() int { return s.channels }
