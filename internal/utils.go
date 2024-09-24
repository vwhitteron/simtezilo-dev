package internal

import (
	"math"
	"strconv"

	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

func gearName(gearNum int) string {
	gearName, ok := gearNames[gearNum]
	if !ok {
		gearName = strconv.Itoa(gearNum)
	}

	return gearName
}

func vectorDelta(vector1 telemetry_client.Vector, vector2 telemetry_client.Vector) telemetry_client.Vector {
	return telemetry_client.Vector{
		X: vector1.X - vector2.X,
		Y: vector1.Y - vector2.Y,
		Z: vector1.Z - vector2.Z,
	}
}

func vectorMagnitude(vector telemetry_client.Vector) float64 {
	return math.Sqrt(float64(vector.X*vector.X + vector.Y*vector.Y + vector.Z*vector.Z))
}

func vectorMagnitudeZBiased(vector telemetry_client.Vector) float64 {
	xComp := vector.X * vector.X * 0.25
	yComp := vector.Y * vector.Y * 0.25
	zComp := vector.Z * vector.Z

	return math.Sqrt(float64(xComp + yComp + zComp))
}

func volumeToGain(volume float64) float64 {
	return math.Pow(10, (volume / 10))
}
