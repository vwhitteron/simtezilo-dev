package internal

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	"github.com/rs/zerolog"
)

type AudioOut struct {
	samples map[string]*beep.Buffer
	log     zerolog.Logger
}

type AudioOutOpts struct {
	AssetDir string
	Logger   zerolog.Logger
}

func NewAudioOutputDevice(opts AudioOutOpts) (*AudioOut, error) {
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
			return nil, fmt.Errorf("decoding thump.ogg: %w", err)
		}

		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/15))

		buffers[sampleName] = beep.NewBuffer(format)
		buffers[sampleName].Append(streamer)
		streamer.Close()
	}

	return &AudioOut{
		samples: buffers,
		log:     opts.Logger,
	}, nil
}

func (a *AudioOut) Play(name string, gain float64) {
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
	physics *physicsTracker
	gain    *float64
}

func NewBumpStream(physics *physicsTracker, gain *float64) BumpStream {
	return BumpStream{
		physics: physics,
		gain:    gain,
	}
}

func (b BumpStream) Stream(samples [][2]float64) (n int, ok bool) {
	sampleLen := len(samples)

	startValue := compressDR(b.physics.last.jerk)
	targetValue := compressDR(b.physics.current.jerk)

	sampleIncr := (targetValue - startValue) / float64(sampleLen)

	if targetValue == startValue {
		for i := range samples {
			samples[i][0] = 0
			samples[i][1] = 0
		}
	}

	currentValue := startValue
	for i := range samples {
		currentValue += sampleIncr
		samples[i][0] = currentValue * *b.gain
		samples[i][1] = currentValue * *b.gain
	}

	return len(samples), true
}

func (b BumpStream) Err() error {
	return nil
}

func compressDR(value float64) float64 {
	scale := 47.5
	isNeg := false

	if value < 0 {
		isNeg = true
		value = -value
	}

	// reduce large signals
	compressed := math.Pow(value, 0.5)

	// don't amplify small signals
	if compressed > value {
		compressed = value
	}

	if isNeg {
		compressed = -compressed
	}

	return compressed / scale
}
