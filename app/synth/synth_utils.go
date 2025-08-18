package synth

import (
	"math"
)

// GainToPowerRatio converts a gain value in dB to a power ratio magnitude.
func GainToPowerRatio(gain float64) float64 {
	if gain == 0 {
		return 1.0
	}

	powerRatio := math.Pow(10, gain/10)

	return powerRatio
}

// GainToAmplitudeRatio converts a gain value in dB to an amplitude magnitude.
func GainToAmplitudeRatio(gain float64) float64 {
	if gain == 0 {
		return 1.0
	}

	amplitudeRatio := math.Pow(10, gain/20)

	return amplitudeRatio
}

// Mixes two samples using a simple sum algorithm.
// Returns the mixed sample and the peak value which is later used to scale a slice of samples.
func mixSampleSum(sample1 float64, sample2 float64, peak *float64) float64 {
	sum := sample1 + sample2

	sumAbs := math.Abs(sum)

	if sumAbs > *peak {
		*peak = sumAbs
	}

	return sum
}

// TODO: probably should remove this as it's not a very good algorithm
// Mixes two samples using a Root Square Sum algorithm.
// If peak is less than 0, it applies a limiter to the output sample to prevent clipping.
// Otherwise, the RSS mixed sample and the peak value are returned.
// func mixSamplesRSS(sample1 float64, sample2 float64, peak *float64) float64 {
// 	sampleOut := 0.0

// 	squareSample1 := signal.Polarity(sample1) * sample1 * sample1
// 	squareSample2 := signal.Polarity(sample2) * sample2 * sample2
// 	sum := math.Abs(squareSample1 + squareSample2)
// 	sampleOut = math.Sqrt(sum)

// 	if sampleOut > *peak {
// 		*peak = sampleOut
// 	}

// 	// Restore the signal to its original polarity since RSS always results in a
// 	// positive value
// 	sampleOut = sampleOut * signal.Polarity(sample1+sample2)

// 	return sampleOut
// }

// Adjusts the gain on a slice of samples using the peak value.
func scaleSamplesPeak(samples *[]float64, peak float64) {
	if peak < 1.0 {
		return
	}

	scale := 1.0 / peak

	for i := range *samples {
		(*samples)[i] = (*samples)[i] * scale
	}
}
