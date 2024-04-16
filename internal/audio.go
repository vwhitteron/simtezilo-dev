package internal

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
)

type AudioOut struct {
	samples map[string]*beep.Buffer
}

func NewAudioOutputDevice(assetDir string) (*AudioOut, error) {
	thumpFile := assetDir + "/audio/thump.ogg"
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
	}, nil
}

func (a *AudioOut) Play(name string, gain float64) {
	if _, ok := a.samples[name]; !ok {
		log.Printf("unable to play unknown audio stream %q", name)
	}

	streamer := a.samples[name].Streamer(0, a.samples[name].Len())

	sample := &effects.Volume{
		Streamer: streamer,
		Base:     10,
		Volume:   gain / 10,
	}

	speaker.Play(sample)
}
