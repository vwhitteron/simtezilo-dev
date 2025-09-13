package app

import (
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/config"
)

// enableHaptics enables the haptic feedback system.
func (a *App) enableHaptics() {
	// speaker.Resume()
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

// hapticEvents generates haptic feedback based on the vehicle telemetry data.
func (a *App) hapticEvents() {
	startTime := time.Now()

	if didUpdate := a.updateState(); !didUpdate {
		return
	}

	if !a.sequenceHasAdvanced() {
		return
	}

	if a.vehicleHasChanged() {
		a.resetState(resetTrackData)
		a.disableHaptics("vehicle changed")

		a.updateVehicle()

		return
	}

	// Disable haptics when the telemetry inactive
	if !a.telemetryIsActive() {
		a.state.telemetryActive = false

		if a.state.hapticsEnabled {
			a.resetState(retainTrackData)
			a.disableHaptics("not live")
		}

		return
	}

	a.state.telemetryActive = true

	// The loading flag typically means the session has restarted or the car has pitted
	if a.sessionHasReset() {
		a.resetState(retainTrackData)
		a.disableHaptics("session reset")

		return
	}

	if a.timeOfDayHasReset() {
		a.resetState(retainTrackData)
		a.disableHaptics("time of day reset")

		a.log.Debug().
			Uint32("sequence_id", a.state.current.sequenceNumber).
			Str("current_time_of_day", a.state.current.timeOfDay.String()).
			Str("last_time_of_day", a.state.last.timeOfDay.String()).
			Msg("time of day reset")

		return
	}

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
