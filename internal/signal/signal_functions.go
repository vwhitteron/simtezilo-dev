package signal

import "math"

func Exponent(source float64, exponent float64) float64 {
	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	result := math.Pow(source, exponent)

	if isNeg {
		result = -result
	}

	return result
}

func Log2(source float64) float64 {
	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	compressed := math.Log2(source + 1)

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

func Log10(source float64) float64 {
	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	compressed := math.Log10(source + 1)

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

func Scale(source float64, scale float64) float64 {

	return source * scale
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

func LargestMagnitude(a float64, b float64) float64 {
	aIsNeg := false
	bIsNeg := false

	if a < 0 {
		aIsNeg = true
		a = -a
	}

	if b < 0 {
		bIsNeg = true
		b = -b
	}

	maxVal := 0.0

	if a > b {
		maxVal = a

		if aIsNeg {
			maxVal = -maxVal
		}
	} else {
		maxVal = b

		if bIsNeg {
			maxVal = -maxVal
		}
	}

	return maxVal
}
