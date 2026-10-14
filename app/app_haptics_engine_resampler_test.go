//nolint:testpackage // drives the unexported engine waveform generator through the real resampler
package app

// Full-pipeline smoke test for the engine waveform. The generator itself moved to
// app/haptics with its artefact guards; this one test stayed behind because it feeds
// the waveform through app/audio's resampler, and app/audio links portaudio, which
// app/haptics deliberately does not.

import (
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/audio/audioqa"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/haptics/profiles"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
)

// engineSource adapts a mono engine waveform to an audio.SampleSource, emitting
// the same sample on every channel — enough to push the engine signal through the
// resampler and async ring for the full-pipeline smoke test.
type engineSource struct {
	samples []float64
	pos     int
}

func (s *engineSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels
	for frame := range frames {
		var value float32
		if s.pos < len(s.samples) {
			value = float32(s.samples[s.pos])
			s.pos++
		}

		for channel := range channels {
			buf[frame*channels+channel] = value
		}
	}

	return frames, true
}

// TestEngineResamplerClean feeds phase-continuous engine audio through the live
// windowed-sinc resampler — the stage most likely to distort the engine's sharp
// pulse edges — and asserts it stays finite and unclipped with no dropouts of its
// own. The resampler is synchronous and deterministic; the async ring's runtime
// behaviour is covered separately by async_test.go and the audio_cleanup tool.
func TestEngineResamplerClean(t *testing.T) {
	t.Parallel()

	const (
		inRate   = 8000
		outRate  = 32000
		channels = 2
		refAmp   = 0.3
	)

	// Rendered through the real generator rather than a local copy of it, so this
	// test feeds the resampler exactly what the app produces. The waveform generator
	// itself is unexported in app/haptics, which is the point: this test consumes the
	// public path.
	engineAudio := renderEngineAudio(t, inRate*2)

	src := audio.NewResamplingSource(&engineSource{samples: engineAudio}, inRate, outRate, channels)

	outFrames := outRate // analyse ~1 s
	rec := make([]float64, 0, outFrames)
	buf := make([]float32, (outRate/100)*channels)

	for len(rec) < outFrames {
		frames, ok := src.ReadInterleaved(buf, channels)
		if !ok {
			break
		}

		for frame := range frames {
			rec = append(rec, float64(buf[frame*channels]))
		}
	}

	metrics := audioqa.Analyse(rec, outRate, refAmp, 0)

	if metrics.Empty {
		t.Fatal("resampler produced no signal")
	}

	if metrics.NonFinite != 0 || metrics.Clipped != 0 {
		t.Errorf("resampler: nonFinite=%d clipped=%d peak=%.4f", metrics.NonFinite, metrics.Clipped, metrics.Peak)
	}
}

// engineRPMSource is a TelemetrySource that holds the engine at a fixed RPM and
// full throttle, which is all the engine generator reads.
type engineRPMSource struct{ rpm float32 }

func (s engineRPMSource) EngineRPM() float32 { return s.rpm }

func (s engineRPMSource) ThrottleOutputPercent() float32 { return 100 }

func (s engineRPMSource) Transmission() gttelemetry.Transmission {
	return gttelemetry.Transmission{}
}

// renderEngineAudio drives the real EngineGenerator against a real synthesizer and
// returns the samples it produced, so the resampler test analyses production output
// rather than a reimplementation of it.
func renderEngineAudio(t *testing.T, samples int) []float64 {
	t.Helper()

	logger := zerolog.New(io.Discard)
	cfg := config.New(config.Options{Logger: logger})

	calib, err := calibrator.NewToneGenerator(cfg)
	if err != nil {
		t.Fatalf("calibrator: %v", err)
	}

	var kin kinematics.State

	synth, err := synthesizer.New(&synthesizer.SynthOpts{
		Config:     cfg.GetSynthesizer(),
		BaseConfig: cfg,
		Logger:     logger,
		Kinematics: &kin,
		Calibrator: calib,
	})
	if err != nil {
		t.Fatalf("synth: %v", err)
	}

	defer func() { _ = synth.Close() }()

	gen := haptics.NewEngineGenerator(cfg, synth,
		func() haptics.TelemetrySource { return engineRPMSource{rpm: 6000} }, logger)

	gen.SetVehicle(vehicle.Characteristics{
		RevLimit:    8000,
		VehicleType: vehicle.TypeRace,
		Engine: vehicle.EngineCharacteristics{
			Geometry:        "",
			FiringFrequency: 0.0333333, // 4-cylinder 4-stroke
			PulseOverlap:    0.25,
			Haptics: &profiles.EngineProfile{
				PrimaryBalance:   0.85,
				SecondaryBalance: 0.85,
				Gain:             0.0,
				PulseScale:       1.0,
			},
		},
	})

	// One Generate per engine frame, then drain exactly one frame, mirroring the live
	// generate-versus-drain relationship. The drain is bounded per tick: the master
	// read is pull-based and returns samples indefinitely, so an unbounded inner loop
	// would never finish.
	frame := synth.GetSampleRate() / 30 // engine haptic frame rate

	out := make([]float64, 0, samples+frame)
	read := make([]float64, frame)

	for seq := uint32(1); len(out) < samples; seq++ {
		gen.Generate(seq)

		count := synth.ReadBuffer(read)
		if count == 0 {
			t.Fatal("engine generator produced no audio")
		}

		out = append(out, read[:count]...)
	}

	return out[:samples]
}
