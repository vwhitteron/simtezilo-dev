package rotataionalenvelope

import (
	"math"

	"github.com/zetetos/gt-telemetry/pkg/models"
)

func Delta(axis1 models.RotationalEnvelope, axis2 models.RotationalEnvelope) models.RotationalEnvelope {
	return models.RotationalEnvelope{
		Pitch: axis1.Pitch - axis2.Pitch,
		Yaw:   axis1.Yaw - axis2.Yaw,
		Roll:  axis1.Roll - axis2.Roll,
	}
}

func Magnitude(axis models.RotationalEnvelope) float64 {
	return math.Sqrt(float64(axis.Pitch*axis.Pitch + axis.Yaw*axis.Yaw + axis.Roll*axis.Roll))
}

func Scale(axis models.RotationalEnvelope, pitchScale float32, yawScale float32, rollScale float32) models.RotationalEnvelope {
	return models.RotationalEnvelope{
		Pitch: axis.Pitch * pitchScale,
		Yaw:   axis.Yaw * yawScale,
		Roll:  axis.Roll * rollScale,
	}
}
