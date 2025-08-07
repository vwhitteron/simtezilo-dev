package app

import (
	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

func (a *App) updateState() {
	a.state.last = a.state.current

	a.state.current.seq = a.gtClient.Telemetry.SequenceID()
	a.state.current.timeOfDay = a.gtClient.Telemetry.TimeOfDay()
	a.state.current.vehicleID = a.gtClient.Telemetry.VehicleID()
	a.state.current.gear = a.gtClient.Telemetry.CurrentGear()
}

func (a *App) sequenceHasAdvanced() bool {
	a.state.current.seqDelta = a.state.current.seq - a.state.last.seq

	if a.state.current.seq == 0 || a.state.current.seqDelta == 0 {
		return false
	}

	return true
}

func (a *App) timeOfDayHasAdvanced() bool {
	timeOfDayDelta := a.state.current.timeOfDay - a.state.last.timeOfDay
	if timeOfDayDelta < 0 {
		return false
	}

	return true
}

func (a *App) telemetryIsActive() bool {
	if a.gtClient.Telemetry.Flags().GamePaused {
		return false
	}

	if a.gtClient.Telemetry.Flags().Live || a.config.App.ReplayMode {
		return true
	}

	return false
}

func (a *App) telemetryPacketsDropped() uint32 {
	dropped := a.state.current.seqDelta - 1

	if dropped > 1 {
		a.log.Debug().Uint32("dropped", dropped).Msg("telemetry packets")
	}

	return dropped
}

func (a *App) vehicleHasChanged() bool {
	return a.state.current.vehicleID != a.state.last.vehicleID
}

// no haptics when vehicle comes to a controlled stop
// TODO: check angular velocity, etc to enable for uncontrolled stops
// if vector.Magnitude(c.kinematics.Current.Velocity.Vector) >= 0.28 {
func (a *App) vehicleIsInMotion() bool {
	lastMag := vector.Magnitude(a.kinematics.Last.SixDOFTranslationCalc.Velocity)
	currentMag := vector.Magnitude(a.kinematics.Current.SixDOFTranslationCalc.Velocity)
	if signal.LargestMagnitude(lastMag, currentMag) >= 0.28 {
		return true
	}

	return false
}
