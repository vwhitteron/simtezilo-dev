package synthesizer

import (
	"math"
)

// EffectsSampleBank holds pre-generated audio samples for various sound effects.
type EffectsSampleBank struct {
	sample map[string][]float64
}

// NewEffectsSampleBank initializes and returns a new EffectsSampleBank with pre-generated samples.
func NewEffectsSampleBank(sampleRateHz int) *EffectsSampleBank {
	return &EffectsSampleBank{
		sample: map[string][]float64{
			"transmission": generateGearShiftSample(sampleRateHz),
		},
	}
}

// GetSample retrieves a pre-generated sample by name. If the sample does not exist, it returns an empty slice.
func (s *EffectsSampleBank) GetSample(name string) []float64 {
	if _, ok := s.sample[name]; !ok {
		return []float64{}
	}

	return s.sample[name]
}

// generateGearShiftSample creates a sample representing a gear shift sound effect.
func generateGearShiftSample(sampleRateHz int) []float64 {
	sampleLengthSeconds := 0.1
	pulseAmplitude := 2.0
	pulseHz := 30
	decayRate := 0.005

	sampleCount := int(sampleLengthSeconds * float64(sampleRateHz))

	pulseWidth := sampleRateHz / (2 * pulseHz)
	waveSamplePeriod := math.Pi / float64(pulseWidth)
	waveOffset := float64(pulseWidth)

	audioSample := make([]float64, sampleCount)

	for i := range audioSample {
		angle := waveSamplePeriod * (float64(i) - waveOffset)
		audioSample[i] = pulseAmplitude * math.Sin(angle)

		pulseAmplitude *= (1 - decayRate)
	}

	return audioSample
}
