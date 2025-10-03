package signal

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/config"
)

func Abs(value float64) float64 {
	if value < 0 {
		return -value
	}

	return value
}

func Equalize(value float64, pulseWidth float64, synth *config.Synthesizer) float64 {
	freq := int(math.Round(float64(synth.InternalSampleRateHz) / (2 * pulseWidth)))

	if freq < 10 || freq > 49 {
		return value
	}

	value = value * synth.Eq[freq-10]

	return value
}

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

func LimitWindow(value float64, min float64, max float64) (float64, bool) {
	var cMin, cMax bool

	value, cMin = LimitMin(value, min)
	value, cMax = LimitMax(value, max)

	if cMin || cMax {
		return value, true
	}

	return value, false
}

func LimitMin(value float64, min float64) (float64, bool) {
	isNeg := false
	if value < 0 {
		isNeg = true
		value = -value
	}

	value = max(value, min)

	if value < min {
		value = min
	}

	if isNeg {
		value = -value
	}

	return value, false
}

func LimitMax(value float64, max float64) (float64, bool) {
	isNeg := false
	if value < 0 {
		isNeg = true
		value = -value
	}

	value = min(value, max)

	if isNeg {
		value = -value
	}

	return value, false
}

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

func Scale(value float64, scale float64) float64 {
	return value * scale
}

func Polarity(value float64) float64 {
	if value < 0 {
		return -1.0
	}

	return 1.0
}
