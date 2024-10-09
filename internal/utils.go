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

func symmetryAxisDelta(axis1 telemetry_client.SymmetryAxes, axis2 telemetry_client.SymmetryAxes) telemetry_client.SymmetryAxes {
	return telemetry_client.SymmetryAxes{
		Pitch: axis1.Pitch - axis2.Pitch,
		Yaw:   axis1.Yaw - axis2.Yaw,
		Roll:  axis1.Roll - axis2.Roll,
	}
}

func symmetryAxisMagnitude(axis telemetry_client.SymmetryAxes) float64 {
	return math.Sqrt(float64(axis.Pitch*axis.Pitch + axis.Yaw*axis.Yaw + axis.Roll*axis.Roll))
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

func vectorMagnitudeBiased(vector telemetry_client.Vector, xScale float32, yScale float32, zScale float32) float64 {
	xComp := vector.X * vector.X * xScale
	yComp := vector.Y * vector.Y * yScale
	zComp := vector.Z * vector.Z * zScale

	return math.Sqrt(float64(xComp + yComp + zComp))
}

func volumeToGain(volume float64) float64 {
	return math.Pow(10, (volume / 10))
}
