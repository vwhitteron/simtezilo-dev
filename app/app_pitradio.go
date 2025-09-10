package app

import (
	"fmt"
	"strings"
	"time"
)

const positionDebounceTime = 3 * time.Second

// commsState tracks state specifically for Discord communications
// This is separate from appState to avoid interference from haptic ticker resets
type commsState struct {
	lastLap          uint16
	lastLapTime      time.Duration
	lastRaceProgress int8
	lastPosition     int16

	position           int16
	positionNotifyTime time.Time // Debounce position changes until this time is reached
}

func (a *App) resetCommsState() {
	a.commsState = &commsState{
		lastLap:            uint16(a.gtClient.Telemetry.CurrentLap()),
		lastLapTime:        a.gtClient.Telemetry.LastLaptime(),
		lastPosition:       a.gtClient.Telemetry.StartingPosition(), // TODO: switch to GridPosition when gt-telelmetry updated
		positionNotifyTime: time.Now().Add(24 * time.Hour),
	}

	a.log.Info().
		Str("component", "app").
		Str("state", fmt.Sprintf("%+v", a.state)).
		Msg("state reset")
}

func (a *App) sendPitRadioMessage() {
	if a.pitRadio == nil {
		return
	}

	if !a.telemetryIsActive() {
		return
	}

	if a.timeOfDayHasReset() {
		a.resetCommsState()
	}

	// Initialize comms state tracker if not already done
	if a.commsState == nil {
		a.resetCommsState()

		return
	}

	if a.positionHasChanged() {
		a.notifyPosition()
	}

	// Check for new lap and send lap time message
	if a.isNewLap() {
		a.notifyLapTime()

		time.Sleep(2 * time.Second) // Brief pause to avoid message overlap

		a.notifyLapNumber()

		// Update the comms state tracker after processing
		a.commsState.lastLap = a.state.current.lap
		a.commsState.lastLapTime = a.state.current.lapTime
	}
}

// isNewLap checks for new lap using the dedicated comms state tracker
func (a *App) isNewLap() bool {
	if a.commsState == nil {
		return false
	}

	if a.state.current.lap <= 0 {
		return false
	}

	// Check if current lap is greater than the last tracked lap for comms
	// and ensure we have valid lap data
	return a.state.current.lap > a.commsState.lastLap
}

// positionHasChanged checks if the grid position has changed since the last update
func (a *App) positionHasChanged() bool {
	if a.commsState == nil {
		return false
	}

	if a.state.current.lap <= 0 {
		return false
	}

	position := a.gtClient.Telemetry.StartingPosition()

	if position <= 0 {
		return false
	}

	if position == a.commsState.lastPosition {
		return false
	} else if a.commsState.position != position {
		a.commsState.position = position
		a.commsState.positionNotifyTime = time.Now().Add(positionDebounceTime)

		a.log.Debug().
			Str("component", "discord").
			Int16("new_position", position).
			Int16("old_position", a.commsState.lastPosition).
			Msg("Position change")
	}

	// Debounce position changes until time delay reached
	if time.Now().Before(a.commsState.positionNotifyTime) {
		return false
	}

	// Reset debounce timer
	a.commsState.positionNotifyTime = time.Now().Add(24 * time.Hour)
	a.commsState.lastPosition = a.commsState.position

	return true
}

func (a *App) notifyPosition() {
	message := fmt.Sprintf("P%d", a.commsState.position)

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Failed to send position change message")
		} else {
			a.log.Debug().
				Str("component", "discord").
				Str("message", message).
				Uint16("lap", a.state.current.lap).
				Msg("Position change message sent")
		}
	}
}

