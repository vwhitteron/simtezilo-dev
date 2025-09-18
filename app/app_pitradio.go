package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/translations"
)

const (
	positionDebounceTime = 5 * time.Second // Suppress position change notifications for this duration
	messagePause         = 5 * time.Second // Pause between pit radio messages
)

// pitRadioState tracks Discord/pit radio communication state
type pitRadioState struct {
	// Nofification tracking to prevent duplicate or noisy messages
	fuelNotifyPrewarnIssued     bool          // Flag indicating whether the fuel pre-warn notification has been sent
	fuelNotifyEmptyIssued       bool          // Flag indicating whether the fuel empty notification has been sent
	lastNotifiedLapFuelCritical int16         // Last lap number when a critical fuel warning was sent
	lastNotifiedLapFuelWarning  int16         // Last lap number when a fuel warning was sent
	lastNotifiedLapFuelStrategy int16         // Last lap number when a fuel strategy was sent
	lastNotifiedLapNumber       int16         // Last notified lap number
	lastNotifiedLapTime         time.Duration // Last notified lap time
	lastNotifiedRaceProgress    int8          // Last notified race progress percentage
	lastNotifiedGridPosition    int16         // Last notified grid position of the vehicle
	debounceGirdPositionNotify  time.Time     // Suppress grid position change notifications until this time

	// Grid position tracking
	currentGridPosition int16 // Current race position of the vehicle
}

func (a *App) resetPitRadioState() {
	a.pitRadioState = &pitRadioState{
		lastNotifiedLapNumber:      a.gtClient.Telemetry.CurrentLap(),
		lastNotifiedLapTime:        a.gtClient.Telemetry.LastLaptime(),
		lastNotifiedGridPosition:   a.gtClient.Telemetry.GridPosition(),
		debounceGirdPositionNotify: time.Now().Add(24 * time.Hour),
	}

	a.log.Debug().
		Str("state", fmt.Sprintf("%+v", a.state)).
		Msg("pit radio state reset")
}

func (a *App) sendPitRadioMessage() {
	if a.pitRadio == nil {
		return
	}

	if !a.sequenceHasAdvanced() {
		return
	}

	if !a.telemetryIsActive() {
		return
	}

	if a.pitRadioState == nil {
		a.resetPitRadioState()

		return
	}

	if a.state.current.lapNumber <= 0 {
		return
	}

	a.notifyFuelWarnings()

	a.notifyGridPositionChange()
}

func (a *App) newLapNotificationHandler() {
	a.log.Debug().
		Str("handler", "new lap notification").
		Msg("Start")

	for {
		select {
		case <-a.lapStartEvents:
			time.Sleep(250 * time.Millisecond) // Wait for lap data to stabilise

			a.notifyLapTime()

			time.Sleep(messagePause)

			a.notifyLapNumber()

			time.Sleep(messagePause)

			a.notifyLapProgress()
		default:
			time.Sleep(8 * time.Millisecond)
		}
	}
}

// positionHasChanged checks if the grid position has changed since the last update
func (a *App) positionHasChanged() bool {
	if a.pitRadioState == nil {
		return false
	}

	if a.state.current.lapNumber <= 0 {
		return false
	}

	position := a.gtClient.Telemetry.GridPosition()

	if position <= 0 {
		return false
	}

	if position == a.pitRadioState.lastNotifiedGridPosition {
		return false
	} else if a.pitRadioState.currentGridPosition != position {
		a.pitRadioState.currentGridPosition = position
		a.pitRadioState.debounceGirdPositionNotify = time.Now().Add(positionDebounceTime)

		a.log.Debug().
			Int16("new_position", position).
			Int16("old_position", a.pitRadioState.lastNotifiedGridPosition).
			Msg("Position change")
	}

	// Debounce position changes until time delay reached
	if time.Now().Before(a.pitRadioState.debounceGirdPositionNotify) {
		return false
	}

	// Reset debounce timer
	a.pitRadioState.debounceGirdPositionNotify = time.Now().Add(24 * time.Hour)
	a.pitRadioState.lastNotifiedGridPosition = a.pitRadioState.currentGridPosition

	return true
}

// notifyGridPositionChange sends position change notifications over the pit radio
func (a *App) notifyGridPositionChange() {
	if !a.positionHasChanged() {
		return
	}

	message := fmt.Sprintf("P%d", a.pitRadioState.currentGridPosition)

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send position change message")
		} else {
			a.log.Debug().
				Str("message", message).
				Int16("lap", a.state.current.lapNumber).
				Msg("Send position change message")
		}
	}
}

