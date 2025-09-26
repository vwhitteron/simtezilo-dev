package translationalenvelope

import (
	"math"

	"github.com/zetetos/gt-telemetry/pkg/models"
)

func Delta(
	e1 models.TranslationalEnvelope,
	e2 models.TranslationalEnvelope,
) models.TranslationalEnvelope {
	return models.TranslationalEnvelope{
		Sway:  e1.Sway - e2.Sway,
		Heave: e1.Heave - e2.Heave,
		Surge: e1.Surge - e2.Surge,
	}
}

func Magnitude(e models.TranslationalEnvelope) float64 {
	return math.Sqrt(float64(e.Sway*e.Sway + e.Heave*e.Heave + e.Surge*e.Surge))
}

func Scale(
	e models.TranslationalEnvelope,
	swayScale float32,
	heaveScale float32,
	surgeScale float32,
) models.RotationalEnvelope {
	return models.RotationalEnvelope{
		Pitch: e.Sway * swayScale,
		Yaw:   e.Heave * heaveScale,
		Roll:  e.Surge * surgeScale,
	}
}
