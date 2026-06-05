package audio

import (
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
)

// beepChannels is fixed: beep's speaker outputs interleaved stereo only.
const beepChannels = 2

func init() {
	registerBackend(BackendBeep, func(log zerolog.Logger) (Backend, error) {
		return &beepBackend{log: log.With().Str("backend", BackendBeep).Logger()}, nil
	})
}

// beepBackend wraps the legacy beep/oto speaker path. beep exposes a single
// global stereo output device, so it supports one default device at one sample
// rate. It exists to preserve the original behaviour as the zero-extra-dependency
// default; use malgo or portaudio for multichannel or multi-device output.
type beepBackend struct {
	log zerolog.Logger
}

func (b *beepBackend) Name() string { return BackendBeep }

func (b *beepBackend) ListDevices() ([]Device, error) {
	return []Device{{
		ID:          "",
		Name:        "System default (stereo)",
		Backend:     BackendBeep,
		MaxChannels: beepChannels,
		IsDefault:   true,
	}}, nil
}

func (b *beepBackend) OpenSink(cfg SinkConfig) (Sink, error) {
	rate := beep.SampleRate(cfg.SampleRate)

	bufSize := rate.N(time.Duration(cfg.LatencyMs) * time.Millisecond)
	if bufSize <= 0 {
		bufSize = rate.N(time.Second / 15)
	}

	return &beepSink{
		rate:    rate,
		bufSize: bufSize,
		log:     b.log,
	}, nil
}

func (b *beepBackend) Close() error { return nil }

type beepSink struct {
	rate    beep.SampleRate
	bufSize int
	log     zerolog.Logger
}

func (s *beepSink) Start(src SampleSource) error {
	if err := speaker.Init(s.rate, s.bufSize); err != nil {
		return err
	}

	speaker.Play(&beepStreamerAdapter{src: src})

	return nil
}

func (s *beepSink) Stop() error {
	speaker.Clear()

	return nil
}

func (s *beepSink) Channels() int { return beepChannels }

// beepStreamerAdapter adapts an interleaved float32 SampleSource to beep's
// [][2]float64 Streamer interface.
type beepStreamerAdapter struct {
	src SampleSource
	buf []float32
}

func (a *beepStreamerAdapter) Stream(samples [][2]float64) (int, bool) {
	n := len(samples)

	if cap(a.buf) < n*beepChannels {
		a.buf = make([]float32, n*beepChannels)
	}

	buf := a.buf[:n*beepChannels]

	frames, ok := a.src.ReadInterleaved(buf, beepChannels)

	for i := range frames {
		samples[i][0] = float64(buf[i*beepChannels])
		samples[i][1] = float64(buf[i*beepChannels+1])
	}

	for i := frames; i < n; i++ {
		samples[i][0] = 0
		samples[i][1] = 0
	}

	return n, ok
}

func (a *beepStreamerAdapter) Err() error { return nil }
