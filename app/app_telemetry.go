package app

func (a *App) updateState() {
	a.state.last = a.state.current

	a.state.current.seq = a.gtClient.Telemetry.SequenceID()
	a.state.current.seqDelta = a.state.current.seq - a.state.last.seq
	a.state.current.timeOfDay = a.gtClient.Telemetry.TimeOfDay()
	a.state.current.vehicleID = a.gtClient.Telemetry.VehicleID()
	a.state.current.gear = a.gtClient.Telemetry.CurrentGear()
	
	// Update lap, position, and lap time data for Discord notifications
	a.state.current.lap = uint16(a.gtClient.Telemetry.CurrentLap())
	a.state.current.lapTime = a.gtClient.Telemetry.LastLaptime()
	
	// Note: Gran Turismo telemetry doesn't provide race position directly
	// Position tracking would need to be implemented via external data or calculated
	// For now, we'll leave position tracking as a placeholder for future enhancement
	// a.state.current.position = // No direct method available in GT telemetry
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
	if a.config.GetAppReplayMode() {
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
