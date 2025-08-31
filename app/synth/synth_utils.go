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

// findZeroCrossing searches for the first zero point or crossing in a given array of samples
// Returns the position of the zero crossing and the polarity just before the crossing (-1 or 1)
// If no zero point or crossing is found then it returns the first index and polarity of first sample
func FindSampleZeroCrossing(samples []float64) (offset int, polarity int) {
	// Find zero crossing or polarity change within the current buffer content
	offset = 0
	polarity = 1

	searchRange := len(samples)
	if searchRange <= 1 {
		if searchRange == 1 {
			if samples[0] < 0 {
				polarity = -1
			}
		}
		return offset, polarity
	}

	// Determine initial polarity from first sample
	if samples[0] < 0 {
		polarity = -1
	}

	// Look for zero crossings in the current buffer content
	for i := range searchRange - 1 {
		currentSample := (samples)[i]
		nextSample := (samples)[i+1]

		// Check for exact zero
		if currentSample == 0.0 {
			offset = i
			// For exact zero, use polarity from previous sample if available
			if i > 0 {
				if samples[i-1] < 0 {
					polarity = -1
				} else {
					polarity = 1
				}
			}
			break
		}

		// Check for polarity change (zero crossing)
		if (currentSample > 0 && nextSample < 0) || (currentSample < 0 && nextSample > 0) {
			offset = i + 1
			// Polarity before crossing is the polarity of current sample
			if currentSample < 0 {
				polarity = -1
			} else {
				polarity = 1
			}
			break
		}
	}

	return offset, polarity
}

// SamplePolarity returns the polarity of the given samples (-1 negative, 1 positive).
// The samples are checked from the last to the first and the polority is returned
// as soon as a non-zero sample is encountered.
// If all samples are zero, it returns positive polarity.
func SamplePolarity(samples []float64) float64 {
	for i := len(samples) - 1; i >= 0; i-- {
		if samples[i] < 0 {
			return -1
		}
	}

	return 1
}

// InvertSamplePolarity inverts the polarity of the given samples.
func InvertSamplePolarity(samples *[]float64) {
	for i := range *samples {
		(*samples)[i] = -(*samples)[i]
	}
}
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

	scaleSamples(samples, scale)
}

// Adjusts the scale of the samples by the given magnitude
func scaleSamples(samples *[]float64, magnitude float64) {
	for i := range *samples {
		(*samples)[i] = (*samples)[i] * magnitude
	}
}
