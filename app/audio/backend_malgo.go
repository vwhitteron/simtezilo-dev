//go:build malgo

package audio

import (
	"encoding/hex"
	"fmt"
	"runtime"
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

		// Enumeration via Devices() only returns ID/name/default; the native
		// data formats (and thus channel count and sample rate) are left empty.
		// Query each device individually to populate them.
		formats := info.Formats
		if len(formats) == 0 {
			if full, err := b.ctx.DeviceInfo(malgo.Playback, info.ID, malgo.Shared); err == nil {
				formats = full.Formats
			} else {
				b.log.Debug().Err(err).Str("device", d.Name).Msg("malgo: query device info")
			}
		}

		// Fill MaxChannels and DefaultSampleRate from the native formats if
		// available; fall back to safe defaults otherwise.
		for _, f := range formats {
			if int(f.Channels) > d.MaxChannels {
				d.MaxChannels = int(f.Channels)
			}
			if d.DefaultSampleRate == 0 {
				d.DefaultSampleRate = int(f.SampleRate)
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

	// If a specific device was requested, decode the hex ID. The pointer into
	// the malgo.DeviceID is wired up (and pinned) in Start, immediately around
	// the InitDevice call, to satisfy cgo's pointer-passing rules.
	var deviceID *malgo.DeviceID
	if cfg.DeviceID != "" {
		raw, err := hex.DecodeString(cfg.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("malgo: decode device ID %q: %w", cfg.DeviceID, err)
		}

		var id malgo.DeviceID
		if len(raw) > len(id) {
			return nil, fmt.Errorf("malgo: device ID %q is too long", cfg.DeviceID)
		}

		copy(id[:], raw)
		deviceID = &id
	}

	return &malgoSink{
		log:          b.log,
		ctx:          b.ctx.Context,
		deviceConfig: deviceConfig,
		deviceID:     deviceID,
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
	deviceID     *malgo.DeviceID
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

	// miniaudio copies the device ID during InitDevice, so it only needs to
	// stay put for the duration of that call. Pin it across InitDevice to
	// satisfy cgo's rule against passing Go pointers to unpinned Go pointers.
	if s.deviceID != nil {
		var pinner runtime.Pinner
		pinner.Pin(s.deviceID)
		s.deviceConfig.Playback.DeviceID = unsafe.Pointer(s.deviceID)
		defer pinner.Unpin()
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