// notifyLapTime sends lap time notifications over the pit radio
func (a *App) notifyLapTime() {
	if a.pitRadioState == nil {
		return
	}

	a.pitRadioState.lastNotifiedLapTime = a.state.current.lastLapTime

	if a.state.current.lastLapTime <= 0 {
		return
	}

	var message string
	bestLapTime := a.gtClient.Telemetry.BestLaptime()

	// TODO: add config option to notify all laps or best lap only
	if bestLapTime > 0 && a.state.current.lastLapTime <= bestLapTime {
		message = fmt.Sprintf("%s. %s",
			a.i18n.GetString(translations.RadioLapRecord),
			notifyDuration(a.state.current.lastLapTime),
		)
	} else if a.state.current.lastLapTime > bestLapTime {
		message = fmt.Sprintf("Down %s seconds",
			notifyDuration(a.state.current.lastLapTime-bestLapTime))
	} else {
		message = notifyDuration(a.state.current.lastLapTime)
	}

	// Send lap time message to Discord
	err := a.pitRadio.Send(message)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "pit radio").
			Str("message", message).
			Msg("Send lap time message")
	} else {
		a.log.Debug().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Dur("lapTime", a.state.current.lastLapTime).
			Msg("Send lap time message")
	}
}

func (a *App) notifyLapNumber() {
	if a.pitRadioState == nil {
		return
	}

	a.pitRadioState.lastNotifiedLapNumber = a.state.current.lapNumber
	if a.state.current.lapNumber <= 0 {
		return
	}

	message := fmt.Sprintf("Lap %d", a.state.current.lapNumber)

	err := a.pitRadio.Send(message)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("message", message).
			Msg("Send lap number message")
	} else {
		a.log.Debug().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Msg("Send lap number message")
	}
}

// notifyLapProgress sends lap number notifications over the pit radio
func (a *App) notifyLapProgress() {
	if a.pitRadioState == nil {
		return
	}

	a.pitRadioState.lastNotifiedLapNumber = a.state.current.lapNumber

	if a.state.current.lapNumber == 0 {
		return
	}

	raceLaps := int16(a.gtClient.Telemetry.RaceLaps())
	if raceLaps == 0 {
		// TODO: handle endurance races
		return
	}

	longRace := raceLaps > 10
	lapsRemaining := raceLaps - a.state.current.lapNumber + 1
	raceProgressPercent := int8(100 * float64(a.state.current.lapNumber) / float64(raceLaps))

	var currentQuarter int8
	switch {
	case raceProgressPercent >= 75:
		currentQuarter = 3
	case raceProgressPercent >= 50:
		currentQuarter = 2
	case raceProgressPercent >= 25:
		currentQuarter = 1
	default:
		currentQuarter = 0
	}

	message := ""
	switch {
	case lapsRemaining <= 0:
		message = a.i18n.GetString(translations.RadioRaceFinish)
	case a.state.current.lapNumber == raceLaps:
		message = a.i18n.GetString(translations.RadioFinalLap)
	case lapsRemaining <= 3 && longRace:
		format := a.i18n.GetString(translations.RadioLapsRemaining)
		message = fmt.Sprintf(format, lapsRemaining)
	case currentQuarter > a.pitRadioState.lastNotifiedRaceProgress && currentQuarter == 3 && longRace:
		format := a.i18n.GetString(translations.RadioLapsWithRemaining)
		message = fmt.Sprintf(format, a.state.current.lapNumber, lapsRemaining)
	case currentQuarter > a.pitRadioState.lastNotifiedRaceProgress && currentQuarter == 2:
		format := a.i18n.GetString(translations.RadioLapsHalfway)
		message = fmt.Sprintf(format, a.state.current.lapNumber)
	case currentQuarter > a.pitRadioState.lastNotifiedRaceProgress && currentQuarter == 1 && longRace:
		format := a.i18n.GetString(translations.RadioLapsWithRemaining)
		message = fmt.Sprintf(format, a.state.current.lapNumber, lapsRemaining)
	}

	a.pitRadioState.lastNotifiedRaceProgress = currentQuarter

	if message == "" {
		return
	}

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send lap number message")
		} else {
			a.log.Debug().
				Str("message", message).
				Int16("lap", a.state.current.lapNumber).
				Msg("Send lap number message")
		}
	}
}

