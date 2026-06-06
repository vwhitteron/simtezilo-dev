// Package local provides a PitRadio implementation that plays pit-radio audio
// on a local output device (e.g. a Bluetooth headset) via the app/audio
// abstraction, independently of the Discord voice path.
package local

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/codec"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio/tts"
)

// outputChannels is the channel count used for local pit-radio playback. Pit
// radio is stereo speech/audio, so two channels suffice regardless of the
// haptic channel count.
const outputChannels = 2

// playbackTail is added after a clip's natural duration so the device buffer can
// drain before the stream is stopped.
const playbackTail = 300 * time.Millisecond

// leadInSilence is a short run of silence prepended to every clip. The device is
// (re)started for each message, so the output route (e.g. a Bluetooth link, or
// CoreAudio's audio unit) may still be waking when the first samples are pulled.
// Playing silence first lets the route stabilise before speech begins, avoiding
// the garbled/clipped start that is otherwise heard on the first message.
const leadInSilence = 250 * time.Millisecond

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
}

// Output plays pit-radio messages on a local audio device.
type Output struct {
	device       string
	deviceName   string
	sampleRate   int
	deviceFn     func() string
	deviceNameFn func() string
	sampleRateFn func() int
	messageGap   time.Duration
	queue        chan pitradio.Message
	log          zerolog.Logger

	// mu guards backend and the pending-backend swap state. backend is the sink
	// factory in use; a swap requested via SetBackend is parked in pendingBackend
	// and applied by the task goroutine (applyPendingBackend) so the backend is
	// never closed while a play() call is mid-clip on it.
	mu             sync.Mutex
	backend        audio.Backend
	pendingBackend audio.Backend
	hasPending     bool

	ctx    context.Context //nolint:containedctx // Context for managing lifecycle
	cancel context.CancelFunc
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
		messageGap:   config.MessageGap,
		queue:        make(chan pitradio.Message, 100),
		log:          config.Logger.With().Str("component", "pitradio local").Logger(),
		ctx:          ctx,
		cancel:       cancel,
	}, nil
}

// BackgroundTask processes queued messages until Close is called.
func (o *Output) BackgroundTask() {
	for {
		select {
		case <-o.ctx.Done():
			return
		case msg := <-o.queue:
			// Apply any queued backend swap before playing, on this goroutine, so
			// the backend is only ever mutated between messages and never while a
			// sink is open on it.
			o.applyPendingBackend()

			o.process(msg)

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

// SetBackend queues a swap to the given audio backend. The swap takes effect
// before the next message is played (see applyPendingBackend); the previously
// active backend is closed at that point. It is safe to call concurrently with
// playback. A superseded pending backend (SetBackend called twice before a
// message plays) is closed immediately.
func (o *Output) SetBackend(backend audio.Backend) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.hasPending && o.pendingBackend != nil {
		_ = o.pendingBackend.Close()
	}

	o.pendingBackend = backend
	o.hasPending = true
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

// Close stops the background task and releases the backend (and any pending one).
func (o *Output) Close() error {
	if o.cancel != nil {
		o.cancel()
	}

	o.mu.Lock()
	backend := o.backend
	o.backend = nil
	pending := o.pendingBackend
	o.pendingBackend = nil
	o.hasPending = false
	o.mu.Unlock()

	if pending != nil {
		_ = pending.Close()
	}

	if backend != nil {
		return backend.Close()
	}

	return nil
}

// applyPendingBackend installs any queued backend, closing the one it replaces.
// It runs only on the task goroutine.
func (o *Output) applyPendingBackend() {
	o.mu.Lock()

	if !o.hasPending {
		o.mu.Unlock()

		return
	}

	old := o.backend
	o.backend = o.pendingBackend
	o.pendingBackend = nil
	o.hasPending = false

	o.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}

	o.log.Info().Str("backend", o.backend.Name()).Msg("pit-radio audio backend switched")
}

// currentBackend returns the active backend under the lock.
func (o *Output) currentBackend() audio.Backend { //nolint:ireturn // returns Backend interface by design; concrete type is private
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.backend
}

// process decodes a message to PCM and plays it on the configured device.
func (o *Output) process(message pitradio.Message) {
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

	err = o.play(pcm)
	if err != nil {
		o.log.Error().Err(err).Msg("play pit-radio audio")
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

// play resamples the clip into a flat output buffer up front, then streams it to
// the device, blocking until the clip has finished. Pre-rendering keeps the
// realtime audio callback free of resampling and allocation, so it cannot miss
// its deadline (and glitch) while the runtime is still cold on the first few
// messages after startup.
func (o *Output) play(pcm codec.PCMFloat64) error {
	// Resolve the device and sample rate at play time so a runtime change (e.g.
	// the user picking a different output device in the web UI) takes effect on
	// the next message without recreating the Output.
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
		return errors.New("no audio backend")
	}

	// Resolve by name (stable across backends and portaudio index reshuffles),
	// with the stored ID as a tiebreaker.
	device := audio.ResolveOutputDevice(backend, name, deviceID)

	sink, err := backend.OpenSink(audio.SinkConfig{
		DeviceID:   device,
		Channels:   outputChannels,
		SampleRate: rate,
	})
	if err != nil {
		return err
	}

	defer func() { _ = sink.Stop() }()

	rendered := o.render(pcm, sink.Channels(), rate)

	err = sink.Start(&bufferSource{buf: rendered, channels: sink.Channels()})
	if err != nil {
		return err
	}

	// Wait for the clip to play out (plus a short drain tail).
	frames := len(rendered) / sink.Channels()
	dur := time.Duration(float64(frames) / float64(rate) * float64(time.Second))

	select {
	case <-o.ctx.Done():
	case <-time.After(dur + playbackTail):
	}

	return nil
}

// render resamples pcm to the given output rate and channel count, returning a
// flat interleaved float32 buffer with leadInSilence frames of silence prepended.
func (o *Output) render(pcm codec.PCMFloat64, channels, rate int) []float32 {
	srcChannels := max(pcm.Channels(), 1)

	srcFrames := pcm.Len() / srcChannels
	clipFrames := int(math.Ceil(float64(srcFrames) * float64(rate) / float64(pcm.SampleRate())))
	leadFrames := int(float64(rate) * leadInSilence.Seconds())

	buf := make([]float32, (leadFrames+clipFrames)*channels)

	// The lead-in region is left as zeros; render the clip after it. Pull the
	// resampler in blocks rather than one giant read so memory use stays bounded.
	source := audio.NewResamplingSource(newPCMSource(pcm), pcm.SampleRate(), rate, channels)

	out := buf[leadFrames*channels:]
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

// pullBlockSamples bounds how many frames render pulls from the resampler per
// iteration.
const pullBlockSamples = 4096

// bufferSource streams a pre-rendered interleaved float32 buffer, emitting
// silence once exhausted. Its ReadInterleaved does no allocation, so it is safe
// to run on the realtime audio callback.
type bufferSource struct {
	buf      []float32
	channels int
	pos      int // sample offset into buf
}

func (b *bufferSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	n := copy(out, b.buf[b.pos:])
	b.pos += n

	for i := n; i < len(out); i++ {
		out[i] = 0
	}

	return len(out) / channels, true
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
