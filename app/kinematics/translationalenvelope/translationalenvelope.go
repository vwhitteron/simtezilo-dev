package translationalenvelope

import (
	"math"

	"github.com/zetetos/gt-telemetry/pkg/models"
)

func Delta(
	envelope1 models.TranslationalEnvelope,
	envelope2 models.TranslationalEnvelope,
) models.TranslationalEnvelope {
	return models.TranslationalEnvelope{
		Sway:  envelope1.Sway - envelope2.Sway,
		Heave: envelope1.Heave - envelope2.Heave,
		Surge: envelope1.Surge - envelope2.Surge,
	}
}

func Magnitude(e models.TranslationalEnvelope) float64 {
	return math.Sqrt(float64(e.Sway*e.Sway + e.Heave*e.Heave + e.Surge*e.Surge))
}

func Scale(
	envelope models.TranslationalEnvelope,
	swayScale float32,
	heaveScale float32,
	surgeScale float32,
) models.RotationalEnvelope {
	return models.RotationalEnvelope{
		Pitch: envelope.Sway * swayScale,
		Yaw:   envelope.Heave * heaveScale,
		Roll:  envelope.Surge * surgeScale,
	}
}
