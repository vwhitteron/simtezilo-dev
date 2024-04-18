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

type AudioOut struct {
	samples map[string]*beep.Buffer
	log     zerolog.Logger
}

type AudioOutOpts struct {
	AssetDir string
	Logger   zerolog.Logger
}

func NewAudioOutputDevice(opts AudioOutOpts) (*AudioOut, error) {
	thumpFile := opts.AssetDir + "/audio/thump.ogg"
	thump, err := os.Open(thumpFile)
	if err != nil {
		return nil, fmt.Errorf("reading file %q: %w", thumpFile, err)
	}

	streamers := map[string]beep.StreamSeekCloser{}

	streamer, format, err := vorbis.Decode(thump)
	if err != nil {
		return nil, fmt.Errorf("decoding thump.ogg: %w", err)
	}

	streamers["gearChange"] = streamer

	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))

	buffers := map[string]*beep.Buffer{}

	buffers["gearChange"] = beep.NewBuffer(format)
	buffers["gearChange"].Append(streamer)
	streamer.Close()

	return &AudioOut{
		samples: buffers,
		log:     opts.Logger,
	}, nil
}

func (a *AudioOut) Play(name string, gain float64) {
	if _, ok := a.samples[name]; !ok {
		a.log.Info().Msgf("unable to play unknown audio stream %q", name)
	}

	streamer := a.samples[name].Streamer(0, a.samples[name].Len())

	sample := &effects.Volume{
		Streamer: streamer,
		Base:     10,
		Volume:   gain / 10,
	}

	speaker.Play(sample)
}
