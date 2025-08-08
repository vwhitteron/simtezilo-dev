package app

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

func (a *App) hapticEvents() {
	startTime := time.Now()

	a.updateState()

	if !a.sequenceHasAdvanced() {
		return
	}

	if a.vehicleHasChanged() {
		a.resetState()
		a.disableHaptics("vehicle changed")

		a.updateVehicle()

		return
	}

	// Do nothing if telemetry is not indicating an active state
	if !a.telemetryIsActive() {
		if a.state.hapticsEnabled {
			a.resetState()
			a.disableHaptics("not live")
		}

		return
	}

	// The loading flag typically means the session has restarted
	if a.sessionHasReset() {
		a.resetState()
		a.disableHaptics("session reset")

		return
	}

	// Initialise the gear if it hasn't been set yet
	if a.state.last.gear == NullGear {
		a.resetState()
		a.disableHaptics("initialising gear")

		return
	}

	if !a.timeOfDayHasAdvanced() {
		a.resetState()
		a.disableHaptics("time of day reset")

		a.log.Debug().
			Uint32("sequence_id", a.state.current.seq).
			Str("current_time_of_day", a.state.current.timeOfDay.String()).
			Str("last_time_of_day", a.state.last.timeOfDay.String()).
			Msg("time of day reset")

		return
	}

	// if !a.vehicleIsInMotion() {
	// 	return
	// }

	if !a.state.hapticsEnabled {
		a.enableHaptics()
	}

	a.kinematics.Current.SequenceID = a.state.current.seq

	// no haptics if telemetry packets dropped/missed
	// if a.telemetryPacketsDropped() > 1 {
	// 	return
	// }

	windowSeconds := (float64(a.state.current.seqDelta) / frameRate)

	a.kinematics.Update(windowSeconds, a.gtClient)

	if a.gearHasChanged() {
		a.playGearChangeHaptic()
	}

	a.generateChassisHaptic()

	a.state.last = a.state.current
	a.kinematics.Current.ComputeTime = time.Since(startTime)
	a.kinematics.Last = a.kinematics.Current

	if a.kinematics.Current.ComputeTime.Microseconds() > 16000 {
		a.log.Warn().Float64("ms", float64(a.kinematics.Current.ComputeTime.Milliseconds())).Msg("slow compute")
	}

}

func (a *App) generateChassisHaptic() {
	startTime := time.Now()

	pulseFrequencyHz := a.calculateChassisHapticPulseFrequency()

	pulseWidth := math.Round(float64(a.config.Synthesizer.SampleRateHz) / (2 * pulseFrequencyHz))

	pulseAmplitude := a.calculateChassisHapticPulseAmplitude()

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	bufferLen := a.synth.GetBufferLength()
	pulseBuffer := make([]float64, bufferLen)

	for i := range int(pulseWidth * 2) {
		phase := waveSamplePeriod * (float64(i) - waveOffset)
		pulseBuffer[i] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
	}

	a.synth.WriteBuffer("chassis", pulseBuffer)

	// log large amplitude values
	if pulseAmplitude > 1.0 || pulseAmplitude < -1.0 {
		a.log.Debug().
			Float64("jerk", a.kinematics.Current.SixDOFTranslationCalc.Jerk).
			Float64("snap", a.kinematics.Current.SixDOFTranslationCalc.Snap).
			Str("process_time", time.Since(startTime).String()).
			Uint32("sequence_id", a.kinematics.Current.SequenceID).
			Msg("Bump inputs")
		a.log.Debug().
			Float64("amplitude", pulseAmplitude).
			Float64("samplePeriod", waveSamplePeriod).
			Float64("pulseWidth", pulseWidth).
			Msg("Bump outputs")
	}
}

func (a *App) calculateChassisHapticPulseAmplitude() float64 {
	sig := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Jerk, (a.kinematics.Current.SixDOFRotation.Jerk * snapMultiplier))
	pulseAmplitude := signal.Exponent(sig, a.config.GetJerkCurve())
	pulseAmplitude = signal.Scale(pulseAmplitude, a.config.GetJerkScale())

	p1 := pulseAmplitude
	pulseAmplitude, wasLimited := signal.LimitMax(pulseAmplitude, a.config.Synthesizer.PulseMaxAmplitude)
	if wasLimited {
		a.log.Debug().Float64("pulse", p1).Msg("limiter")
	}

	a.kinematics.Last.SynthOutputAmplitude = a.kinematics.Current.SynthOutputAmplitude
	a.kinematics.Current.SynthOutputAmplitude = pulseAmplitude

	return pulseAmplitude
}

func (a *App) calculateChassisHapticPulseFrequency() float64 {
	snap := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Snap, (a.kinematics.Current.SixDOFRotation.Snap * snapMultiplier))

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, a.config.GetSnapCurve()))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, a.config.GetSnapScale())
	pulseFrequencyHz := a.config.GetFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < a.config.GetMinHz() {
		pulseFrequencyHz = a.config.GetMinHz()
	} else if pulseFrequencyHz > a.config.GetMaxHz() {
		pulseFrequencyHz = a.config.GetMaxHz()
	}

	a.kinematics.Last.SynthOutputFrequency = a.kinematics.Current.SynthOutputFrequency
	a.kinematics.Current.SynthOutputFrequency = int(pulseFrequencyHz)

	return pulseFrequencyHz
}

func (a *App) disableHaptics(reason string) {
	// speaker.Suspend()
	a.synth.Silence()
	a.synth.ClearBuffer()
	a.state.hapticsEnabled = false

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Str("reason", reason).Msg("haptics state change")
}

func (a *App) enableHaptics() {
	// speaker.Resume()
	a.synth.FadeIn(3 * time.Second)
	a.state.hapticsEnabled = true

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Msg("haptics state change")
}

func (a *App) playGearChangeHaptic() {
	volumePercent := a.determineGearChangeVolume()

	a.synth.PlayEffectWithVolume("gearchange", volumePercent)
	a.log.Debug().
		Int("sequence_id", int(a.state.current.seq)).
		Int("volume_pc", volumePercent).
		Int("gear", a.kinematics.Current.TransmissionGear).
		Msg("gear change")
}

func (a *App) determineGearChangeVolume() int {
	volumeMaxPercent, _ := a.synth.GetChannelVolume("gearchange")

	if !a.config.DynamicGearShiftFeedbackEnabled() {
		return volumeMaxPercent
	}

	gForce := a.kinematics.GetSurgeGforce()
	gforceMax := a.config.GetGearShiftGforceMax()
	volumeCurve := a.config.GetGearShiftCurve()

	volumeMax := float64(volumeMaxPercent) / 100.0
	gearChangeVolume := (math.Pow((gForce/gforceMax), volumeCurve) * volumeMax)
	gearChangeVolume, _ = signal.LimitWindow(gearChangeVolume, a.gearVolumeMin, volumeMax)

	return int(gearChangeVolume * 100.0)
}
