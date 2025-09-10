package app

import (
	"fmt"
	"strings"
	"time"
)

const positionDebounceTime = 3 * time.Second

func (a *App) resetPitRadioState() {
	a.pitRadioState = &pitRadioState{
		lastNotifiedLapNumber:  a.gtClient.Telemetry.CurrentLap(),
		lastNotifiedLapTime:    a.gtClient.Telemetry.LastLaptime(),
		lastNotifiedPosition:   a.gtClient.Telemetry.StartingPosition(), // TODO: switch to GridPosition when gt-telemetry updated
		positionNotifyDebounce: time.Now().Add(24 * time.Hour),
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
		a.resetPitRadioState()
	}

	// Initialize pit radio state tracker if not already done
	if a.pitRadioState == nil {
		a.resetPitRadioState()

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

		// Update the pit radio state tracker after processing
		a.pitRadioState.lastNotifiedLapNumber = a.state.current.currentLapNumber
		a.pitRadioState.lastNotifiedLapTime = a.state.current.lastLapTime
	}
}

// isNewLap checks for new lap using the dedicated pit radio state tracker
func (a *App) isNewLap() bool {
	if a.pitRadioState == nil {
		return false
	}

	if a.state.current.currentLapNumber <= 0 {
		return false
	}

	// Check if current lap is greater than the last tracked lap for pit radio
	// and ensure we have valid lap data
	return a.state.current.currentLapNumber > a.pitRadioState.lastNotifiedLapNumber
}

// positionHasChanged checks if the grid position has changed since the last update
func (a *App) positionHasChanged() bool {
	if a.pitRadioState == nil {
		return false
	}

	if a.state.current.currentLapNumber <= 0 {
		return false
	}

	position := a.gtClient.Telemetry.StartingPosition()

	if position <= 0 {
		return false
	}

	if position == a.pitRadioState.lastNotifiedPosition {
		return false
	} else if a.pitRadioState.currentPosition != position {
		a.pitRadioState.currentPosition = position
		a.pitRadioState.positionNotifyDebounce = time.Now().Add(positionDebounceTime)

		a.log.Debug().
			Str("component", "discord").
			Int16("new_position", position).
			Int16("old_position", a.pitRadioState.lastNotifiedPosition).
			Msg("Position change")
	}

	// Debounce position changes until time delay reached
	if time.Now().Before(a.pitRadioState.positionNotifyDebounce) {
		return false
	}

	// Reset debounce timer
	a.pitRadioState.positionNotifyDebounce = time.Now().Add(24 * time.Hour)
	a.pitRadioState.lastNotifiedPosition = a.pitRadioState.currentPosition

	return true
}

func (a *App) notifyPosition() {
	message := fmt.Sprintf("P%d", a.pitRadioState.currentPosition)

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
				Int16("lap", a.state.current.currentLapNumber).
				Msg("Position change message sent")
		}
	}
}

// notifyLapTime sends lap time notifications to Discord
func (a *App) notifyLapTime() {
	if a.state.current.lastLapTime <= 0 {
		return
	}

	message := notifyDuration(a.state.current.lastLapTime)

	// Check if this lap matches or beats the best lap time
	bestLapTime := a.gtClient.Telemetry.BestLaptime()
	if bestLapTime > 0 && a.state.current.lastLapTime <= bestLapTime {
		message = "lap record. " + message
	}

	if a.state.current.lastLapTime > bestLapTime {
		message = "Down " + notifyDuration(a.state.current.lastLapTime-bestLapTime) + " seconds"
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
				Int16("lap", a.state.current.currentLapNumber).
				Dur("lapTime", a.state.current.lastLapTime).
				Msg("Lap time message sent")
		}
	}
}

func (a *App) notifyLapNumber() {
	if a.state.current.currentLapNumber == 0 {
		return
	}

	raceLaps := int16(a.gtClient.Telemetry.RaceLaps())
	longRace := raceLaps > 10
	raceProgress := int8(100*float64(a.state.current.currentLapNumber)/float64(raceLaps)) % 4
	lapsRemaining := raceLaps - a.state.current.currentLapNumber + 1

	a.log.Info().
		Int16("lap", a.state.current.currentLapNumber).
		Int16("raceLaps", raceLaps).
		Int8("lastProgress", a.pitRadioState.lastRaceProgress).
		Int8("progress", raceProgress).
		Msg("Lap progress")

	message := ""
	switch {
	case a.state.current.currentLapNumber == raceLaps:
		message = "final lap"
	case raceLaps-a.state.current.currentLapNumber <= 3 && longRace:
		message = fmt.Sprintf("%d laps remaining", lapsRemaining)
	case raceProgress > a.pitRadioState.lastRaceProgress && raceProgress == 3 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.currentLapNumber, lapsRemaining)
	case raceProgress > a.pitRadioState.lastRaceProgress && raceProgress == 2:
		message = fmt.Sprintf("Lap %d, halfway there", a.state.current.currentLapNumber)
	case raceProgress > a.pitRadioState.lastRaceProgress && raceProgress == 1 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.currentLapNumber, lapsRemaining)
	}

	a.pitRadioState.lastRaceProgress = raceProgress

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
				Int16("lap", a.state.current.currentLapNumber).
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
