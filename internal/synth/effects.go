package synth

import (
	"math"
)

type EffectsSampleBank struct {
	sample map[string][]float64
}

func NewEffectsSampleBank(sampleRateHz int) *EffectsSampleBank {
	return &EffectsSampleBank{
		sample: map[string][]float64{
			"gearchange": generateGearChangeSample(sampleRateHz),
		},
	}
}

func (s *EffectsSampleBank) GetSample(name string) []float64 {
	if _, ok := s.sample[name]; !ok {
		return []float64{}
	}

	return s.sample[name]
}

func generateGearChangeSample(sampleRateHz int) []float64 {
	sampleLengthSeconds := 0.1
	pulseAmplitude := 1.6
	pulseHz := 30
	decayRate := 0.005

	sampleCount := int(sampleLengthSeconds * float64(sampleRateHz))

	pulseWidth := sampleRateHz / (2 * pulseHz)
	waveSamplePeriod := math.Pi / float64(pulseWidth)
	waveOffset := float64(pulseWidth)

	audioSample := make([]float64, sampleCount)

	for i := range audioSample {
		angle := waveSamplePeriod * (float64(i) - waveOffset)
		audioSample[i] = (pulseAmplitude * math.Sin(angle)) / 2

		pulseAmplitude = pulseAmplitude * (1 - decayRate)
	}

	return audioSample
}
