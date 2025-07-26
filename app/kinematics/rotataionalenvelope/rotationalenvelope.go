package rotataionalenvelope

import (
	"math"

	telemetry_client "github.com/zetetos/gt-telemetry"
)

func Delta(axis1 telemetry_client.RotationalEnvelope, axis2 telemetry_client.RotationalEnvelope) telemetry_client.RotationalEnvelope {
	return telemetry_client.RotationalEnvelope{
		Pitch: axis1.Pitch - axis2.Pitch,
		Yaw:   axis1.Yaw - axis2.Yaw,
		Roll:  axis1.Roll - axis2.Roll,
	}
}

func Magnitude(axis telemetry_client.RotationalEnvelope) float64 {
	return math.Sqrt(float64(axis.Pitch*axis.Pitch + axis.Yaw*axis.Yaw + axis.Roll*axis.Roll))
}

func Scale(axis telemetry_client.RotationalEnvelope, pitchScale float32, yawScale float32, rollScale float32) telemetry_client.RotationalEnvelope {
	return telemetry_client.RotationalEnvelope{
		Pitch: axis.Pitch * pitchScale,
		Yaw:   axis.Yaw * yawScale,
		Roll:  axis.Roll * rollScale,
	}
}
