package internal

import (
	"fmt"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	"github.com/rs/zerolog"
)

type AudioOutDevice struct {
	samples map[string]*beep.Buffer
	log     zerolog.Logger
}

type AudioOutDeviceOpts struct {
	AssetDir string
	Logger   zerolog.Logger
}

func NewAudioOutputDevice(opts AudioOutDeviceOpts) (*AudioOutDevice, error) {
	audioSamples := []string{
		"gearchange",
	}

	buffers := map[string]*beep.Buffer{}

	for _, sampleName := range audioSamples {
		sampleFile := opts.AssetDir + "/audio/" + sampleName + ".ogg"
		sampleData, err := os.Open(sampleFile)
		if err != nil {
			return nil, fmt.Errorf("reading file %q: %w", sampleFile, err)
		}

		streamer, format, err := vorbis.Decode(sampleData)
		if err != nil {
			return nil, fmt.Errorf("decoding %s.ogg: %w", sampleName, err)
		}

		bufferSize := format.SampleRate.N(time.Second / 15)
		speaker.Init(format.SampleRate, bufferSize)

		opts.Logger.Debug().
			Int("sample rate", int(format.SampleRate)).
			Int("buffer size", bufferSize).
			Msgf("loaded audio stream %q", sampleName)

		buffers[sampleName] = beep.NewBuffer(format)
		buffers[sampleName].Append(streamer)
		streamer.Close()
	}

	return &AudioOutDevice{
		samples: buffers,
		log:     opts.Logger,
	}, nil
}

func (a *AudioOutDevice) Play(name string, gain float64) {
	if _, ok := a.samples[name]; !ok {
		a.log.Info().Msgf("unable to play unknown audio stream %q", name)
		return
	}

	if gain >= maxGain {
		gain = maxGain
	}

	streamer := a.samples[name].Streamer(0, a.samples[name].Len())

	sample := &effects.Volume{
		Streamer: streamer,
		Base:     10,
		Volume:   gain / 10,
	}

	speaker.Play(sample)
}

type BumpStream struct {
	audioBuffer *audioBuffer
	physics     *physicsTracker
	gain        *float64
	enabled     bool
}

func NewBumpStream(buffer *audioBuffer, physics *physicsTracker, gain *float64, enabled bool) BumpStream {
	return BumpStream{
		audioBuffer: buffer,
		physics:     physics,
		gain:        gain,
		enabled:     enabled,
	}
}

func (b BumpStream) Stream(samples [][2]float64) (n int, ok bool) {
	buffer := *b.audioBuffer

	// fmt.Printf("Buffer: %v\n", buffer.buffer)

	for i := range samples {
		sample := buffer.buffer[i] * *b.gain
		samples[i][0] = sample
		samples[i][1] = sample

		// buffer.buffer[i] = 0
	}

	return len(samples), true
}

func (b BumpStream) Err() error {
	return nil
}
