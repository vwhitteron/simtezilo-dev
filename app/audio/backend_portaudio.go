//go:build portaudio

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
		if err := portaudio.Initialize(); err != nil {
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

	devices := make([]Device, 0, len(all))
	for _, dev := range all {
		if dev.MaxOutputChannels <= 0 {
			continue
		}

		isDefault := defaultDev != nil && dev.Index == defaultDev.Index

		devices = append(devices, Device{
			ID:                strconv.Itoa(dev.Index),
			Name:              dev.Name,
			Backend:           BackendPortAudio,
			MaxChannels:       dev.MaxOutputChannels,
			DefaultSampleRate: int(dev.DefaultSampleRate),
			IsDefault:         isDefault,
		})
	}

	return devices, nil
}

func (b *portAudioBackend) OpenSink(cfg SinkConfig) (Sink, error) {
	var dev *portaudio.DeviceInfo
	var err error

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
	if err := portaudio.Terminate(); err != nil {
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

	return nil
}

func (s *portAudioSink) Stop() error {
	if s.stream == nil {
		return nil
	}

	if err := s.stream.Stop(); err != nil {
		return fmt.Errorf("portaudio: stop stream: %w", err)
	}

	if err := s.stream.Close(); err != nil {
		return fmt.Errorf("portaudio: close stream: %w", err)
	}

	s.stream = nil

	return nil
}

func (s *portAudioSink) Channels() int { return s.channels }
