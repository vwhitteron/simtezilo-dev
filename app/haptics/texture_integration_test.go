package haptics

import (
	"io"
	"math"
	"testing"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// textureTestRig is a minimal driver for the texture generator: a real synthesizer,
// a shared kinematics state the generator reads, and the Generator under test. It
// mirrors newCaptureApp's synth wiring but needs no telemetry client, since the
// texture and pulse generators read only config, synth and kinematics.
type textureTestRig struct {
	gen   *Generator
	synth *synthesizer.Synthesizer
	kin   *kinematics.State
}

// buildTextureTestRig constructs the rig, driven by a config built from JSON so the
// texture layer can be toggled via its synth mute.
func buildTextureTestRig(t *testing.T, textureEnabled bool) *textureTestRig {
	t.Helper()

	logger := zerolog.New(io.Discard)

	// The texture source is on when its synth channel is unmuted.
	muted := "true"
	if textureEnabled {
		muted = "false"
	}

	cfgJSON := `{
		"schemaVersion": "1.0.0",
		"synthesizer": {
			"textureMute": ` + muted + `
		},
		"haptics": {
			"textureMinFrequencyHz": 25,
			"textureMaxFrequencyHz": 45
		}
	}`

	cfg := config.NewFromJSON([]byte(cfgJSON), logger)

	calib, err := calibrator.NewToneGenerator(cfg)
	if err != nil {
		t.Fatalf("calibrator: %v", err)
	}

	kin := &kinematics.State{}

	synth, err := synthesizer.New(&synthesizer.SynthOpts{
		Config:     cfg.GetSynthesizer(),
		BaseConfig: cfg,
		Logger:     logger,
		Kinematics: kin,
		Calibrator: calib,
	})
	if err != nil {
		t.Fatalf("synth: %v", err)
	}

	return &textureTestRig{
		gen:   NewGenerator(cfg, synth, kin, logger),
		synth: synth,
		kin:   kin,
	}
}

// drainRMS pulls samples from the synth master output and returns their RMS and peak.
func drainRMS(rig *textureTestRig, frames int) (rms, peak float64) {
	buf := make([]float64, 256)

	var sumSq float64

	var n int

	for range frames {
		got := rig.synth.ReadBuffer(buf)
		for _, s := range buf[:got] {
			sumSq += s * s
			n++

			if a := math.Abs(s); a > peak {
				peak = a
			}
		}
	}

	if n == 0 {
		return 0, 0
	}

	return math.Sqrt(sumSq / float64(n)), peak
}

// dirtCorners returns an all-dirt surface set (a loud, coarse surface) for driving the
// texture layer in tests.
func dirtCorners() models.CornerSetGeneric[models.SurfaceType] {
	return models.CornerSetGeneric[models.SurfaceType]{
		FrontLeft: models.SurfaceTypeDirt, FrontRight: models.SurfaceTypeDirt,
		RearLeft: models.SurfaceTypeDirt, RearRight: models.SurfaceTypeDirt,
	}
}

// TestChassisTextureProducesBoundedOutput drives the texture layer on a loud surface at
// speed and confirms it produces a non-silent, bounded, finite signal on the chassis
// output.
func TestChassisTextureProducesBoundedOutput(t *testing.T) {
	rig := buildTextureTestRig(t, true)

	rig.kin.Current.SurfaceType = dirtCorners()
	rig.kin.Current.GroundSpeed = 40 // above the speed gate, mid frequency band

	// Prime and run several frames so the carrier fills the channel and drains.
	for range 30 {
		rig.gen.Texture()
		_, _ = drainRMS(rig, 1)
	}

	rig.kin.Current.SurfaceType = dirtCorners()
	rig.kin.Current.GroundSpeed = 40

	for range 20 {
		rig.gen.Texture()
	}

	rms, peak := drainRMS(rig, 40)

	if math.IsNaN(rms) || math.IsInf(rms, 0) || math.IsNaN(peak) || math.IsInf(peak, 0) {
		t.Fatalf("non-finite output: rms=%v peak=%v", rms, peak)
	}

	if rms == 0 {
		t.Fatal("texture enabled at speed produced silence")
	}

	// Texture must stay below the impact-pulse ceiling (pulses approach unity). The
	// modulation RMS is capped at textureMaxAmplitude and band-limited noise runs a
	// crest factor above that, so allow headroom for the crest but require it stays
	// clear of unity.
	if peak > textureMaxAmplitude*2 {
		t.Fatalf("texture peak %.3f is too loud; should sit below the pulse ceiling", peak)
	}
}

// TestChassisTextureDisabledIsSilent confirms the layer emits nothing when disabled.
func TestChassisTextureDisabledIsSilent(t *testing.T) {
	rig := buildTextureTestRig(t, false)

	rig.kin.Current.SurfaceType = dirtCorners()
	rig.kin.Current.GroundSpeed = 50

	for range 30 {
		rig.gen.Texture()
	}

	_, peak := drainRMS(rig, 40)
	if peak != 0 {
		t.Fatalf("disabled texture should be silent, got peak %.4f", peak)
	}
}

// TestChassisTextureMutedBelowSpeed confirms the speed gate silences the layer at a
// standstill even on a loud surface.
func TestChassisTextureMutedBelowSpeed(t *testing.T) {
	rig := buildTextureTestRig(t, true)

	rig.kin.Current.SurfaceType = dirtCorners()
	rig.kin.Current.GroundSpeed = 0 // stationary

	for range 30 {
		rig.gen.Texture()
	}

	_, peak := drainRMS(rig, 40)
	if peak != 0 {
		t.Fatalf("stationary texture should be silent, got peak %.4f", peak)
	}
}
