package synthesizer

import (
	"math"
	"testing"
)

// TestCalibrationEQAmplitudeCalculation tests the EQ amplitude calculation logic
// used during calibration mode.
func TestCalibrationEQAmplitudeCalculation(t *testing.T) {
	t.Parallel()

	// Create a test EQ curve: notch filter at 20Hz (-12dB)
	// Curve spans 10Hz to 70Hz with 0.5Hz resolution = 121 buckets
	const (
		minFreq    = 10.0
		maxFreq    = 70.0
		resolution = 0.5
		numBuckets = int((maxFreq-minFreq)/resolution) + 1
	)

	// Create a flat curve (all 1.0 = 0dB)
	flatCurve := make([]float64, numBuckets)
	for i := range flatCurve {
		flatCurve[i] = 1.0
	}

	// Create a curve with -12dB notch at 20Hz
	// -12dB = 10^(-12/20) ≈ 0.251
	notchAt20Hz := make([]float64, numBuckets)
	for i := range notchAt20Hz {
		notchAt20Hz[i] = 1.0
	}

	notchIndex := int((20.0 - minFreq) / resolution)
	notchAt20Hz[notchIndex] = math.Pow(10, -12.0/20.0) // -12dB

	// Create a curve with -12dB notch at 30Hz
	notchAt30Hz := make([]float64, numBuckets)
	for i := range notchAt30Hz {
		notchAt30Hz[i] = 1.0
	}

	notchIndex30 := int((30.0 - minFreq) / resolution)
	notchAt30Hz[notchIndex30] = math.Pow(10, -12.0/20.0)

	tests := []struct {
		name         string
		frequency    float64
		curve        []float64
		minFreq      float64
		resolution   float64
		expectedAmp  float64
		tolerance    float64
		curveEnabled bool
	}{
		{
			name:         "flat curve at 20Hz returns unity",
			frequency:    20.0,
			curve:        flatCurve,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  1.0,
			tolerance:    0.001,
			curveEnabled: true,
		},
		{
			name:         "notch at 20Hz returns -12dB amplitude",
			frequency:    20.0,
			curve:        notchAt20Hz,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  math.Pow(10, -12.0/20.0),
			tolerance:    0.001,
			curveEnabled: true,
		},
		{
			name:         "notch at 20Hz but querying 30Hz returns unity",
			frequency:    30.0,
			curve:        notchAt20Hz,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  1.0,
			tolerance:    0.001,
			curveEnabled: true,
		},
		{
			name:         "notch at 30Hz returns -12dB at 30Hz",
			frequency:    30.0,
			curve:        notchAt30Hz,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  math.Pow(10, -12.0/20.0),
			tolerance:    0.001,
			curveEnabled: true,
		},
		{
			name:         "frequency below range returns unity",
			frequency:    5.0,
			curve:        notchAt20Hz,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  1.0,
			tolerance:    0.001,
			curveEnabled: true,
		},
		{
			name:         "frequency above range returns unity",
			frequency:    100.0,
			curve:        notchAt20Hz,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  1.0,
			tolerance:    0.001,
			curveEnabled: true,
		},
		{
			name:         "empty curve returns unity",
			frequency:    20.0,
			curve:        nil,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  1.0,
			tolerance:    0.001,
			curveEnabled: true,
		},
		{
			name:         "EQ disabled returns unity",
			frequency:    20.0,
			curve:        notchAt20Hz,
			minFreq:      minFreq,
			resolution:   resolution,
			expectedAmp:  1.0,
			tolerance:    0.001,
			curveEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			amp := calculateEQAmplitude(tt.frequency, tt.curve, tt.minFreq, tt.resolution, tt.curveEnabled)

			if math.Abs(amp-tt.expectedAmp) > tt.tolerance {
				t.Errorf("calculateEQAmplitude() = %v, want %v (±%v)", amp, tt.expectedAmp, tt.tolerance)
			}
		})
	}
}

// TestCalibrationPerChannelEQ verifies that different channels can have different EQ curves.
func TestCalibrationPerChannelEQ(t *testing.T) {
	t.Parallel()

	const (
		minFreq    = 10.0
		resolution = 0.5
		numBuckets = 121
	)

	// Channel 0: notch at 20Hz
	ch0Curve := make([]float64, numBuckets)
	for i := range ch0Curve {
		ch0Curve[i] = 1.0
	}

	ch0Curve[int((20.0-minFreq)/resolution)] = 0.25 // -12dB

	// Channel 1: notch at 30Hz
	ch1Curve := make([]float64, numBuckets)
	for i := range ch1Curve {
		ch1Curve[i] = 1.0
	}

	ch1Curve[int((30.0-minFreq)/resolution)] = 0.25 // -12dB

	curves := [][]float64{ch0Curve, ch1Curve}
	enabled := []bool{true, true}

	// Test at 20Hz - channel 0 should be attenuated, channel 1 should be full
	freq := 20.0
	ch0Amp := calculateEQAmplitude(freq, curves[0], minFreq, resolution, enabled[0])
	ch1Amp := calculateEQAmplitude(freq, curves[1], minFreq, resolution, enabled[1])

	if math.Abs(ch0Amp-0.25) > 0.01 {
		t.Errorf("Channel 0 at 20Hz: got %v, want ~0.25", ch0Amp)
	}

	if math.Abs(ch1Amp-1.0) > 0.01 {
		t.Errorf("Channel 1 at 20Hz: got %v, want 1.0", ch1Amp)
	}

	// Test at 30Hz - channel 0 should be full, channel 1 should be attenuated
	freq = 30.0
	ch0Amp = calculateEQAmplitude(freq, curves[0], minFreq, resolution, enabled[0])
	ch1Amp = calculateEQAmplitude(freq, curves[1], minFreq, resolution, enabled[1])

	if math.Abs(ch0Amp-1.0) > 0.01 {
		t.Errorf("Channel 0 at 30Hz: got %v, want 1.0", ch0Amp)
	}

	if math.Abs(ch1Amp-0.25) > 0.01 {
		t.Errorf("Channel 1 at 30Hz: got %v, want ~0.25", ch1Amp)
	}
}

// calculateEQAmplitude is a pure function that calculates the EQ amplitude multiplier
// for a given frequency. This mirrors the logic in MixToMaster's calibration path.
func calculateEQAmplitude(frequency float64, curve []float64, minFreq, resolution float64, enabled bool) float64 {
	if !enabled {
		return 1.0
	}

	if len(curve) == 0 {
		return 1.0
	}

	index := int((frequency - minFreq) / resolution)
	if index < 0 || index >= len(curve) {
		return 1.0
	}

	return curve[index]
}
