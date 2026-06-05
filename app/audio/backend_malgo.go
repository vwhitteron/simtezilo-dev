//go:build malgo

package audio

import (
	"encoding/hex"
	"fmt"
	"unsafe"

	"github.com/gen2brain/malgo"
	"github.com/rs/zerolog"
)

func init() {
	registerBackend(BackendMalgo, func(log zerolog.Logger) (Backend, error) {
		ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
			log.Debug().Str("backend", BackendMalgo).Msg(message)
		})
		if err != nil {
			return nil, fmt.Errorf("malgo: init context: %w", err)
		}

		return &malgoBackend{
			log: log.With().Str("backend", BackendMalgo).Logger(),
			ctx: ctx,
		}, nil
	})
}

// malgoBackend implements Backend using github.com/gen2brain/malgo (miniaudio).
type malgoBackend struct {
	log zerolog.Logger
	ctx *malgo.AllocatedContext
}

func (b *malgoBackend) Name() string { return BackendMalgo }

func (b *malgoBackend) ListDevices() ([]Device, error) {
	infos, err := b.ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, fmt.Errorf("malgo: list devices: %w", err)
	}

	devices := make([]Device, 0, len(infos))
	for _, info := range infos {
		d := Device{
			ID:        info.ID.String(),
			Name:      info.Name(),
			Backend:   BackendMalgo,
			IsDefault: info.IsDefault != 0,
		}

		// Fill MaxChannels and DefaultSampleRate from the first native format if
		// available; fall back to safe defaults otherwise.
		if len(info.Formats) > 0 {
			for _, f := range info.Formats {
				if int(f.Channels) > d.MaxChannels {
					d.MaxChannels = int(f.Channels)
				}
				if d.DefaultSampleRate == 0 {
					d.DefaultSampleRate = int(f.SampleRate)
				}
			}
		}

		devices = append(devices, d)
	}

	return devices, nil
}

func (b *malgoBackend) OpenSink(cfg SinkConfig) (Sink, error) {
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatF32
	deviceConfig.Playback.Channels = uint32(cfg.Channels)
	deviceConfig.SampleRate = uint32(cfg.SampleRate)

	if cfg.LatencyMs > 0 {
		deviceConfig.PeriodSizeInMilliseconds = uint32(cfg.LatencyMs)
	}

	// If a specific device was requested, decode the hex ID and set it.
	var deviceID malgo.DeviceID
	if cfg.DeviceID != "" {
		raw, err := hex.DecodeString(cfg.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("malgo: decode device ID %q: %w", cfg.DeviceID, err)
		}

		if len(raw) > len(deviceID) {
			return nil, fmt.Errorf("malgo: device ID %q is too long", cfg.DeviceID)
		}

		copy(deviceID[:], raw)
		deviceConfig.Playback.DeviceID = unsafe.Pointer(&deviceID)
	}

	return &malgoSink{
		log:          b.log,
		ctx:          b.ctx.Context,
		deviceConfig: deviceConfig,
		channels:     cfg.Channels,
	}, nil
}

func (b *malgoBackend) Close() error {
	if err := b.ctx.Uninit(); err != nil {
		return fmt.Errorf("malgo: uninit context: %w", err)
	}

	b.ctx.Free()

	return nil
}

// malgoSink is a Sink backed by a malgo device.
type malgoSink struct {
	log          zerolog.Logger
	ctx          malgo.Context
	deviceConfig malgo.DeviceConfig
	channels     int
	device       *malgo.Device
}

func (s *malgoSink) Start(src SampleSource) error {
	callbacks := malgo.DeviceCallbacks{
		Data: func(pOutputSample, _ []byte, frameCount uint32) {
			if len(pOutputSample) == 0 {
				return
			}

			// Reinterpret the output byte buffer as []float32 in-place.
			nSamples := int(frameCount) * s.channels
			out := unsafe.Slice((*float32)(unsafe.Pointer(&pOutputSample[0])), nSamples)
			src.ReadInterleaved(out, s.channels)
		},
	}

	dev, err := malgo.InitDevice(s.ctx, s.deviceConfig, callbacks)
	if err != nil {
		return fmt.Errorf("malgo: init device: %w", err)
	}

	if err := dev.Start(); err != nil {
		dev.Uninit()
		return fmt.Errorf("malgo: start device: %w", err)
	}

	s.device = dev

	return nil
}

func (s *malgoSink) Stop() error {
	if s.device == nil {
		return nil
	}

	s.device.Uninit()
	s.device = nil

	return nil
}

func (s *malgoSink) Channels() int { return s.channels }
