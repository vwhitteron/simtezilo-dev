package audioqa_test

import (
	"math"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/audio/audioqa"
)

const (
	testRate = 8000
	testFreq = 220.0
	testAmp  = 0.5
)

// cleanSine returns one second of a pure sine — the baseline a healthy pipeline
// should reproduce with no glitches or dropouts.
func cleanSine() []float64 {
	samples := make([]float64, testRate)
	inc := 2 * math.Pi * testFreq / testRate

	for i := range samples {
		samples[i] = testAmp * math.Sin(inc*float64(i))
	}

	return samples
}

// stepBound is the largest sample-to-sample step a clean test sine produces.
func stepBound() float64 { return testAmp * 2 * math.Pi * testFreq / testRate }

func TestAnalyseCleanSineHasNoArtefacts(t *testing.T) {
	t.Parallel()

	metrics := audioqa.Analyse(cleanSine(), testRate, testAmp, stepBound())

	if metrics.Empty {
		t.Fatal("clean sine analysed as empty")
	}

	if metrics.NonFinite != 0 || metrics.Clipped != 0 || metrics.Dropouts != 0 || metrics.Glitches != 0 {
		t.Errorf("clean sine flagged: nonFinite=%d clipped=%d dropouts=%d glitches=%d",
			metrics.NonFinite, metrics.Clipped, metrics.Dropouts, metrics.Glitches)
	}

	if metrics.MaxStep > stepBound()*1.01 {
		t.Errorf("clean sine max step %.6f exceeds bound %.6f", metrics.MaxStep, stepBound())
	}
}

// TestAnalyseFlagsDiscontinuity is the detector's core guarantee: a spliced-in
// chunk of waveform from a non-adjacent phase — the signature of the engine
// channel's stale-gap bug — produces a step the analyser must flag as a glitch.
func TestAnalyseFlagsDiscontinuity(t *testing.T) {
	t.Parallel()

	samples := cleanSine()

	// Splice a block sampled at the sine's negative peak into a region where the
	// signal is near its positive peak: a sharp discontinuity at both seams.
	at := len(samples) / 2
	for i := range 40 {
		samples[at+i] = -testAmp
	}

	metrics := audioqa.Analyse(samples, testRate, testAmp, stepBound())

	if metrics.Glitches == 0 {
		t.Fatalf("analyser missed a spliced discontinuity (maxStep %.6f, bound %.6f)", metrics.MaxStep, stepBound())
	}
}

func TestZeroRunsDetectsDropout(t *testing.T) {
	t.Parallel()

	samples := cleanSine()

	// A run of exact zeros longer than two samples is the underrun signature.
	at := len(samples) / 3
	for i := range 16 {
		samples[at+i] = 0
	}

	count, longest := audioqa.ZeroRuns(samples)
	if count == 0 || longest < 16 {
		t.Errorf("ZeroRuns missed the dropout: count=%d longest=%d", count, longest)
	}
}

func TestAnalyseToneRecoversFundamental(t *testing.T) {
	t.Parallel()

	metrics, tone := audioqa.AnalyseTone(cleanSine(), testRate, testFreq, testAmp)

	if metrics.Empty {
		t.Fatal("clean sine analysed as empty")
	}

	if math.Abs(tone.Gain-1) > 0.01 {
		t.Errorf("recovered gain %.4f, want ~1.0", tone.Gain)
	}

	if tone.SNR < 60 {
		t.Errorf("clean sine SNR %.1f dB unexpectedly low", tone.SNR)
	}
}
