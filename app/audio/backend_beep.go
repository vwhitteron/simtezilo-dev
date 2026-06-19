package audio

import (
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
)

// beepChannels is fixed: beep's speaker outputs interleaved stereo only.
const beepChannels = 2

// beep's speaker wraps a global oto context that can only be created once per
// process and is never released (oto cannot recreate it). speaker.Init therefore
// errors on a second call ("speaker cannot be initialized more than once"), which
// would break a live backend switch away from beep and back. We init it at most
// once and reuse it thereafter; Stop uses speaker.Clear (not Close) so the player
// keeps running and a later Play resumes output. The sample rate and buffer size
// are fixed at first init for the process lifetime.
var (
	speakerMu       sync.Mutex      //nolint:gochecknoglobals // process-global; oto context cannot be recreated
	speakerInited   bool            //nolint:gochecknoglobals // process-global; oto context cannot be recreated
	speakerInitRate beep.SampleRate //nolint:gochecknoglobals // process-global; oto context cannot be recreated
)

func init() { //nolint:gochecknoinits // registers the beep backend factory; init is the correct pattern for optional backend registration
	registerBackend(BackendBeep, func(log zerolog.Logger) (Backend, error) {
		return &beepBackend{log: log.With().Str("backend", BackendBeep).Logger()}, nil
	})
}

// beepBackend wraps the legacy beep/oto speaker path. beep exposes a single
// global stereo output device, so it supports one default device at one sample
// rate. It exists to preserve the original behaviour as the zero-extra-dependency
// default; use portaudio for multichannel or multi-device output.
type beepBackend struct {
	log zerolog.Logger
}

func (b *beepBackend) Name() string { return BackendBeep }

func (b *beepBackend) ListDevices() ([]Device, error) {
	return []Device{{
		ID:          "",
		Name:        "System default (stereo)",
		DisplayName: "System default (stereo)",
		Type:        DeviceBuiltin,
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
	speakerMu.Lock()
	defer speakerMu.Unlock()

	if !speakerInited {
		err := speaker.Init(s.rate, s.bufSize)
		if err != nil {
			return err
		}

		speakerInited = true
		speakerInitRate = s.rate
	} else if s.rate != speakerInitRate {
		// The oto context's rate is fixed at first init and cannot change; output
		// would play at the original rate (wrong pitch) if we proceeded silently.
		s.log.Warn().
			Int("requested", int(s.rate)).
			Int("active", int(speakerInitRate)).
			Msg("beep speaker sample rate is fixed for the process lifetime; ignoring change (restart to apply)")
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
	sampleCount := len(samples)

	if cap(a.buf) < sampleCount*beepChannels {
		a.buf = make([]float32, sampleCount*beepChannels)
	}

	buf := a.buf[:sampleCount*beepChannels]

	frames, readOk := a.src.ReadInterleaved(buf, beepChannels)

	for frameIdx := range frames {
		samples[frameIdx][0] = float64(buf[frameIdx*beepChannels])
		samples[frameIdx][1] = float64(buf[frameIdx*beepChannels+1])
	}

	for frameIdx := frames; frameIdx < sampleCount; frameIdx++ {
		samples[frameIdx][0] = 0
		samples[frameIdx][1] = 0
	}

	return sampleCount, readOk
}

func (a *beepStreamerAdapter) Err() error { return nil }
