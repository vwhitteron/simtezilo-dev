package signal

import "math"

func Abs(value float64) float64 {
	if value < 0 {
		return -value
	}

	return value
}

func Equalize(value float64, pulseWidth float64) float64 {
	eq := map[int]float64{
		10: 1.4,
		11: 1.4,
		12: 1.4,
		13: 1.4,
		14: 1.4,
		15: 1.4,
		16: 1.4,
		17: 1.4,
		18: 1.4,
		19: 1.35,
		20: 1.5,
		21: 1.4,
		22: 1.3,
		23: 1.2,
		24: 1.1,
		25: 1,
		26: 1,
		27: 1,
		28: 1,
		29: 1,
		30: 1,
		31: 1,
		32: 1,
		33: 1,
		34: 1,
		35: 1,
		36: 1,
		37: 1,
		38: 1,
		39: 1,
		40: 1,
	}

	freq := int(math.Round(8000 / (2 * pulseWidth)))

	if _, ok := eq[freq]; ok {
		value = value * eq[freq]
	}

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

	maxVal := 0.0

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

func Limit(value float64, max float64) (float64, bool) {
	if value > max {
		return max, true
	}

	if value < -max {
		return -max, true
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
