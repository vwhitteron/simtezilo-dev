package app

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

func (a *App) hapticEvents() {
	startTime := time.Now()

	seq := a.gtClient.Telemetry.SequenceID()

	// Do nothing until the sequence ID advances
	seqDelta := seq - a.state.seq
	if seq == 0 || seqDelta == 0 {
		return
	}

	currentGear := a.gtClient.Telemetry.CurrentGear()
	currentVehicleID := a.gtClient.Telemetry.VehicleID()

	if a.state.vehicleID != currentVehicleID {
		a.resetState(seq, currentGear)
		a.disableHaptics("vehicle changed")

		go a.updateVehicle(currentVehicleID, currentGear)
		a.log.Debug().Uint32("ID", currentVehicleID).Msg("vehicle ID changed")

		return
	}

	if !a.shouldGenerateHaptics() {
		if a.state.hapticsEnabled {
			a.resetState(seq, currentGear)
			a.disableHaptics("not live")
		}

		return
	}

	// The loading flag typically means the session has restarted
	if a.sessionHasReset(seq) {
		a.resetState(seq, currentGear)
		a.disableHaptics("session reset")

		return
	}

	// Initialise the gear if it hasn't been set yet
	if a.state.lastGear == NullGear {
		a.state.seq = seq
		a.state.lastGear = currentGear

		a.resetState(seq, currentGear)
		a.disableHaptics("initialising gear")

		return
	}

	currentTimeOfDay := a.gtClient.Telemetry.TimeOfDay()
	timeOfDayDelta := currentTimeOfDay - a.state.timeOfDay
	a.state.timeOfDay = currentTimeOfDay
	if timeOfDayDelta < 0 {
		a.state.seq = seq

		a.resetState(seq, currentGear)
		a.disableHaptics("time of day reset")

		a.log.Debug().
			Uint32("sequence_id", seq).
			Str("time_of_day_delta", timeOfDayDelta.String()).
			Msg("time of day reset")

		return
	}

	if !a.state.hapticsEnabled {
		a.enableHaptics()
	}

	a.kinematics.Current.SequenceID = seq

	a.processHaptics(seqDelta)

	a.state.seq = seq
	a.kinematics.Current.ComputeTime = time.Since(startTime)
	a.kinematics.Last = a.kinematics.Current

	if a.kinematics.Current.ComputeTime.Microseconds() > 16000 {
		a.log.Warn().Float64("ms", float64(a.kinematics.Current.ComputeTime.Milliseconds())).Msg("slow compute")
	}

}

func (a *App) processHaptics(seqDelta uint32) {
	// no haptics if sequence ID has not advanced
	if seqDelta < 1 {
		return
	}

	windowMilliseconds := (float64(seqDelta) / frameRate)

	a.kinematics.Update(windowMilliseconds, a.gtClient)

	// no haptics if telemetry packets dropped/missed
	if seqDelta > 1 {
		a.log.Debug().Uint32("delta", seqDelta).Msg("missed packets")
	}

	if a.hasGearChanged() {
		a.playGearChangeHaptic()
	}

	a.generateBump()
}

func (a *App) generateBump() {
	startTime := time.Now()

	snap := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Snap, (a.kinematics.Current.SixDOFRotation.Snap * 200))

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, a.config.GetSnapCurve()))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, a.config.GetSnapScale())
	pulseFrequencyHz := a.config.GetFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < a.config.GetMinHz() {
		pulseFrequencyHz = a.config.GetMinHz()
	} else if pulseFrequencyHz > a.config.GetMaxHz() {
		pulseFrequencyHz = a.config.GetMaxHz()
	}

	pulseWidth := math.Round(float64(a.config.Synthesizer.SampleRateHz) / (2 * pulseFrequencyHz))

	sig := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Jerk, (a.kinematics.Current.SixDOFRotation.Jerk * 200))
	pulseAmplitude := signal.Exponent(sig, a.config.GetJerkCurve())
	pulseAmplitude = signal.Scale(pulseAmplitude, a.config.GetJerkScale())

	p1 := pulseAmplitude
	pulseAmplitude, wasLimited := signal.LimitMax(pulseAmplitude, a.config.Synthesizer.PulseMaxAmplitude)
	if wasLimited {
		a.log.Debug().Float64("pulse", p1).Msg("limiter")
	}

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	bufferLen := a.synth.GetBufferLength()
	pulseBuffer := make([]float64, bufferLen)
	for i := range int(pulseWidth * 2) {
		phase := waveSamplePeriod * (float64(i) - waveOffset)
		pulseBuffer[i] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
	}

	// no haptics when vehicle comes to a controlled stop
	// TODO: check angular velocity, etc to enable for uncontrolled stops
	// if vector.Magnitude(c.kinematics.Current.Velocity.Vector) >= 0.28 {
	lastMag := vector.Magnitude(a.kinematics.Last.SixDOFTranslationCalc.Velocity)
	currentMag := vector.Magnitude(a.kinematics.Current.SixDOFTranslationCalc.Velocity)
	if signal.LargestMagnitude(lastMag, currentMag) >= 0.28 {
		a.synth.WriteBuffer("chassis", pulseBuffer)
	}

	a.kinematics.Last.SynthOutputAmplitude = a.kinematics.Current.SynthOutputAmplitude
	a.kinematics.Current.SynthOutputAmplitude = pulseAmplitude

	a.kinematics.Last.SynthOutputFrequency = a.kinematics.Current.SynthOutputFrequency
	a.kinematics.Current.SynthOutputFrequency = int(pulseFrequencyHz)

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

func (a *App) shouldGenerateHaptics() bool {
	if a.gtClient.Telemetry.Flags().GamePaused {
		return false
	}

	if !a.gtClient.Telemetry.Flags().Live && !a.config.App.ReplayMode {
		return false
	}

	return true
}

func (a *App) playGearChangeHaptic() {
	volumePercent := a.determineGearChangeVolume()

	a.synth.PlayEffectWithVolume("gearchange", volumePercent)
	a.log.Debug().
		Int("sequence_id", int(a.state.seq)).
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
