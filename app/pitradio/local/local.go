// Package local provides a PitRadio implementation that plays pit-radio audio
// on a local output device (e.g. a Bluetooth headset) via the app/audio
// abstraction, independently of the Discord voice path.
package local

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/codec"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio/tts"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

// outputChannels is the channel count used for local pit-radio playback. Pit
// radio is stereo speech/audio, so two channels suffice regardless of the
// haptic channel count.
const outputChannels = 2

// playbackTail is added after a clip's natural duration so the device buffer can
// drain before the next clip (or silence) is fed.
const playbackTail = 300 * time.Millisecond

// sinkPollInterval is how often the background task re-checks the resolved output
// device/rate so a runtime change (e.g. picking a different device in the web UI)
// reopens the persistent sink.
const sinkPollInterval = 2 * time.Second

// Config holds the options for creating a local pit-radio Output.
type Config struct {
	Backend    audio.Backend // Audio backend used to open the playback sink
	Device     string        // Backend device ID ("" selects the default device)
	DeviceName string        // Human-readable device name (stable selection key)
	SampleRate int           // Output sample rate in Hz
	MessageGap time.Duration // Minimum delay between consecutive messages
	Logger     zerolog.Logger

	// DeviceFn, DeviceNameFn and SampleRateFn, when non-nil, are consulted before
	// each clip is played so the output device and sample rate can be changed at
	// runtime without recreating the Output. A sink is opened fresh per message,
	// so the next message simply picks up the new values. They fall back to the
	// static Device/DeviceName/SampleRate fields when nil (or when SampleRateFn
	// returns <= 0). The device is resolved by name first (see
	// audio.ResolveOutputDevice), with the ID as a tiebreaker.
	DeviceFn     func() string
	DeviceNameFn func() string
	SampleRateFn func() int

	// VolumeFn, when non-nil, is read before each clip is scaled so the output
	// level can be changed at runtime. It returns the playback volume as a
	// percentage (0-100); 100 (or a nil VolumeFn) leaves the audio unattenuated.
	VolumeFn func() int

	// OnSinkActive, when non-nil, is called when the persistent playback sink is
	// (re)opened on a device (active=true) or torn down (active=false), with the
	// resolved device name. The app uses it to drive the Bluetooth audio bridge
	// lifecycle (the bridge needs this sink — its loopback master — open).
	OnSinkActive func(deviceName string, active bool)
}

// Output plays pit-radio messages on a local audio device.
type Output struct {
	device       string
	deviceName   string
	sampleRate   int
	deviceFn     func() string
	deviceNameFn func() string
	sampleRateFn func() int
	volumeFn     func() int
	messageGap   time.Duration
	queue        chan pitradio.Message
	log          zerolog.Logger
	onSinkActive func(deviceName string, active bool)

	// backend is the sink factory. It is set once at construction; the backend
	// selection is restart-required, so it never changes over the lifetime of an
	// Output.
	backend audio.Backend

	// Persistent sink state. The sink is held open continuously (streaming silence
	// when idle) so downstream routes that need a live stream — notably the
	// snd-aloop Bluetooth bridge, whose capture is slaved to this playback side —
	// stay up. Touched only from BackgroundTask, so it needs no locking.
	sink       audio.Sink
	source     *streamSource
	openDevice string
	openName   string
	openRate   int

	ctx    context.Context //nolint:containedctx // Context for managing lifecycle
	cancel context.CancelFunc

	// started is set when BackgroundTask begins; done is closed when it returns
	// (and the sink is fully torn down). Close waits on done only when the task
	// actually started, so the PortAudio stream is aborted/closed before
	// portaudio.Terminate runs, while a never-started Output closes immediately.
	started atomic.Bool
	done    chan struct{}
}

