package rotataionalenvelope

import (
	"math"

	gtmodels "github.com/zetetos/gt-telemetry/pkg/models"
)

func Delta(axis1 gtmodels.RotationalEnvelope, axis2 gtmodels.RotationalEnvelope) gtmodels.RotationalEnvelope {
	return gtmodels.RotationalEnvelope{
		Pitch: axis1.Pitch - axis2.Pitch,
		Yaw:   axis1.Yaw - axis2.Yaw,
		Roll:  axis1.Roll - axis2.Roll,
	}
}

func Magnitude(axis gtmodels.RotationalEnvelope) float64 {
	return math.Sqrt(float64(axis.Pitch*axis.Pitch + axis.Yaw*axis.Yaw + axis.Roll*axis.Roll))
}

func Scale(axis gtmodels.RotationalEnvelope, pitchScale float32, yawScale float32, rollScale float32) gtmodels.RotationalEnvelope {
	return gtmodels.RotationalEnvelope{
		Pitch: axis.Pitch * pitchScale,
		Yaw:   axis.Yaw * yawScale,
		Roll:  axis.Roll * rollScale,
	}
}
