// Package local provides a PitRadio implementation that plays pit-radio audio
// on a local output device (e.g. a Bluetooth headset) via the app/audio
// abstraction, independently of the Discord voice path.
package local

import (
	"context"
	"errors"
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

// Config holds the options for creating a local pit-radio Output.
type Config struct {
	Backend    audio.Backend // Audio backend used to open the playback sink
	Device     string        // Backend device ID ("" selects the default device)
	SampleRate int           // Output sample rate in Hz
	MessageGap time.Duration // Minimum delay between consecutive messages
	Logger     zerolog.Logger
}

// Output plays pit-radio messages on a local audio device.
type Output struct {
	backend    audio.Backend
	device     string
	sampleRate int
	messageGap time.Duration
	queue      chan pitradio.Message
	log        zerolog.Logger

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
		backend:    config.Backend,
		device:     config.Device,
		sampleRate: config.SampleRate,
		messageGap: config.MessageGap,
		queue:      make(chan pitradio.Message, 100),
		log:        config.Logger.With().Str("component", "pitradio local").Logger(),
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// BackgroundTask processes queued messages until Close is called.
func (o *Output) BackgroundTask() {
	for {
		select {
		case <-o.ctx.Done():
			return
		case msg := <-o.queue:
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

// Send enqueues a message for local playback.
func (o *Output) Send(message pitradio.Message) error {
	select {
	case o.queue <- message:
		return nil
	default:
		return errors.New("local pit-radio queue full")
	}
}

// Close stops the background task and releases the backend.
func (o *Output) Close() error {
	if o.cancel != nil {
		o.cancel()
	}

	if o.backend != nil {
		return o.backend.Close()
	}

	return nil
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

	if err := o.play(pcm); err != nil {
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

// play streams decoded PCM to the device, blocking until the clip has finished.
func (o *Output) play(pcm codec.PCMFloat64) error {
	sink, err := o.backend.OpenSink(audio.SinkConfig{
		DeviceID:   o.device,
		Channels:   outputChannels,
		SampleRate: o.sampleRate,
	})
	if err != nil {
		return err
	}

	defer func() { _ = sink.Stop() }()

	source := audio.NewResamplingSource(
		newPCMSource(pcm),
		pcm.SampleRate(),
		o.sampleRate,
		sink.Channels(),
	)

	if err := sink.Start(source); err != nil {
		return err
	}

	// Wait for the clip to play out (plus a short drain tail).
	srcChannels := pcm.Channels()
	if srcChannels < 1 {
		srcChannels = 1
	}

	frames := pcm.Len() / srcChannels
	dur := time.Duration(float64(frames) / float64(pcm.SampleRate()) * float64(time.Second))

	select {
	case <-o.ctx.Done():
	case <-time.After(dur + playbackTail):
	}

	return nil
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
	srcChannels := pcm.Channels()
	if srcChannels < 1 {
		srcChannels = 1
	}

	return &pcmSource{
		samples:     pcm.Samples(),
		srcChannels: srcChannels,
	}
}

func (p *pcmSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels
	srcFrames := len(p.samples) / p.srcChannels

	for f := range frames {
		for c := range channels {
			var v float64

			if p.pos < srcFrames {
				sc := c
				if sc >= p.srcChannels {
					sc = p.srcChannels - 1
				}

				v = p.samples[p.pos*p.srcChannels+sc]
			}

			buf[f*channels+c] = float32(v)
		}

		p.pos++
	}

	return frames, true
}
