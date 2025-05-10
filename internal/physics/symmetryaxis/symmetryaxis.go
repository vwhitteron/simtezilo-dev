package symmetryaxis

import (
	"math"

	telemetry_client "github.com/zetetos/gt-telemetry"
)

func Delta(axis1 telemetry_client.SymmetryAxes, axis2 telemetry_client.SymmetryAxes) telemetry_client.SymmetryAxes {
	return telemetry_client.SymmetryAxes{
		Pitch: axis1.Pitch - axis2.Pitch,
		Yaw:   axis1.Yaw - axis2.Yaw,
		Roll:  axis1.Roll - axis2.Roll,
	}
}

func Magnitude(axis telemetry_client.SymmetryAxes) float64 {
	return math.Sqrt(float64(axis.Pitch*axis.Pitch + axis.Yaw*axis.Yaw + axis.Roll*axis.Roll))
}

func Scale(axis telemetry_client.SymmetryAxes, pitchScale float32, yawScale float32, rollScale float32) telemetry_client.SymmetryAxes {
	return telemetry_client.SymmetryAxes{
		Pitch: axis.Pitch * pitchScale,
		Yaw:   axis.Yaw * yawScale,
		Roll:  axis.Roll * rollScale,
	}
}
