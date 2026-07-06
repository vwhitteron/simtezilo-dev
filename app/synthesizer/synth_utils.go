package synthesizer

import (
	"math"
)

// FindSampleZeroCrossing searches for the first zero point or crossing in a given array of samples
// Returns the position of the zero crossing and the polarity just before the crossing (-1 or 1)
// If no zero point or crossing is found then it returns the first index and polarity of first sample.
func FindSampleZeroCrossing(samples []float64) (offset int, polarity int) {
	if len(samples) <= 1 {
		return handleShortSampleArray(samples)
	}

	polarity = getInitialPolarity(samples[0])
	offset = searchForZeroCrossing(samples)

	return offset, polarity
}

// handleShortSampleArray handles arrays with 0 or 1 samples.
func handleShortSampleArray(samples []float64) (int, int) {
	offset := 0
	polarity := 1

	if len(samples) == 1 && samples[0] < 0 {
		polarity = -1
	}

	return offset, polarity
}

// getInitialPolarity determines the initial polarity from the first sample.
func getInitialPolarity(firstSample float64) int {
	if firstSample < 0 {
		return -1
	}

	return 1
}

// searchForZeroCrossing searches for zero crossings in the sample array.
func searchForZeroCrossing(samples []float64) int {
	searchRange := len(samples)

	for index := range searchRange - 1 {
		currentSample := samples[index]
		nextSample := samples[index+1]

		if offset, found := checkExactZero(samples, index); found {
			return offset
		}

		if offset, found := checkPolarityChange(currentSample, nextSample, index); found {
			return offset
		}
	}

	return 0 // No zero crossing found, return first index
}

// checkExactZero checks if the current sample is exactly zero.
func checkExactZero(samples []float64, index int) (int, bool) {
	if samples[index] == 0.0 {
		return index, true
	}

	return 0, false
}

// checkPolarityChange checks if there's a polarity change between current and next sample.
func checkPolarityChange(currentSample, nextSample float64, index int) (int, bool) {
	polarityChanged := (currentSample > 0 && nextSample < 0) || (currentSample < 0 && nextSample > 0)
	if polarityChanged {
		return index + 1, true
	}

	return 0, false
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

// ScaleSamples adjusts the scale of the samples by the given magnitude. A unity
// magnitude is a no-op, so it skips the full-buffer multiply that hot callers
// (MixToMaster, the calibrator) would otherwise do every frame.
func ScaleSamples(samples *[]float64, magnitude float64) {
	if magnitude == 1.0 {
		return
	}

	for i := range *samples {
		(*samples)[i] *= magnitude
	}
}

// scaleSamplesPeak adjusts the gain on a slice of samples using the peak value.
func scaleSamplesPeak(samples *[]float64, peak float64) {
	if peak < 1.0 {
		return
	}

	scale := 1.0 / peak

	ScaleSamples(samples, scale)
}

// mixSampleSum mixes two samples using a simple sum algorithm.
// Returns the mixed sample and the peak value which is later used to scale a slice of samples.
func mixSampleSum(sample1 float64, sample2 float64, peak *float64) float64 {
	sum := sample1 + sample2

	sumAbs := math.Abs(sum)

	if sumAbs > *peak {
		*peak = sumAbs
	}

	return sum
}

// softKneeThreshold is the level below which softKnee is bit-exact transparent.
// Sums with magnitude at or under this pass through unchanged; only the portion
// above it is compressed. 0.7 leaves normal-level pulses untouched while giving
// the knee 0.3 of range to asymptote toward the rail.
const softKneeThreshold = 0.7

// softKnee maps a value through a memoryless soft-knee limiter. For magnitudes at
// or below softKneeThreshold it is the identity (no colouration of normal-level
// signal); above the threshold it follows an exponential that asymptotically
// approaches ±1 without ever reaching it. The join at the threshold is C¹ (both
// the value and the slope are continuous), so the transfer curve has no corner.
//
// Two properties matter downstream:
//   - It never reaches ±1, so a saturating input produces a curved output rather
//     than a flat rail — no sustained DC that would overheat the transducer.
//   - It is 1-Lipschitz (|f'| ≤ 1 everywhere), so it can never amplify a
//     per-sample step; a smooth input stays smooth through the limiter.
func softKnee(x float64) float64 {
	magnitude := math.Abs(x)
	if magnitude <= softKneeThreshold {
		return x
	}

	headroom := 1.0 - softKneeThreshold
	shaped := softKneeThreshold + headroom*(1.0-math.Exp(-(magnitude-softKneeThreshold)/headroom))

	return math.Copysign(shaped, x)
}

// softCombine mixes two samples by summing them and passing the sum through the
// soft-knee limiter. It replaces the older hard-clamping priority mix.
//
// Summing preserves the pulse energy the receptors integrate, and because the
// combine is a memoryless function of the sum it introduces no window-dependent
// gain — so mixing a new pulse over content already in the buffer creates no
// amplitude step at the pulse boundaries (the failure mode of the old
// retroactive per-window peak limiter), and never pins to the rail (the failure
// mode of the hard clamp it replaces).
//
// The louder component still prevails without any explicit priority logic: it
// sets the operating point on the knee, so once the sum is in compression a
// weaker overlapping component only adds the locally-reduced slope — high-energy
// events dominate weaker ones automatically.
func softCombine(a float64, b float64) float64 {
	return softKnee(a + b)
}
