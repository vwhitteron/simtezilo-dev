package app

import (
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

func (a *App) sendTelemetryChartData() {
	if a.webUI == nil {
		return
	}

	if !a.webUI.HasActiveClients() {
		return
	}

	if a.gtClient.Telemetry.Flags().GamePaused {
		return
	}

	if a.gtClient.Finished {
		return
	}

	if a.kinematics.Current.SequenceID == a.webSequenceId {
		return
	}

	a.webSequenceId = a.kinematics.Current.SequenceID

	go func() {
		a.telemetryChartFeed <- map[string]float32{
			"computeTime":                 float32(a.kinematics.Last.ComputeTime.Microseconds()),
			"seq":                         float32(a.state.seq),
			"timeOfDay":                   float32(a.gtClient.Telemetry.TimeOfDay().Milliseconds()),
			"throttleInput":               a.gtClient.Telemetry.ThrottleInputPercent(),
			"throttleOutput":              a.gtClient.Telemetry.ThrottleOutputPercent(),
			"brakeInput":                  a.gtClient.Telemetry.BrakeInputPercent(),
			"brakeOutput":                 a.gtClient.Telemetry.BrakeOutputPercent(),
			"rpm":                         a.gtClient.Telemetry.EngineRPM(),
			"speed":                       a.gtClient.Telemetry.GroundSpeedKPH(),
			"gear":                        float32(a.kinematics.Current.TransmissionGear),
			"surgeGforce":                 float32(a.kinematics.Current.SixDOFTranslation.Acceleration) / kinematics.GravityConstant,
			"surgeGforceCalc":             float32(a.kinematics.Current.SurgeCalculated) / kinematics.GravityConstant,
			"SixDOFTranslationalJerk":     float32(a.kinematics.Current.SixDOFTranslation.Jerk),
			"SixDOFTranslationalSnap":     float32(a.kinematics.Current.SixDOFTranslation.Snap),
			"SixDOFTranslationalJerkCalc": float32(a.kinematics.Current.SixDOFTranslationCalc.Jerk),
			"SixDOFTranslationalSnapCalc": float32(a.kinematics.Current.SixDOFTranslationCalc.Snap),
			"SixDOFRotationalJerk":        float32(a.kinematics.Current.SixDOFRotation.Jerk), // * 50),
			"SixDOFRotationalSnap":        float32(a.kinematics.Current.SixDOFRotation.Snap),
			"synthOutputAmplitude":        float32(signal.Abs(float64(a.kinematics.Current.SynthOutputAmplitude))),
			"synthOutputFrequency":        float32(a.kinematics.Current.SynthOutputFrequency),
		}
	}()
}
