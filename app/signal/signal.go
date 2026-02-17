// Package signal provides various signal processing functions.
package signal

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/config"
)

// Abs returns the absolute value of a float64.
func Abs(value float64) float64 {
	if value < 0 {
		return -value
	}

	return value
}

// Equalize applies equalization based on pulse width and synthesizer settings for a specific channel.
// Uses a precomputed EQ curve for efficient lookup instead of calculating per-sample.
func Equalize(value float64, pulseWidth float64, channel int, cfg *config.Config) float64 {
	// Check if EQ is enabled for this channel
	if !cfg.GetSynthChannelEqEnabled(channel) {
		return value
	}

	sampleRate := cfg.GetSynthInternalSampleRateHz()
	freq := float64(sampleRate) / (2 * pulseWidth)

	curve, minFreq, resolution := cfg.GetSynthChannelEqCurve(channel)
	if len(curve) == 0 {
		return value // No EQ curve computed
	}

	// Calculate bucket index for this frequency
	index := int((freq - minFreq) / resolution)

	// Bounds check
	if index < 0 || index >= len(curve) {
		return value // Outside EQ range
	}

	// Apply precomputed amplitude ratio
	return value * curve[index]
}

// DRXShift determines whether DRX (Dynamic Range Extension) should activate for a pulse
// and returns the optimal frequency and replacement amplitude. DRX exploits device
// resonance at EQ-attenuated frequencies to produce physical output above the digital
// 0dB ceiling.
//
// Algorithm:
//  1. Compute the desired boost from the unclamped jerk curve amplitude above 0dB
//  2. Find the nearest EQ bucket to the original frequency with sufficient attenuation
//  3. Set the digital amplitude so that device resonance produces the desired physical level
//  4. Fall back to the deepest bucket if no bucket has enough attenuation, capping at
//     whatever physical boost that bucket can provide
//
// The key insight is that EQ attenuation exists because the device resonates at those
// frequencies. By bypassing EQ and outputting at a resonant frequency, the device's natural
// amplification provides a physical boost above the normal flattened response.
//
// Parameters:
//   - pulseFrequencyHz: the original calculated pulse frequency
//   - unclampedAmplitude: the jerk curve output before LimitMax clamping (absolute value)
//   - channel: output channel index
//   - cfg: application configuration
//
// Returns the shifted frequency, the full replacement amplitude (not additive),
// the EQ ratio at the selected bucket, and true if DRX was activated.
func DRXShift(
	pulseFrequencyHz float64,
	unclampedAmplitude float64,
	channel int,
	cfg *config.Config,
) (drxFreqHz float64, drxAmplitude float64, bucketRatio float64, active bool) {
	maxAmplitude := cfg.GetHapticsPulseMaxAmplitude()

	// DRX requires both DRX and EQ to be enabled, and amplitude must exceed 0dB
	if !cfg.GetSynthDRXEnabled() || !cfg.GetSynthChannelEqEnabled(channel) || unclampedAmplitude <= maxAmplitude {
		return pulseFrequencyHz, 0, 0, false
	}

	curve, minFreq, resolution := cfg.GetSynthChannelEqCurve(channel)
	if len(curve) == 0 {
		return pulseFrequencyHz, 0, 0, false
	}

	// The required bucket ratio ensures the digital output stays within limits.
	// A bucket with this ratio (or lower) has enough resonance headroom to produce
	// the desired physical amplitude without digital clipping.
	requiredBucketRatio := maxAmplitude / unclampedAmplitude

	// Find the best bucket: nearest with sufficient attenuation, or deepest as fallback
	drxBucket, found := selectDRXBucket(curve, minFreq, resolution, pulseFrequencyHz, requiredBucketRatio)
	if !found {
		return pulseFrequencyHz, 0, 0, false
	}

	// Set digital amplitude so that device resonance produces the desired physical
	// level. The device amplifies by 1/bucketRatio at this frequency, so:
	//   physical = digital × (1 / bucketRatio) = unclampedAmplitude
	//   digital  = unclampedAmplitude × bucketRatio
	// Capped at maxAmplitude for the fallback case where the bucket is shallower
	// than ideal — the device still provides as much boost as the bucket allows.
	drxAmplitude = min(unclampedAmplitude*drxBucket.ratio, maxAmplitude)

	// Clamp the shifted frequency within the configured pulse frequency range
	drxFreqHz = max(drxBucket.frequencyHz, cfg.GetHapticsPulseMinHz())
	drxFreqHz = min(drxFreqHz, cfg.GetHapticsPulseMaxHz())

	return drxFreqHz, drxAmplitude, drxBucket.ratio, true
}

// Exponent raises a value to a given exponent.
// If the value is negative, the exponentiation is applied to its absolute value, and the sign is restored.
func Exponent(value float64, exponent float64) float64 {
	isNeg := false

	if value < 0 {
		isNeg = true
		value = -value
	}

	result := math.Pow(value, exponent)

	if isNeg {
		result = -result
	}

	return result
}

