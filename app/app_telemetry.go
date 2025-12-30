package app

import (
	"time"

	gtmodels "github.com/zetetos/gt-telemetry/pkg/models"
)

// checkForNewLap sends an event to the lapStartEvents channel when a new lap is detected.
func (a *App) checkForNewLap() {
	current := a.state.current.lapNumber
	last := a.state.last.lapNumber
	lapDelta := current - last

	//nolint:gocritic // if-else chain is clearer here
	if lapDelta == 1 {
		a.lapStartEvents <- a.state.current.sequenceNumber

		a.log.Debug().
			Int16("current", a.state.current.lapNumber).
			Int16("previous", a.state.last.lapNumber).
			Str("status", "normal").
			Msg("New lap started")
	} else if lapDelta > 1 {
		a.log.Debug().
			Int16("current", a.state.current.lapNumber).
			Int16("previous", a.state.last.lapNumber).
			Str("status", "skip forwards").
			Msg("New lap started")
	} else if lapDelta < 0 {
		a.log.Debug().
			Int16("current", a.state.current.lapNumber).
			Int16("previous", a.state.last.lapNumber).
			Str("status", "skip backwards or reset").
			Msg("New lap started")
	}
}

func (a *App) checkRaceComplete() {
	lastLap := a.state.last.lapNumber
	currentLap := a.state.current.lapNumber
	raceLaps := a.gtClient.Telemetry.RaceLaps()

	// TODO: handle endurance races, time trials and free practice sessions
	if raceLaps == 0 {
		a.state.raceCompleteTime = time.Time{}

		return
	}

	// Invalid state
	if lastLap > currentLap || lastLap >= raceLaps {
		a.state.raceCompleteTime = time.Time{}

		return
	}

	if currentLap >= raceLaps {
		if a.state.raceCompleteTime.IsZero() {
			a.state.raceCompleteTime = time.Now()
		}

		a.log.Info().
			Int16("current_lap", currentLap).
			Int16("race_laps", raceLaps).
			Msg("Race complete")

		return
	}
}

// sequenceHasAdvanced checks if the telemetry sequence number has advanced.
func (a *App) sequenceHasAdvanced() bool {
	if a.state.current.sequenceNumber == 0 || a.state.current.sequenceDelta == 0 {
		return false
	}

	return true
}

// telemetryIsActive checks if the telemetry is in an active state.
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
	if a.config.GetHapticsEnableReplay() {
		return true
	}

	return false
}

// telemetryPacketsDropped checks if telemetry packets have been dropped.
// Returns the count of dropped packets.
func (a *App) telemetryPacketsDropped() uint32 {
	dropped := a.state.current.sequenceDelta - 1

	if dropped > 1 {
		a.log.Debug().Uint32("dropped", dropped).Msg("telemetry packets")
	}

	return dropped
}

// timeOfDayHasReset checks if the time of day has reset (gone backwards).
func (a *App) timeOfDayHasReset() bool {
	if a.state.current.sequenceNumber == 0 {
		return false
	}

	timeOfDayDelta := a.state.current.timeOfDay - a.state.last.timeOfDay

	return timeOfDayDelta.Milliseconds() < 0
}

// gameStateHasChanged checks if the game state has changed between updates.
// For main menu state, requires 3 consecutive frames to avoid false positives from transient flickering.
func (a *App) gameStateHasChanged() bool {
	current := a.state.current.gameState
	last := a.state.last.gameState

	return current != last
}

// liveFlagHasChanged checks if the live flag has changed between live and replay modes.
func (a *App) liveFlagHasChanged() bool {
	return a.state.current.isLive != a.state.last.isLive
}

// updateState copies the current state to the previous state and updates the current
// state with the latest telemetry data.
// State updates are skipped if the sequence ID has not changed.
// A boolean is returned indicating if the statewas updated (true) or not (false).
func (a *App) updateState() (didUpdate bool) {
	if a.gtClient.Telemetry.SequenceID() == a.state.current.sequenceNumber {
		return false
	}

	a.state.last = a.state.current

	// Game
	gameState := a.gtClient.Telemetry.GameState()
	if gameState == gtmodels.GameStateMainMenu {
		// Main menu state changes are delayed to avoid flapping (switch vehicle, start replay, etc)
		if a.state.mainMenuFrameCount >= 10 {
			a.state.current.gameState = gtmodels.GameStateMainMenu
		} else {
			a.state.mainMenuFrameCount++
		}
	} else {
		// Other game state changes are immediate
		a.state.mainMenuFrameCount = 0
		a.state.current.gameState = gameState
	}

	// Session
	a.state.current.sequenceNumber = a.gtClient.Telemetry.SequenceID()
	a.state.current.sequenceDelta = a.state.current.sequenceNumber - a.state.last.sequenceNumber
	a.state.current.timeOfDay = a.gtClient.Telemetry.TimeOfDay()
	a.state.current.isLive = a.gtClient.Telemetry.Flags().Live

	// Vehicle
	a.state.current.transmissionGear = a.gtClient.Telemetry.CurrentGear()

	// Race
	a.state.current.lapNumber = a.gtClient.Telemetry.CurrentLap()
	a.state.current.lastLapTime = a.gtClient.Telemetry.LastLaptime()

	return true
}

// vehicleHasChanged checks if the vehicle has changed based on telemetry data.
func (a *App) vehicleHasChanged() bool {
	return a.vehicle.ID != a.gtClient.Telemetry.VehicleID()
}

// vehicleIsOnTrack checks if the vehicle is on track based on telemetry data.
// When in the menu system the race laps will be set to uin16 max.
// When at a  track screen before a session has started, the race laps will be set to 0.
func (a *App) vehicleIsOnTrack() bool {
	return a.gtClient.Telemetry.RaceLaps() < 32000
}
