package app

import (
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
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

	if a.kinematics.Current.SequenceID == a.webSequenceID {
		return
	}

	a.webSequenceID = a.kinematics.Current.SequenceID

	go func() {
		// Snapshot the audio health/diagnostics once; both are derived per call.
		health := a.hapticSource.Health()
		diag := a.synth.Diagnostics()
		latency := a.audioMon.BuildReport(health, diag, a.state.current.sequenceNumber)
		channelFill := func(name string) float32 {
			for _, ch := range diag.Channels {
				if ch.Name == name {
					return float32(ch.Health.FillRatio)
				}
			}

			return 0
		}

		// Sequence-id gap: dropped telemetry packets since the previous frame.
		// sequenceDelta is 1 in the nominal case; guard the subtraction so a
		// zero delta cannot underflow the unsigned counter.
		var seqGap float32
		if a.state.current.sequenceDelta > 1 {
			seqGap = float32(a.state.current.sequenceDelta - 1)
		}

		a.telemetryChartFeed <- map[string]float32{
			"computeTime":                 float32(a.kinematics.Last.ComputeTime.Microseconds()),
			"seq":                         float32(a.state.current.sequenceNumber),
			"seqGap":                      seqGap,
			"timeOfDay":                   float32(a.gtClient.Telemetry.TimeOfDay().Milliseconds()),
			"throttleInput":               a.gtClient.Telemetry.ThrottleInputPercent(),
			"throttleOutput":              a.gtClient.Telemetry.ThrottleOutputPercent(),
			"brakeInput":                  a.gtClient.Telemetry.BrakeInputPercent(),
			"brakeOutput":                 a.gtClient.Telemetry.BrakeOutputPercent(),
			"rpm":                         a.gtClient.Telemetry.EngineRPM(),
			"speed":                       a.gtClient.Telemetry.GroundSpeedKPH(),
			"gear":                        float32(a.kinematics.Current.TransmissionGear),
			"fuelUsagePerKm":              float32(a.fuelRange.UsageRatePerKm()),
			"fuelRangeKm":                 float32(a.fuelRange.DistanceMetres() / 1000),
			"fuelRangeLaps":               float32(a.fuelRange.DistanceLaps(a.circuit.LengthMetres())),
			"tyreTempFL":                  a.gtClient.Telemetry.TyreTemperatureCelsius().FrontLeft,
			"tyreTempFR":                  a.gtClient.Telemetry.TyreTemperatureCelsius().FrontRight,
			"tyreTempRL":                  a.gtClient.Telemetry.TyreTemperatureCelsius().RearLeft,
			"tyreTempRR":                  a.gtClient.Telemetry.TyreTemperatureCelsius().RearRight,
			"surgeGforce":                 float32(a.kinematics.Current.SixDOFTranslation.Acceleration.Surge) / kinematics.GravityConstant,
			"surgeGforceCalc":             float32(a.kinematics.Current.SurgeCalculated) / kinematics.GravityConstant,
			"SixDOFTranslationalJerk":     float32(a.kinematics.Current.SixDOFTranslation.Jerk),
			"SixDOFTranslationalSnap":     float32(a.kinematics.Current.SixDOFTranslation.Snap),
			"SixDOFTranslationalJerkCalc": float32(a.kinematics.Current.SixDOFTranslationCalc.Jerk),
			"SixDOFTranslationalSnapCalc": float32(a.kinematics.Current.SixDOFTranslationCalc.Snap),
			"SixDOFTranslationalAccelX":   float32(a.kinematics.Current.SixDOFTranslationCalc.Acceleration.X),
			"SixDOFTranslationalAccelY":   float32(a.kinematics.Current.SixDOFTranslationCalc.Acceleration.Y),
			"SixDOFTranslationalAccelZ":   float32(a.kinematics.Current.SixDOFTranslationCalc.Acceleration.Z),
			"SixDOFRotationalJerk":        float32(a.kinematics.Current.SixDOFRotationCalc.Jerk),
			"SixDOFRotationalSnap":        float32(a.kinematics.Current.SixDOFRotationCalc.Snap),
			"SixDOFRotationalAccelX":      float32(a.kinematics.Current.SixDOFRotationCalc.Acceleration.X),
			"SixDOFRotationalAccelY":      float32(a.kinematics.Current.SixDOFRotationCalc.Acceleration.Y),
			"SixDOFRotationalAccelZ":      float32(a.kinematics.Current.SixDOFRotationCalc.Acceleration.Z),
			"synthChannelAmplitudeL":      float32(a.kinematics.Current.SynthChannelAmplitude[0]),
			"synthChannelFrequencyL":      float32(a.kinematics.Current.SynthChannelFrequency[0]),
			"synthChannelAmplitudeR":      float32(a.kinematics.Current.SynthChannelAmplitude[1]),
			"synthChannelFrequencyR":      float32(a.kinematics.Current.SynthChannelFrequency[1]),
			"engineVibrationEnabled": func() float32 {
				if a.gtClient.Telemetry.EngineRPM() > 0 {
					return 1
				}

				return 0
			}(),
			// Audio pipeline health metrics
			"asyncBufferFill":    float32(health.FillRatio),
			"asyncUnderruns":     float32(health.Underruns),
			"asyncProducerWaits": float32(health.ProducerWaits),
			"mixerEngineFill":    channelFill("engine"),
			"mixerChassis0Fill":  channelFill("chassis_0"),
			// Haptic latency/drift monitor (milliseconds)
			"engineLatencyMs":  float32(latency.EngineLatencyMs),
			"chassisLatencyMs": float32(latency.ChassisLatencyMs),
			"ringLatencyMs":    float32(latency.RingLatencyMs),
			"driftMs":          float32(latency.DriftMs),
			"seqJitterMs":      float32(latency.SeqJitterMs),
		}
	}()
}
