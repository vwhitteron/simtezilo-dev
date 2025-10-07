package synthesizer

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
// If no zero point or crossing is found then it returns the first index and polarity of first sample.
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
	for index := range searchRange - 1 {
		currentSample := (samples)[index]
		nextSample := (samples)[index+1]

		// Check for exact zero
		if currentSample == 0.0 {
			offset = index
			// For exact zero, use polarity from previous sample if available
			if index > 0 {
				if samples[index-1] < 0 {
					polarity = -1
				} else {
					polarity = 1
				}
			}

			break
		}

		// Check for polarity change (zero crossing)
		if (currentSample > 0 && nextSample < 0) || (currentSample < 0 && nextSample > 0) {
			offset = index + 1
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

// Adjusts the scale of the samples by the given magnitude.
func ScaleSamples(samples *[]float64, magnitude float64) {
	for i := range *samples {
		(*samples)[i] *= magnitude
	}
}

// Adjusts the gain on a slice of samples using the peak value.
func scaleSamplesPeak(samples *[]float64, peak float64) {
	if peak < 1.0 {
		return
	}

	scale := 1.0 / peak

	ScaleSamples(samples, scale)
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
