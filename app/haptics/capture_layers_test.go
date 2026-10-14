package haptics_test

// Guards for the layer selector on the CGO-free capture: a zero CaptureLayers must
// keep the chassis-only behaviour the jerk/snap tuning workflow depends on, and each
// additional layer must actually contribute signal.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// captureLayerReplay resolves the replay used by these tests, skipping when it is
// absent so a checkout without the replay set still passes.
func captureLayerReplay(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "data", "replays",
		"20260801.111955-circuit-de-spa-francorchamps-toyota-supra-rz-97.gtz"))
	if err != nil {
		t.Fatalf("resolving replay path: %v", err)
	}

	_, statErr := os.Stat(path)
	if statErr != nil {
		t.Skipf("replay not present: %v", statErr)
	}

	return "file://" + path
}

// renderLayers captures one lap section and returns the peak sample magnitude.
func renderLayers(t *testing.T, layers haptics.CaptureLayers) float64 {
	t.Helper()

	var peak float64

	_, err := haptics.CaptureChassis(context.Background(), haptics.CaptureOptions{
		Source: captureLayerReplay(t),
		Layers: layers,
		Window: &haptics.CaptureWindow{Lap: 2, FromFrame: 0, ToFrame: 1800},
		Sink: func(samples []float64) {
			for _, sample := range samples {
				if magnitude := math.Abs(sample); magnitude > peak {
					peak = magnitude
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	return peak
}

// The zero value must still render the chassis pulse, because every existing caller
// (the tuning assistant included) passes no Layers at all.
func TestZeroLayersRendersChassisOnly(t *testing.T) {
	t.Parallel()

	if peak := renderLayers(t, haptics.CaptureLayers{}); peak <= 0 {
		t.Fatalf("zero CaptureLayers rendered silence, peak=%v", peak)
	}
}

// Each layer must be selectable on its own, so the tuning assistant can show one
// waveform lane per layer rather than a single mixed trace.
func TestEachLayerRendersOnItsOwn(t *testing.T) {
	t.Parallel()

	cases := map[string]haptics.CaptureLayers{
		"texture":      {NoChassis: true, Texture: true},
		"transmission": {NoChassis: true, Transmission: true},
		"engine":       {NoChassis: true, Engine: true},
	}

	for name, layers := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if peak := renderLayers(t, layers); peak <= 0 {
				t.Fatalf("%s layer rendered silence, peak=%v", name, peak)
			}
		})
	}
}

// Suppressing every layer must produce silence. This is the control for the tests
// above: without it, a layer test could pass on another layer's signal.
func TestAllLayersOffRendersSilence(t *testing.T) {
	t.Parallel()

	if peak := renderLayers(t, haptics.CaptureLayers{NoChassis: true}); peak != 0 {
		t.Fatalf("all layers off should render silence, peak=%v", peak)
	}
}
