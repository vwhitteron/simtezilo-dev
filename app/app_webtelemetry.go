package app

import (
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
)

// channelFill returns the fill ratio of the named mixer channel, or 0 if the
// channel is not present in the diagnostics snapshot.
func channelFill(diag synthesizer.MixerDiagnostics, name string) float32 {
	for _, ch := range diag.Channels {
		if ch.Name == name {
			return float32(ch.Health.FillRatio)
		}
	}

	return 0
}

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

	// Snapshot the audio health/diagnostics once; both are derived per call.
	// This runs on the main loop goroutine, so it is safe to reuse and retain
	// the diagScratch backing array across calls.
	health := a.hapticSource.Health()
	diag := a.synth.DiagnosticsInto(a.diagScratch)
	a.diagScratch = diag.Channels
	latency := a.audioMon.BuildReport(health, diag, a.state.current.sequenceNumber)

	// Sequence-id gap: dropped telemetry packets since the previous frame.
	// sequenceDelta is 1 in the nominal case; guard the subtraction so a
	// zero delta cannot underflow the unsigned counter.
	var seqGap float32
	if a.state.current.sequenceDelta > 1 {
		seqGap = float32(a.state.current.sequenceDelta - 1)
	}

	var engineVibrationEnabled float32
	if a.gtClient.Telemetry.EngineRPM() > 0 {
		engineVibrationEnabled = 1
	}

	tyreTemp := a.gtClient.Telemetry.TyreTemperatureCelsius()

	// Per-channel synth amplitude/frequency and chassis mixer fill for the web
	// charts. These slices are sent across the buffered telemetry feed to the
	// broadcaster goroutine, so they must be freshly allocated per frame rather
	// than reused (a shared backing array would corrupt queued frames).
	numChannels := a.synth.NumOutputChannels()
	synthChannelAmplitude := make([]float32, numChannels)
	synthChannelFrequency := make([]float32, numChannels)
	mixerChassisFill := make([]float32, numChannels)

	for ch := range numChannels {
		synthChannelAmplitude[ch] = float32(channelValueAt(a.kinematics.Current.SynthChannelAmplitude, ch))
		synthChannelFrequency[ch] = float32(channelValueAt(a.kinematics.Current.SynthChannelFrequency, ch))
		mixerChassisFill[ch] = channelFill(diag, synthesizer.ChassisChannelName(ch))
	}

	frame := webui.TelemetryFrame{
		ComputeTime:                 float32(a.kinematics.Last.ComputeTime.Microseconds()),
		Seq:                         float32(a.state.current.sequenceNumber),
		SeqGap:                      seqGap,
		TimeOfDay:                   float32(a.gtClient.Telemetry.TimeOfDay().Milliseconds()),
		ThrottleInput:               a.gtClient.Telemetry.ThrottleInputPercent(),
		ThrottleOutput:              a.gtClient.Telemetry.ThrottleOutputPercent(),
		BrakeInput:                  a.gtClient.Telemetry.BrakeInputPercent(),
		BrakeOutput:                 a.gtClient.Telemetry.BrakeOutputPercent(),
		RPM:                         a.gtClient.Telemetry.EngineRPM(),
		Speed:                       a.gtClient.Telemetry.GroundSpeedKPH(),
		Gear:                        float32(a.kinematics.Current.TransmissionGear),
		FuelUsagePerKm:              float32(a.fuelRange.UsageRatePerKm()),
		FuelRangeKm:                 float32(a.fuelRange.DistanceMetres() / 1000),
		FuelRangeLaps:               float32(a.fuelRange.DistanceLaps(a.circuit.LengthMetres())),
		TyreTempFL:                  tyreTemp.FrontLeft,
		TyreTempFR:                  tyreTemp.FrontRight,
		TyreTempRL:                  tyreTemp.RearLeft,
		TyreTempRR:                  tyreTemp.RearRight,
		SurgeGforce:                 float32(a.kinematics.Current.SixDOFTranslation.Acceleration.Surge) / kinematics.GravityConstant,
		SurgeGforceCalc:             float32(a.kinematics.Current.SurgeCalculated) / kinematics.GravityConstant,
		SixDOFTranslationalJerk:     float32(a.kinematics.Current.SixDOFTranslation.Jerk),
		SixDOFTranslationalSnap:     float32(a.kinematics.Current.SixDOFTranslation.Snap),
		SixDOFTranslationalJerkCalc: float32(a.kinematics.Current.SixDOFTranslationCalc.Jerk),
		SixDOFTranslationalSnapCalc: float32(a.kinematics.Current.SixDOFTranslationCalc.Snap),
		SixDOFTranslationalAccelX:   float32(a.kinematics.Current.SixDOFTranslationCalc.Acceleration.X),
		SixDOFTranslationalAccelY:   float32(a.kinematics.Current.SixDOFTranslationCalc.Acceleration.Y),
		SixDOFTranslationalAccelZ:   float32(a.kinematics.Current.SixDOFTranslationCalc.Acceleration.Z),
		SixDOFRotationalJerk:        float32(a.kinematics.Current.SixDOFRotationCalc.Jerk),
		SixDOFRotationalSnap:        float32(a.kinematics.Current.SixDOFRotationCalc.Snap),
		SixDOFRotationalAccelX:      float32(a.kinematics.Current.SixDOFRotationCalc.Acceleration.X),
		SixDOFRotationalAccelY:      float32(a.kinematics.Current.SixDOFRotationCalc.Acceleration.Y),
		SixDOFRotationalAccelZ:      float32(a.kinematics.Current.SixDOFRotationCalc.Acceleration.Z),
		SynthChannelAmplitude:       synthChannelAmplitude,
		SynthChannelFrequency:       synthChannelFrequency,
		EngineVibrationEnabled:      engineVibrationEnabled,
		// Audio pipeline health metrics
		AsyncBufferFill:    float32(health.FillRatio),
		AsyncUnderruns:     float32(health.Underruns),
		AsyncProducerWaits: float32(health.ProducerWaits),
		MixerEngineFill:    channelFill(diag, "engine"),
		MixerChassisFill:   mixerChassisFill,
		// Haptic latency/drift monitor (milliseconds)
		EngineLatencyMs:  float32(latency.EngineLatencyMs),
		ChassisLatencyMs: float32(latency.ChassisLatencyMs),
		RingLatencyMs:    float32(latency.RingLatencyMs),
		DriftMs:          float32(latency.DriftMs),
		SeqJitterMs:      float32(latency.SeqJitterMs),
	}

	select {
	case a.telemetryChartFeed <- frame:
	default:
	}
}