// LargestMagnitude returns the largest magnitude of two values, preserving their signs.
func LargestMagnitude(valueA float64, valueB float64) float64 {
	aIsNeg := false
	bIsNeg := false

	if valueA < 0 {
		aIsNeg = true
		valueA = -valueA
	}

	if valueB < 0 {
		bIsNeg = true
		valueB = -valueB
	}

	var maxVal float64

	if valueA > valueB {
		maxVal = valueA

		if aIsNeg {
			maxVal = -maxVal
		}
	} else {
		maxVal = valueB

		if bIsNeg {
			maxVal = -maxVal
		}
	}

	return maxVal
}

// LimitWindow constrains a value within a specified minimum and maximum range.
// If the value is negative, the constraints are applied to its absolute value, and the sign is restored.
func LimitWindow(value float64, minValue float64, maxValue float64) (limitedValue float64, wasLimited bool) {
	var cMin, cMax bool

	value, cMin = LimitMin(value, minValue)
	value, cMax = LimitMax(value, maxValue)

	if cMin || cMax {
		return value, true
	}

	return value, false
}

// LimitMin constrains a value to be no less than the specified minimum.
// If the value is negative, the minimum constraint is applied to its absolute value, and the sign is restored.
func LimitMin(value float64, minValue float64) (limitedValue float64, wasLimited bool) {
	isNeg := false
	if value < 0 {
		isNeg = true
		value = -value
	}

	original := value
	value = max(value, minValue)
	wasLimited = original != value

	if isNeg {
		value = -value
	}

	return value, wasLimited
}

// LimitMax constrains a value to be no greater than the specified maximum.
// If the value is negative, the maximum constraint is applied to its absolute value, and the sign is restored.
func LimitMax(value float64, maxValue float64) (limitedValue float64, wasLimited bool) {
	isNeg := false
	if value < 0 {
		isNeg = true
		value = -value
	}

	original := value
	value = min(value, maxValue)

	wasLimited = original != value

	if isNeg {
		value = -value
	}

	return value, wasLimited
}

// Log2 applies a base 2 logarithmic transformation to the given value.
// If the value is negative, the transformation is applied to its absolute value, and the sign is restored.
func Log2(value float64) float64 {
	isNeg := false

	if value < 0 {
		isNeg = true
		value = -value
	}

	compressed := math.Log2(value + 1)

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

// Log10 applies a base 10 logarithmic transformation to the given value.
// If the value is negative, the transformation is applied to its absolute value, and the sign is restored.
func Log10(value float64) float64 {
	isNeg := false

	if value < 0 {
		isNeg = true
		value = -value
	}

	compressed := math.Log10(value + 1)

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

// AmplitudeToDecibels converts a linear amplitude ratio to decibels (dB).
// A ratio of 1.0 corresponds to 0 dB, 0.5 to approximately -6 dB, and 2.0 to approximately +6 dB.
func AmplitudeToDecibels(ratio float64) float64 {
	return 20 * math.Log10(ratio)
}

// Scale multiplies the given value by the specified scale factor.
func Scale(value float64, scale float64) float64 {
	return value * scale
}

// Polarity returns -1.0 for negative values and +1.0 for zero or positive values.
func Polarity(value float64) float64 {
	if value < 0 {
		return -1.0
	}

	return 1.0
}

// drxBucket represents an EQ curve bucket with attenuation below unity.
type drxBucket struct {
	frequencyHz float64
	ratio       float64
}

// selectDRXBucket finds the best EQ bucket for DRX activation in a single pass.
// It prefers the bucket closest to targetFreqHz with ratio <= maxRatio (sufficient
// attenuation for the full boost). If no such bucket exists, it falls back to the
// deepest attenuated bucket (lowest ratio < 1.0) to provide a capped boost.
func selectDRXBucket(
	curve []float64,
	minFreq float64,
	resolution float64,
	targetFreqHz float64,
	maxRatio float64,
) (bucket drxBucket, found bool) {
	var nearest drxBucket

	nearestFound := false
	nearestDistance := math.Inf(1)

	var deepest drxBucket

	deepestFound := false
	deepest.ratio = 1.0

	for i, ratio := range curve {
		if ratio >= 1.0 {
			continue
		}

		freq := minFreq + float64(i)*resolution

		// Track deepest attenuated bucket as fallback
		if ratio < deepest.ratio {
			deepest = drxBucket{frequencyHz: freq, ratio: ratio}
			deepestFound = true
		}

		// Track nearest bucket with sufficient attenuation
		if ratio <= maxRatio {
			distance := math.Abs(freq - targetFreqHz)
			if !nearestFound || distance < nearestDistance {
				nearest = drxBucket{frequencyHz: freq, ratio: ratio}
				nearestDistance = distance
				nearestFound = true
			}
		}
	}

	if nearestFound {
		return nearest, true
	}

	return deepest, deepestFound
}
