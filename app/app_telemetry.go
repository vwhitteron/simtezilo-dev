package app

func (a *App) updateState() {
	a.state.last = a.state.current

	a.state.current.seq = a.gtClient.Telemetry.SequenceID()
	a.state.current.seqDelta = a.state.current.seq - a.state.last.seq
	a.state.current.timeOfDay = a.gtClient.Telemetry.TimeOfDay()
	a.state.current.vehicle.vehicleID = a.gtClient.Telemetry.VehicleID()
	// TODO: Uncomment when gt-telemetry is updated
	// a.state.current.vehicle.engineLayout = a.gtClient.Telemetry.EngineLayout()
	a.state.current.gear = a.gtClient.Telemetry.CurrentGear()
}

// vehicleIsOnTrack checks if the vehicle is on track based on telemetry data.
// When in the menu system the race laps will be set to uin16 max.
// When at a  track screen before a session has started, the race laps will be set to 0.
func (a *App) vehicleIsOnTrack() bool {
	return a.gtClient.Telemetry.RaceLaps() < 65000
}

func (a *App) sequenceHasAdvanced() bool {
	if a.state.current.seq == 0 || a.state.current.seqDelta == 0 {
		return false
	}

	return true
}

func (a *App) timeOfDayHasReset() bool {
	timeOfDayDelta := a.state.current.timeOfDay - a.state.last.timeOfDay
	return timeOfDayDelta.Milliseconds() < 0
}

func (a *App) telemetryIsActive() bool {
	if a.gtClient.Telemetry.Flags().GamePaused {
		return false
	}

	if !a.vehicleIsOnTrack() {
		return false
	}

	if a.gtClient.Telemetry.Flags().Live {
		return true
	}

	// If in replay mode, telmetry is considered active if the time of day has advanced.
	if a.config.App.ReplayMode {
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
	return a.state.current.vehicle.vehicleID != a.state.last.vehicle.vehicleID
}
