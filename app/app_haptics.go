package app

import (
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/config"
)

// enableHaptics enables the haptic feedback system.
func (a *App) enableHaptics() {
	// speaker.Resume()
	a.state.raceCompleteTime = 0

	a.synth.FadeIn(config.FadeInDuration)
	a.state.hapticsEnabled = true

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Msg("haptics state change")
}

// disableHaptics disables the haptic feedback system.
// Takes a reason string for logging purposes.
func (a *App) disableHaptics(reason string) {
	// speaker.Suspend()
	a.synth.Silence()
	a.state.hapticsEnabled = false

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Str("reason", reason).Msg("haptics state change")
}

// generateForceHaptics generates haptic feedback based on the vehicle telemetry data.
func (a *App) generateForceHaptics() {
	startTime := time.Now()

	if !a.sequenceHasAdvanced() {
		return
	}

	// Disable haptics when the telemetry inactive or race is complete
	// Avoids haptics output after a race has finished and completion screens are yet to be dismissed
	if !a.telemetryIsActive() || a.raceHasFinished() {
		a.state.telemetryActive = false

		if a.state.hapticsEnabled {
			a.resetAppState()
			a.disableHaptics("not live")
		}

		return
	}

	a.state.telemetryActive = true

	if !a.state.hapticsEnabled {
		a.enableHaptics()
	}

	// a.ui.SetLive(true)

	a.kinematics.Current.SequenceID = a.state.current.sequenceNumber

	// no haptics if telemetry packets dropped/missed
	// if a.telemetryPacketsDropped() > 1 {
	// 	return
	// }

	windowSeconds := (float64(a.state.current.sequenceDelta) / frameRate)

	a.kinematics.Update(windowSeconds, a.gtClient)

	if a.gearHasChanged() {
		a.playGearShiftHaptic()
	}

	a.generateChassisHaptic()

	a.kinematics.Current.ComputeTime = time.Since(startTime)
	a.kinematics.Last = a.kinematics.Current

	if a.kinematics.Current.ComputeTime.Microseconds() > 16000 {
		a.log.Warn().Float64("ms", float64(a.kinematics.Current.ComputeTime.Milliseconds())).Msg("slow compute")
	}
}
