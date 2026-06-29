package synthesizer

import (
	"math"
	"testing"
	"time"
)

// halfSinePulse returns a self-terminating raised half-sine hump of the given
// length and peak amplitude. It starts and ends at zero, matching the chassis
// pulse shape in app_haptics_chassis.go.
func halfSinePulse(length int, amplitude float64) []float64 {
	pulse := make([]float64, length)
	for i := range pulse {
		// 0..pi over the pulse → sin goes 0→1→0.
		phase := math.Pi * float64(i) / float64(length-1)
		pulse[i] = amplitude * math.Sin(phase)
	}

	return pulse
}

func TestMixSamplePriority(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		a, b float64
		want float64
	}{
		{"zero subordinate returns dominant unchanged", 0.4, 0.0, 0.4},
		{"zero dominant returns subordinate unchanged", 0.0, -0.7, -0.7},
		{"both fit under unity sum normally", 0.3, 0.2, 0.5},
		{"louder wins, weaker ducked into headroom", 0.9, 0.5, 1.0},
		{"negative louder wins, weaker ducked", -0.9, -0.5, -1.0},
		{"dominant at unity drops subordinate", 1.0, 0.5, 1.0},
		{"dominant over unity is clamped", 1.4, 0.5, 1.0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := mixSamplePriority(testCase.a, testCase.b)
			if math.Abs(got-testCase.want) > 1e-9 {
				t.Errorf("mixSamplePriority(%v, %v) = %v, want %v", testCase.a, testCase.b, got, testCase.want)
			}

			if math.Abs(got) > 1.0+1e-9 {
				t.Errorf("mixSamplePriority(%v, %v) = %v exceeds unity", testCase.a, testCase.b, got)
			}
		})
	}
}

// TestMixModeNoBoundaryStep verifies that mixing a short, high-amplitude pulse on
// top of a longer, lower-amplitude pulse already in the buffer introduces no
// amplitude step at the short pulse's boundaries — the failure mode that the old
// retroactive peak limiter produced (scaling only the written window of a shared
// buffer), which was audible as a click.
func TestMixModeNoBoundaryStep(t *testing.T) {
	t.Parallel()

	const rate = 1000

	buffer := NewAdaptiveBuffer(time.Second, rate)
	buffer.Clear()

	// A long, low-amplitude wave already playing in the buffer.
	const longLen = 200

	long := halfSinePulse(longLen, 0.4)
	buffer.Write(long, 0, false)

	// Consume the first part so the short pulse lands partway into the long wave.
	const consumed = 50

	buf := make([]float64, consumed)
	buffer.Read(buf)

	// A short, high-amplitude pulse whose overlap with the long wave would clip.
	const shortLen = 40

	short := halfSinePulse(shortLen, 0.9)
	buffer.Write(short, 0, false)

	// Read back the remaining contiguous region: long-wave tail with the short
	// pulse mixed into indices [0, shortLen).
	buf = make([]float64, longLen-consumed)
	length := buffer.Read(buf)
	out := buf[:length]

	// Nothing may exceed unity.
	for i, s := range out {
		if math.Abs(s) > 1.0+1e-9 {
			t.Fatalf("sample %d = %v exceeds unity", i, s)
		}
	}

	// The dominant short pulse must retain its peak (it is the loudest component,
	// so it should not be attenuated).
	peak := 0.0
	for i := range shortLen {
		peak = math.Max(peak, out[i])
	}

	if peak < 0.9-1e-6 {
		t.Errorf("dominant short pulse peak %v was attenuated below 0.9", peak)
	}

	// There must be no step at the short pulse's trailing boundary. The
	// underlying long wave is smooth there, so the delta across the boundary must
	// stay within the long wave's own per-sample slope (with a small margin).
	longSlope := 0.0
	for i := 1; i < len(long); i++ {
		longSlope = math.Max(longSlope, math.Abs(long[i]-long[i-1]))
	}

	maxStep := longSlope * 3 // generous margin over the natural slope

	delta := math.Abs(out[shortLen] - out[shortLen-1])
	if delta > maxStep {
		t.Errorf("amplitude step %v at short-pulse trailing boundary exceeds %v (long-wave slope %v)",
			delta, maxStep, longSlope)
	}
}

// TestMixModeNoDiscontinuities is a regression guard for the audible pop/click
// that occurred before the priority-mix fix. It reproduces the real pipeline
// cadence — a new self-terminating pulse mixed in every frame while earlier,
// longer pulses are still playing, with samples drained between frames — then
// scans the entire consumed output stream for any amplitude step that exceeds
// what the constituent waveforms can legitimately produce.
//
// The old retroactive peak limiter scaled only the just-written window of the
// shared buffer, injecting large steps into the longer waves still in flight;
// this test fails loudly if that class of bug is reintroduced.
func TestMixModeNoDiscontinuities(t *testing.T) {
	t.Parallel()

	const (
		rate       = 8000 // internal synth rate
		frames     = 120  // ~2 s of 60 Hz frames
		frameAdv   = rate / 60
		maxPulseHz = 60.0
	)

	buffer := NewAdaptiveBuffer(2*time.Second, rate)
	buffer.Clear()

	// Highest legitimate per-sample slope: peak amplitude pulse at the highest
	// frequency. Two pulses can overlap, so allow for the sum of two such slopes
	// plus a generous margin. Any step beyond this is a discontinuity artifact.
	maxSinglePulseSlope := 1.0 * math.Pi / (float64(rate) / maxPulseHz)
	maxLegitStep := maxSinglePulseSlope * 2 * 4

	stream := make([]float64, 0, frames*frameAdv)

	for frame := range frames {
		// Alternate long low-frequency and short high-frequency pulses at full
		// amplitude so their overlaps would clip and exercise the limiter.
		freqHz := 16.0
		if frame%2 == 1 {
			freqHz = maxPulseHz
		}

		pulseLen := int(float64(rate) / freqHz)
		buffer.Write(halfSinePulse(pulseLen, 1.0), 0, false)

		block := make([]float64, frameAdv)
		length := buffer.Read(block)
		stream = append(stream, block[:length]...)
	}

	for i, s := range stream {
		if math.Abs(s) > 1.0+1e-9 {
			t.Fatalf("sample %d = %v exceeds unity", i, s)
		}
	}

	for i := 1; i < len(stream); i++ {
		delta := math.Abs(stream[i] - stream[i-1])
		if delta > maxLegitStep {
			t.Fatalf("discontinuity at sample %d: step %v exceeds max legitimate step %v",
				i, delta, maxLegitStep)
		}
	}
}