// notifyLapTime sends lap time notifications to Discord
func (a *App) notifyLapTime() {
	if a.state.current.lapTime <= 0 {
		return
	}

	message := notifyDuration(a.state.current.lapTime)

	// Check if this lap matches or beats the best lap time
	bestLapTime := a.gtClient.Telemetry.BestLaptime()
	if bestLapTime > 0 && a.state.current.lapTime <= bestLapTime {
		message = "lap record. " + message
	}

	if a.state.current.lapTime > bestLapTime {
		message = "Down " + notifyDuration(a.state.current.lapTime-bestLapTime) + " seconds"
	}

	// Send lap time message to Discord
	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Failed to send lap time message")
		} else {
			a.log.Debug().
				Str("component", "discord").
				Str("message", message).
				Uint16("lap", a.state.current.lap).
				Dur("lapTime", a.state.current.lapTime).
				Msg("Lap time message sent")
		}
	}
}

func (a *App) notifyLapNumber() {
	if a.state.current.lap == 0 {
		return
	}

	raceLaps := a.gtClient.Telemetry.RaceLaps()
	longRace := raceLaps > 10
	raceProgress := int8(100*float64(a.state.current.lap)/float64(raceLaps)) % 4
	lapsRemaining := raceLaps - a.state.current.lap + 1

	a.log.Info().
		Uint16("lap", a.state.current.lap).
		Uint16("raceLaps", raceLaps).
		Int8("lastProgress", a.commsState.lastRaceProgress).
		Int8("progress", raceProgress).
		Msg("Lap progress")

	message := ""
	switch {
	case a.state.current.lap == raceLaps:
		message = "final lap"
	case raceLaps-a.state.current.lap <= 3 && longRace:
		message = fmt.Sprintf("%d laps remaining", lapsRemaining)
	case raceProgress > a.commsState.lastRaceProgress && raceProgress == 3 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.lap, lapsRemaining)
	case raceProgress > a.commsState.lastRaceProgress && raceProgress == 2:
		message = fmt.Sprintf("Lap %d, halfway there", a.state.current.lap)
	case raceProgress > a.commsState.lastRaceProgress && raceProgress == 1 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.lap, lapsRemaining)
	}

	a.commsState.lastRaceProgress = raceProgress

	if message == "" {
		return
	}

	// Send lap number message to Discord
	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Failed to send lap number message")
		} else {
			a.log.Debug().
				Str("component", "discord").
				Str("message", message).
				Uint16("lap", a.state.current.lap).
				Msg("Lap number message sent")
		}
	}
}

// notifyDuration formats a time.Duration value for text and speech output
func notifyDuration(lapTime time.Duration) string {
	minutes := int(lapTime.Minutes())
	lapTime = lapTime - (time.Duration(minutes) * time.Minute)

	seconds := int(lapTime.Seconds())
	lapTime = lapTime - (time.Duration(seconds) * time.Second)

	milliseconds := int(lapTime.Milliseconds())

	minutesStr := fmt.Sprintf("%d", minutes)

	var secondsStr string
	if seconds == 0 {
		secondsStr = "0"
	} else {
		secondsFmt := "%02d"
		if minutesStr == "0" {
			secondsFmt = "%d"
		}

		secondsStr = fmt.Sprintf(secondsFmt, seconds)
	}

	millisecondsStr := fmt.Sprintf("%03d", milliseconds)

	fmt.Printf("%s:%s.%s\n", minutesStr, secondsStr, millisecondsStr)

	return pronounceTime(minutesStr, secondsStr, millisecondsStr, false)
}

// pronounceTime formats minutes, seconds and millisecond time components for text and speech output
func pronounceTime(minutes string, seconds string, milliseconds string, includeUnits bool) string {
	announce := []string{}

	if minutes != "0" {
		announce = append(announce, minutes)
		if includeUnits {
			announce = append(announce, "minutes")
		}
	}

	announce = append(announce, seconds)
	// if includeUnits {
	announce = append(announce, "point")
	// }

	for _, r := range milliseconds {
		rune := string(r)

		if rune == "0" {
			rune = "oh"
		}

		announce = append(announce, rune)
	}

	return strings.Join(announce, " ")
}