func (a *App) notifyFuelWarnings() {
	if a.fuelRange == nil {
		return
	}

	circuitLengthMeters := a.circuit.LengthMeters()
	if circuitLengthMeters <= 0 {
		return
	}

	currentLap := a.gtClient.Telemetry.CurrentLap()

	remainingLaps := float64(a.gtClient.Telemetry.RaceLaps()) - float64(currentLap)

	fuelRangeLaps := a.fuelRange.DistanceLaps(circuitLengthMeters)
	fuelRangeLapsSafe := fuelRangeLaps - a.config.GetFuelRangeSafetyMarginLaps()
	fuelRangeMeters := a.fuelRange.DistanceMeters()
	lapProgress := a.circuit.LapProgress()
	fuelRangeLapsUntilBox := fuelRangeLapsSafe + lapProgress

	var message string
	suppressNotify := true

	fuelEmpty := a.gtClient.Telemetry.FuelLevelPercent() <= 0
	fuelCritical := fuelRangeLapsUntilBox <= 0.5
	boxThisLap := fuelRangeLapsUntilBox <= 1
	boxInAFewLaps := fuelRangeLapsUntilBox <= a.config.GetFuelPreWarnNotifyLaps()+a.config.GetFuelRangeSafetyMarginLaps()
	fuelStrategyUpdate := remainingLaps > fuelRangeLaps && int16(currentLap)%int16(a.config.GetFuelStrategyNotifyLaps()) == 0

	// Critical fuel warnings based on lap range
	switch {
	case fuelEmpty:
		// Fuel eempty
		message = a.i18n.GetString(translations.RadioOutOfFuel)

		if !a.pitRadioState.fuelNotifyEmptyIssued {
			suppressNotify = false
			a.pitRadioState.fuelNotifyEmptyIssued = true
			a.pitRadioState.lastNotifiedLapFuelWarning = currentLap
		}
	case fuelCritical:
		// Fuel will run out during this lap
		message = a.i18n.GetString(translations.RadioFuelCritical)

		if a.pitRadioState.lastNotifiedLapFuelCritical != currentLap {
			suppressNotify = false
			a.pitRadioState.lastNotifiedLapFuelCritical = currentLap
		}
	case boxThisLap:
		// Fuel will run out during the next lap
		message = a.i18n.GetString(translations.RadioBoxForFuel)

		if a.pitRadioState.lastNotifiedLapFuelWarning != currentLap {
			suppressNotify = false
			a.pitRadioState.lastNotifiedLapFuelWarning = currentLap
		}
	case boxInAFewLaps:
		// Early warning when range drops below threshold plus pre-warn buffer
		format := a.i18n.GetString(translations.RadioFuelPreWarn)
		message = fmt.Sprintf(format, int(fuelRangeLapsUntilBox))

		if !a.pitRadioState.fuelNotifyPrewarnIssued {
			suppressNotify = false
			a.pitRadioState.fuelNotifyPrewarnIssued = true
			a.pitRadioState.lastNotifiedLapFuelWarning = currentLap
		}
	case fuelStrategyUpdate:
		// Periodic fuel range updates when insufficient fuel for the remainder of the race
		format := a.i18n.GetString(translations.RadioFuelRange)
		message = fmt.Sprintf(format, int(fuelRangeLaps), int(remainingLaps))

		if a.pitRadioState.lastNotifiedLapFuelWarning == currentLap {
			suppressNotify = true
		} else if a.pitRadioState.lastNotifiedLapFuelStrategy != currentLap {
			suppressNotify = false
			a.pitRadioState.lastNotifiedLapFuelStrategy = currentLap
		}
	default:
		return
	}

	if suppressNotify {
		return
	}

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send fuel message")
		} else {
			a.log.Info().
				Str("message", message).
				Int16("lap", a.state.current.lapNumber).
				Float32("fuel_percent", a.gtClient.Telemetry.FuelLevelPercent()).
				Float64("lap_meters", circuitLengthMeters).
				Float64("range_meters", fuelRangeMeters).
				Float64("range_laps", fuelRangeLaps).
				Float64("range_laps_safe", fuelRangeLapsSafe).
				Float64("range_laps_to_box", fuelRangeLapsUntilBox).
				Float64("lap_progress", lapProgress).
				Msg("Send fuel message")
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
