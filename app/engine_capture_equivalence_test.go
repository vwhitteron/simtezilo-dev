//nolint:testpackage // compares the app-side harness against the CGO-free one
package app

// The engine layer has two renderers: this package's discrete-event harness, and the
// CGO-free packet-driven capture in app/haptics that the tuning assistant calls. They
// drive the same generator but step time differently, so they split the generated audio
// into blocks at different points and are NOT sample-identical.
//
// This test pins how far apart they are allowed to drift. It is what would catch one
// renderer changing without the other.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// engineEquivalenceTolerance is the fraction by which the two renders' peak and RMS may
// differ.
//
// The headroom is deliberately generous, for two reasons: the block boundaries differ,
// so the amplitude ramp lands on slightly different samples; and the two captures do
// not cover the same span (the app-side harness windows by seconds, the CGO-free one by
// lap frames), so RMS is compared as overall character rather than section for section.
//
// In practice the two agree to four decimal places. The tolerance is here to catch a
// real divergence — a wrong firing frequency, a missing profile, or an engine tick
// landing on the wrong beat — each of which moves these figures by far more than this.
const engineEquivalenceTolerance = 0.15

func TestEngineRenderersAgree(t *testing.T) {
	t.Parallel()

	root, _ := filepath.Abs("..")
	replay := filepath.Join(root, "data", "replays",
		"20260801.111955-circuit-de-spa-francorchamps-toyota-supra-rz-97.gtz")

	_, statErr := os.Stat(replay)
	if statErr != nil {
		t.Skipf("replay not present: %v", statErr)
	}

	source := "file://" + replay

	appSide, err := CaptureHaptics(HapticCaptureOptions{
		Source: source, Engine: true, SeekSeconds: 20, DurSeconds: 30,
	})
	if err != nil {
		t.Fatalf("app-side capture: %v", err)
	}

	var free []float64

	_, err = haptics.CaptureChassis(context.Background(), haptics.CaptureOptions{
		Source: source,
		Layers: haptics.CaptureLayers{NoChassis: true, Engine: true},
		Sink:   func(samples []float64) { free = append(free, samples...) },
	})
	if err != nil {
		t.Fatalf("cgo-free capture: %v", err)
	}

	appPeak, appRMS := peakRMS(appSide.Samples)
	freePeak, freeRMS := peakRMS(free)

	t.Logf("app-side  peak=%.6f rms=%.6f samples=%d", appPeak, appRMS, len(appSide.Samples))
	t.Logf("cgo-free  peak=%.6f rms=%.6f samples=%d", freePeak, freeRMS, len(free))

	if appPeak <= 0 || freePeak <= 0 {
		t.Fatal("one renderer produced silence")
	}

	assertWithin(t, "peak", appPeak, freePeak)
	assertWithin(t, "rms", appRMS, freeRMS)
}

// assertWithin fails when two figures differ by more than the tolerance, relative to
// the larger of the two.
func assertWithin(t *testing.T, name string, appSide, cgoFree float64) {
	t.Helper()

	drift := math.Abs(appSide-cgoFree) / math.Max(appSide, cgoFree)
	if drift > engineEquivalenceTolerance {
		t.Errorf("%s drifted %.1f%% (app-side %.6f, cgo-free %.6f), tolerance %.0f%%",
			name, drift*100, appSide, cgoFree, engineEquivalenceTolerance*100)
	}

	t.Logf("%s drift %.1f%%", name, drift*100)
}

func peakRMS(samples []float64) (peak, rms float64) {
	if len(samples) == 0 {
		return 0, 0
	}

	var sumSquares float64

	for _, sample := range samples {
		magnitude := math.Abs(sample)
		if magnitude > peak {
			peak = magnitude
		}

		sumSquares += sample * sample
	}

	return peak, math.Sqrt(sumSquares / float64(len(samples)))
}
