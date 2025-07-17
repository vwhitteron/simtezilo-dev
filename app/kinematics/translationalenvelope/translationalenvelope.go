package translationalenvelope

import (
	"math"

	telemetry_client "github.com/zetetos/gt-telemetry"
)

func Delta(
	e1 telemetry_client.TranslationalEnvelope,
	e2 telemetry_client.TranslationalEnvelope,
) telemetry_client.TranslationalEnvelope {
	return telemetry_client.TranslationalEnvelope{
		Sway:  e1.Sway - e2.Sway,
		Heave: e1.Heave - e2.Heave,
		Surge: e1.Surge - e2.Surge,
	}
}

func Magnitude(e telemetry_client.TranslationalEnvelope) float64 {
	return math.Sqrt(float64(e.Sway*e.Sway + e.Heave*e.Heave + e.Surge*e.Surge))
}

func Scale(
	e telemetry_client.TranslationalEnvelope,
	swayScale float32,
	heaveScale float32,
	surgeScale float32,
) telemetry_client.SymmetryAxes {
	return telemetry_client.SymmetryAxes{
		Pitch: e.Sway * swayScale,
		Yaw:   e.Heave * heaveScale,
		Roll:  e.Surge * surgeScale,
	}
}