// New creates a local pit-radio Output.
func New(config Config) (*Output, error) {
	if config.Backend == nil {
		return nil, errors.New("audio backend is required")
	}

	if config.SampleRate <= 0 {
		config.SampleRate = 48000
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Output{
		backend:      config.Backend,
		device:       config.Device,
		deviceName:   config.DeviceName,
		sampleRate:   config.SampleRate,
		deviceFn:     config.DeviceFn,
		deviceNameFn: config.DeviceNameFn,
		sampleRateFn: config.SampleRateFn,
		volumeFn:     config.VolumeFn,
		messageGap:   config.MessageGap,
		queue:        make(chan pitradio.Message, 100),
		log:          config.Logger.With().Str("component", "pitradio local").Logger(),
		onSinkActive: config.OnSinkActive,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
	}, nil
}

// BackgroundTask holds the output sink open and plays queued messages until
// Close is called. The sink is kept open continuously (streaming silence when
// idle) so the snd-aloop Bluetooth bridge has a live master to slave to; the
// ticker reopens it when the resolved device/rate changes.
func (o *Output) BackgroundTask() {
	o.started.Store(true)

	defer close(o.done)
	defer o.closeSink()

	ticker := time.NewTicker(sinkPollInterval)
	defer ticker.Stop()

	o.ensureSink()

	for {
		select {
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			o.ensureSink()
		case msg := <-o.queue:
			o.ensureSink()
			o.playMessage(msg)

			// Pace consecutive messages.
			if o.messageGap > 0 {
				select {
				case <-o.ctx.Done():
					return
				case <-time.After(o.messageGap):
				}
			}
		}
	}
}

// Send enqueues a message for local playback.
func (o *Output) Send(message pitradio.Message) error {
	select {
	case o.queue <- message:
		return nil
	default:
		return errors.New("local pit-radio queue full")
	}
}

// Close stops the background task and releases the backend. It waits for
// BackgroundTask to finish tearing down the sink before releasing the backend,
// so the PortAudio stream is aborted/closed before portaudio.Terminate runs (a
// terminate racing an open stream can hang). A short timeout guards against the
// task never having been started.
func (o *Output) Close() error {
	if o.cancel != nil {
		o.cancel()
	}

	if o.started.Load() {
		select {
		case <-o.done:
		case <-time.After(5 * time.Second):
			o.log.Warn().Msg("timed out waiting for pit-radio background task to stop")
		}
	}

	backend := o.backend
	o.backend = nil

	if backend != nil {
		return backend.Close()
	}

	return nil
}

// currentBackend returns the active backend. It is set once at construction and
// never swapped, so no synchronisation is needed.
func (o *Output) currentBackend() audio.Backend {
	return o.backend
}

// playMessage decodes a message to PCM and plays it through the persistent sink,
// blocking until the clip has played out so consecutive messages don't overlap.
func (o *Output) playMessage(message pitradio.Message) {
	mp3, err := o.decode(message)
	if err != nil {
		o.log.Error().Err(err).Msg("decode pit-radio message")

		return
	}

	pcm, err := mp3.ToPCMFloat64()
	if err != nil {
		o.log.Error().Err(err).Msg("decode MP3 to PCM")

		return
	}

	if o.source == nil {
		o.log.Warn().Msg("pit-radio sink not open; dropping message")

		return
	}

	rendered := o.render(pcm, o.source.channels, o.openRate)
	o.applyVolume(rendered)
	o.source.enqueue(rendered)

	// Wait for the clip to play out (plus a short drain tail) before the next.
	frames := len(rendered) / o.source.channels
	dur := time.Duration(float64(frames) / float64(o.openRate) * float64(time.Second))

	select {
	case <-o.ctx.Done():
	case <-time.After(dur + playbackTail):
	}
}

// ensureSink (re)opens the persistent sink when it isn't open or the resolved
// device/rate has changed. Best-effort: on failure the sink is left closed and
// the next call retries. The device is resolved by name (stable across backends
// and portaudio index reshuffles), with the stored ID as a tiebreaker.
func (o *Output) ensureSink() {
	deviceID := o.device
	if o.deviceFn != nil {
		deviceID = o.deviceFn()
	}

	name := o.deviceName
	if o.deviceNameFn != nil {
		name = o.deviceNameFn()
	}

	rate := o.sampleRate
	if o.sampleRateFn != nil {
		if r := o.sampleRateFn(); r > 0 {
			rate = r
		}
	}

	backend := o.currentBackend()
	if backend == nil {
		return
	}

	device := audio.ResolveOutputDevice(backend, name, deviceID)

	if o.sink != nil && device == o.openDevice && rate == o.openRate {
		return
	}

	o.closeSink()

	src := &streamSource{channels: outputChannels}

	sink, err := backend.OpenSink(audio.SinkConfig{
		DeviceID:   device,
		Channels:   outputChannels,
		SampleRate: rate,
	})
	if err != nil {
		o.log.Error().Err(err).Str("device", name).Msg("open pit-radio sink")

		return
	}

	src.channels = sink.Channels()

	err = sink.Start(src)
	if err != nil {
		o.log.Error().Err(err).Msg("start pit-radio sink")

		_ = sink.Stop()

		return
	}

	o.sink = sink
	o.source = src
	o.openDevice = device
	o.openName = name
	o.openRate = rate

	if o.onSinkActive != nil {
		o.onSinkActive(name, true)
	}
}

// closeSink stops and releases the persistent sink if open.
func (o *Output) closeSink() {
	if o.sink == nil {
		return
	}

	_ = o.sink.Stop()
	o.sink = nil
	o.source = nil

	name := o.openName
	o.openDevice = ""
	o.openName = ""
	o.openRate = 0

	if o.onSinkActive != nil {
		o.onSinkActive(name, false)
	}
}

// decode resolves a message to MP3 audio, synthesising speech for text messages.
func (o *Output) decode(message pitradio.Message) (*codec.MP3, error) {
	switch message.MessageType {
	case pitradio.AudioMessage:
		if len(message.Audio) == 0 {
			return nil, errors.New("audio message has no data")
		}

		return codec.NewMP3(message.Audio), nil
	case pitradio.TextMessage:
		mp3, err := tts.TextToSpeech(message)
		if err != nil {
			return nil, err
		}

		return &mp3, nil
	default:
		return nil, errors.New("unknown message type")
	}
}

// render resamples pcm to the given output rate and channel count, returning a
// flat interleaved float32 buffer. No lead-in silence is needed: the sink is
// held open continuously, so the output route is already warm when a clip plays.
func (o *Output) render(pcm codec.PCMFloat64, channels, rate int) []float32 {
	srcChannels := max(pcm.Channels(), 1)

	srcFrames := pcm.Len() / srcChannels
	clipFrames := int(math.Ceil(float64(srcFrames) * float64(rate) / float64(pcm.SampleRate())))

	buf := make([]float32, clipFrames*channels)

	// Pull the resampler in blocks rather than one giant read so memory use stays
	// bounded.
	source := audio.NewResamplingSource(newPCMSource(pcm), pcm.SampleRate(), rate, channels)

	out := buf
	for len(out) > 0 {
		chunk := out
		if len(chunk) > pullBlockSamples*channels {
			chunk = chunk[:pullBlockSamples*channels]
		}

		n, ok := source.ReadInterleaved(chunk, channels)
		out = out[n*channels:]

		if !ok {
			break
		}
	}

	return buf
}

// volumeFloorDB is the attenuation applied at the bottom of the volume slider
// (just above 0%). A logarithmic taper cannot reach true silence (0 gain is
// -∞ dB), so the slider spans 0 dB (100%) down to this floor, with 0% forced to
// silence.
const volumeFloorDB = -40.0

// applyVolume scales the interleaved buffer in place by the configured playback
// volume. The volume is read live per clip via volumeFn (percentage 0-100), so a
// change in the UI takes effect on the next message. A nil volumeFn or a value of
// 100 leaves the audio untouched; out-of-range values are clamped.
//
// The taper is logarithmic (perceptual): the slider position maps linearly onto
// decibels between 0 dB at 100% and volumeFloorDB just above 0%, which is then
// converted back to a linear amplitude. This matches how loudness is perceived,
// so equal slider movements give roughly equal changes in loudness.
func (o *Output) applyVolume(buf []float32) {
	if o.volumeFn == nil {
		return
	}

	volume := min(100, max(0, o.volumeFn()))
	if volume == 100 {
		return
	}

	if volume == 0 {
		for i := range buf {
			buf[i] = 0
		}

		return
	}

	// Map the slider position onto decibels (100% -> 0 dB, ~0% -> floor), then
	// back to a linear amplitude via the shared dB conversion.
	db := volumeFloorDB * (1 - float64(volume)/100)
	gain := float32(signal.GainToAmplitudeRatio(db))

	for i := range buf {
		buf[i] *= gain
	}
}

// pullBlockSamples bounds how many frames render pulls from the resampler per
// iteration.
const pullBlockSamples = 4096

// streamSource is the persistent source feeding the held-open sink. It emits the
// current clip buffer then silence, and is fed new clips via enqueue. Its
// ReadInterleaved does no allocation and only takes a short mutex, so it is safe
// to run on the realtime audio callback.
type streamSource struct {
	mu       sync.Mutex
	buf      []float32
	pos      int // sample offset into buf
	channels int
}

// ReadInterleaved implements audio.SampleSource. It copies the current clip
// buffer into out, then emits silence once the clip is exhausted. It is safe to
// call from the realtime audio callback.
func (s *streamSource) ReadInterleaved(out []float32, channels int) (n int, ok bool) {
	s.mu.Lock()

	n = 0
	if s.buf != nil {
		n = copy(out, s.buf[s.pos:])
		s.pos += n

		if s.pos >= len(s.buf) {
			s.buf = nil
			s.pos = 0
		}
	}

	s.mu.Unlock()

	for i := n; i < len(out); i++ {
		out[i] = 0
	}

	return len(out) / channels, true
}

// enqueue swaps in a new clip buffer, replacing any still playing.
func (s *streamSource) enqueue(buf []float32) {
	s.mu.Lock()
	s.buf = buf
	s.pos = 0
	s.mu.Unlock()
}

// pcmSource adapts interleaved float64 PCM to an audio.SampleSource, mapping the
// source channels onto the requested output channels and emitting silence once
// the clip is exhausted.
type pcmSource struct {
	samples     []float64
	srcChannels int
	pos         int // current source frame index
}

func newPCMSource(pcm codec.PCMFloat64) *pcmSource {
	srcChannels := max(pcm.Channels(), 1)

	return &pcmSource{
		samples:     pcm.Samples(),
		srcChannels: srcChannels,
	}
}

func (p *pcmSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels
	srcFrames := len(p.samples) / p.srcChannels

	for frameIdx := range frames {
		for channelIdx := range channels {
			var sample float64

			if p.pos < srcFrames {
				sc := channelIdx
				if sc >= p.srcChannels {
					sc = p.srcChannels - 1
				}

				sample = p.samples[p.pos*p.srcChannels+sc]
			}

			buf[frameIdx*channels+channelIdx] = float32(sample)
		}

		p.pos++
	}

	return frames, true
}
