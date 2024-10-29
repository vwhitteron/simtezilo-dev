package internal

import (
	"strconv"
)

func gearName(gearNum int) string {
	gearName, ok := gearNames[gearNum]
	if !ok {
		gearName = strconv.Itoa(gearNum)
	}

	return gearName
}

func pulseWidthToFrequency(samples float64) float64 {
	return synthSampleRateHz / (2 * samples)
}

// (((1000 / frequencyHz) / 2) / 1000) * sampleRate
func frequencyToPulseWidth(hertz float64) float64 {
	return synthSampleRateHz / (2 * hertz)
}
