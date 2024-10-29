package synth

import (
	"github.com/gopxl/beep"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/physics"
)

const maxGain = 0

type OutputDevice struct {
	samples map[string]*beep.Buffer
	log     zerolog.Logger
}

type SynthOutDeviceOpts struct {
	AssetDir string
	Logger   zerolog.Logger
}

func NewOutputDevice(opts SynthOutDeviceOpts) (*OutputDevice, error) {
	buffers := map[string]*beep.Buffer{}

	return &OutputDevice{
		samples: buffers,
		log:     opts.Logger,
	}, nil
}

type BumpStream struct {
	synthBuffer *Buffer
	physics     *physics.PhysicsTracker
	mixer       *Mixer
}

func NewBumpStream(buffer *Buffer, physics *physics.PhysicsTracker, mixer *Mixer) BumpStream {
	return BumpStream{
		synthBuffer: buffer,
		physics:     physics,
		mixer:       mixer,
	}
}

func (b BumpStream) Stream(samples [][2]float64) (n int, ok bool) {
	buffer := b.synthBuffer.Read(len(samples))

	for i := range samples {
		sample := buffer[i] * b.mixer.output
		samples[i][0] = sample
		samples[i][1] = sample
	}

	b.synthBuffer.ShiftBuffer(len(samples))

	return len(samples), true
}

func (b BumpStream) Err() error {
	return nil
}
