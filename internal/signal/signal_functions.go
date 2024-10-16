package signal

import "math"

func Abs(value float64) float64 {
	if value < 0 {
		return -value
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

func Limit(value float64, max float64) float64 {
	if value > max {
		return max
	}

	if value < -max {
		return -max
	}

	return value
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
