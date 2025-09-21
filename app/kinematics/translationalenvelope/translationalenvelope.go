package translationalenvelope

import (
	"math"

	gtmodels "github.com/zetetos/gt-telemetry/pkg/models"
)

func Delta(
	e1 gtmodels.TranslationalEnvelope,
	e2 gtmodels.TranslationalEnvelope,
) gtmodels.TranslationalEnvelope {
	return gtmodels.TranslationalEnvelope{
		Sway:  e1.Sway - e2.Sway,
		Heave: e1.Heave - e2.Heave,
		Surge: e1.Surge - e2.Surge,
	}
}

func Magnitude(e gtmodels.TranslationalEnvelope) float64 {
	return math.Sqrt(float64(e.Sway*e.Sway + e.Heave*e.Heave + e.Surge*e.Surge))
}

func Scale(
	e gtmodels.TranslationalEnvelope,
	swayScale float32,
	heaveScale float32,
	surgeScale float32,
) gtmodels.RotationalEnvelope {
	return gtmodels.RotationalEnvelope{
		Pitch: e.Sway * swayScale,
		Yaw:   e.Heave * heaveScale,
		Roll:  e.Surge * surgeScale,
	}
}
