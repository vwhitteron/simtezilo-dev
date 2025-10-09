package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
)

const (
	positionDebounceTime = 5 * time.Second // Suppress position change notifications for this duration
)

// pitRadioState tracks Discord/pit radio communication state.
type pitRadioState struct {
	// Nofification tracking to prevent duplicate or noisy messages
	fuelNotifyPrewarnIssued          bool          // Flag indicating whether the fuel pre-warn notification has been sent
	fuelNotifyEmptyIssued            bool          // Flag indicating whether the fuel empty notification has been sent
	lastNotifiedLapFuelCritical      int16         // Last lap number when a critical fuel warning was sent
	lastNotifiedLapFuelWarning       int16         // Last lap number when a fuel warning was sent
	lastNotifiedLapFuelStrategy      int16         // Last lap number when a fuel strategy was sent
	lastNotifiedLapNumber            int16         // Last notified lap number
	lastNotifiedLapTime              time.Duration // Last notified lap time
	lastNotifiedRaceProgressInterval int8          // Last notified race progress percentage
	lastNotifiedGridPosition         int16         // Last notified grid position of the vehicle
	debounceGirdPositionNotify       time.Time     // Suppress grid position change notifications until this time

	// Grid position tracking
	currentGridPosition int16 // Current race position of the vehicle

	// Circuit tracking
	circuitName string // Current circuit name
}

// resetPitRadioState resets the pit radio state to initial values.
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

// sendPitRadioMessage sends pit radio messages based on fuel range, lap progress and position changes.
func (a *App) sendPitRadioMessage() {
	if a.pitRadio == nil {
		return
	}

	if a.pitRadioState == nil {
		a.resetPitRadioState()

		return
	}

	if !a.sequenceHasAdvanced() || !a.telemetryIsActive() {
		return
	}

	if a.state.current.lapNumber <= 0 {
		return
	}

	a.notifyCircuitChange()

	a.notifyFuelWarnings()

	a.notifyRaceProgress()

	a.notifyGridPositionChange()
}

// notifyCircuitChange sends a circuit change notification over the pit radio.
func (a *App) notifyCircuitChange() {
	if a.pitRadioState == nil {
		return
	}

	circuitName := a.circuit.Name()
	if circuitName == "" || circuitName == "unknown" {
		return
	}

	if circuitName == a.pitRadioState.circuitName {
		return
	}

	a.pitRadioState.circuitName = circuitName

	message := "Circuit updated to " + circuitName
	if a.pitRadio != nil {
		err := a.pitRadio.Send(pitradio.Message{
			MessageType: pitradio.TextMessage,
			Text:        message,
			Lang:        a.i18n.LanguageCode(),
			Accent:      a.config.GetAppAccent(),
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send circuit change message")

			return
		}

		a.log.Debug().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Msg("Send circuit change message")
	}
}

// positionHasChanged checks if the grid position has changed since the last update.
func (a *App) positionHasChanged() bool {
	if a.pitRadioState == nil {
		return false
	}

	if a.state.current.lapNumber <= 0 {
		return false
	}

	if a.gtClient.Telemetry.RaceEntrants() <= 1 {
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

// notifyGridPositionChange sends position change notifications over the pit radio.
func (a *App) notifyGridPositionChange() {
	if !a.positionHasChanged() {
		return
	}

	message := fmt.Sprintf("P%d", a.pitRadioState.currentGridPosition)

	if a.pitRadio != nil {
		err := a.pitRadio.Send(pitradio.Message{
			MessageType: pitradio.TextMessage,
			Text:        message,
			Lang:        a.i18n.LanguageCode(),
			Accent:      a.config.GetAppAccent(),
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send position change message")

			return
		}

		a.log.Debug().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Msg("Send position change message")
	}
}

// notifyLapTime sends lap time notifications over the pit radio.
func (a *App) notifyLapTime() {
	if a.pitRadioState == nil {
		return
	}

	if a.pitRadioState.lastNotifiedLapTime == a.state.current.lastLapTime {
		return
	}

	a.pitRadioState.lastNotifiedLapTime = a.state.current.lastLapTime

	if a.state.current.lastLapTime <= 0 {
		return
	}

	var message string

	bestLapTime := a.gtClient.Telemetry.BestLaptime()

	// TODO: add config option to notify all laps or best lap only
	if bestLapTime > 0 && a.state.current.lastLapTime <= bestLapTime && a.state.current.lapNumber > 2 {
		message = fmt.Sprintf("%s. %s",
			a.i18n.GetString(languagedb.RadioLapRecord),
			formatDuration(a.state.current.lastLapTime),
		)
	} else if a.state.current.lapNumber > 2 && a.state.current.lastLapTime > bestLapTime {
		// TODO: either make this a translation or drop it
		message = fmt.Sprintf("Down %s seconds",
			formatDuration(a.state.current.lastLapTime-bestLapTime))
	} else {
		message = formatDuration(a.state.current.lastLapTime)
	}

	// Send lap time message to Discord
	err := a.pitRadio.Send(pitradio.Message{
		MessageType: pitradio.TextMessage,
		Text:        message,
		Lang:        a.i18n.LanguageCode(),
		Accent:      a.config.GetAppAccent(),
		NoCache:     true,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "pit radio").
			Str("message", message).
			Msg("Send lap time message")

		return
	}

	a.log.Debug().
		Str("message", message).
		Int16("lap", a.state.current.lapNumber).
		Dur("lapTime", a.state.current.lastLapTime).
		Msg("Send lap time message")
}

// notifyLapNumber sends lap number notifications over the pit radio.
func (a *App) notifyLapNumber() {
	if a.pitRadioState == nil {
		return
	}

	currentLap := a.state.current.lapNumber
	raceLaps := a.gtClient.Telemetry.RaceLaps()

	if a.pitRadioState.lastNotifiedLapNumber >= currentLap {
		return
	}

	if currentLap <= 0 || currentLap > raceLaps {
		return
	}

	message := ""

	longRace := raceLaps > 8
	lapsRemaining := raceLaps - currentLap + 1

	raceCompleted := lapsRemaining <= 0 && a.pitRadioState.lastNotifiedLapNumber != currentLap
	finalLap := a.state.current.lapNumber == raceLaps && a.pitRadioState.lastNotifiedLapNumber != currentLap
	LastFewLaps := lapsRemaining <= 3 && longRace && a.pitRadioState.lastNotifiedLapNumber != currentLap

	switch {
	case raceCompleted:
		message = a.i18n.GetString(languagedb.RadioRaceFinish)

		a.pitRadioState.lastNotifiedLapNumber = currentLap
	case finalLap:
		message = a.i18n.GetString(languagedb.RadioFinalLap)

		a.pitRadioState.lastNotifiedLapNumber = currentLap
	case LastFewLaps:
		format := a.i18n.GetString(languagedb.RadioLapsRemainingFmt)
		message = fmt.Sprintf(format, lapsRemaining)

		a.pitRadioState.lastNotifiedLapNumber = currentLap
	}

	a.pitRadioState.lastNotifiedLapNumber = currentLap

	err := a.pitRadio.Send(pitradio.Message{
		MessageType: pitradio.TextMessage,
		Text:        message,
		Lang:        a.i18n.LanguageCode(),
		Accent:      a.config.GetAppAccent(),
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("message", message).
			Msg("Send lap number message")

		return
	}

	a.log.Debug().
		Str("message", message).
		Int16("lap", a.state.current.lapNumber).
		Msg("Send lap number message")
}

// notifyRaceProgress sends lap number notifications based on race progress over the pit radio.
func (a *App) notifyRaceProgress() {
	if a.pitRadioState == nil {
		return
	}

	progressInterval := 25 // race percent TODO: make a config option

	currentLap := a.state.current.lapNumber

	if currentLap == 0 {
		return
	}

	raceLaps := a.gtClient.Telemetry.RaceLaps()
	if raceLaps == 0 {
		// TODO: handle time based endurance races
		return
	}

	circuitLengthMeters := a.circuit.LengthMeters()
	if circuitLengthMeters <= 0 {
		return
	}

	// Calculate race progress based on distance in meters
	totalRaceDistanceMeters := float64(raceLaps) * circuitLengthMeters
	currentRaceDistanceMeters := float64(currentLap-1)*circuitLengthMeters + a.circuit.LapProgress()*circuitLengthMeters

	raceProgressPercent := int8(100 * currentRaceDistanceMeters / totalRaceDistanceMeters)

	// Calculate current progress interval based on progressInterval
	currentProgressInterval := (raceProgressPercent / int8(progressInterval)) * int8(progressInterval)

	// Skip notifications at 0%
	if raceProgressPercent <= 0 {
		return
	}

	NotifyInterval := currentProgressInterval > a.pitRadioState.lastNotifiedRaceProgressInterval &&
		raceProgressPercent < 100

	if !NotifyInterval {
		return
	}

	format := a.i18n.GetString(languagedb.RadioRaceProgressFmt)
	message := fmt.Sprintf(format, raceProgressPercent)

	a.pitRadioState.lastNotifiedRaceProgressInterval = currentProgressInterval

	if a.pitRadio != nil {
		err := a.pitRadio.Send(pitradio.Message{
			MessageType: pitradio.TextMessage,
			Text:        message,
			Lang:        a.i18n.LanguageCode(),
			Accent:      a.config.GetAppAccent(),
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send lap number message")

			return
		}

		a.log.Debug().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Msg("Send lap number message")
	}
}

// notifyFuelWarnings sends fuel warning notifications over the pit radio.
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

	if remainingLaps < 0 {
		return
	}

	fuelRangeLaps := a.fuelRange.DistanceLaps(circuitLengthMeters)
	fuelRangeLapsSafe := fuelRangeLaps - a.config.GetFuelRangeSafetyMarginLaps()
	fuelRangeMeters := a.fuelRange.DistanceMeters()
	lapProgress := a.circuit.LapProgress()
	lapProgressRemaining := a.circuit.LapProgressRemaining()
	fuelRangeLapsUntilBox := fuelRangeLapsSafe - lapProgressRemaining

	fuelEmpty := a.gtClient.Telemetry.FuelLevelPercent() <= 0
	fuelCritical := fuelRangeLapsUntilBox <= 0
	fuelEmptyNextLap := fuelRangeLapsUntilBox <= 1
	fuelEmptySoon := fuelRangeLapsUntilBox <= a.config.GetFuelPreWarnNotifyLaps()+a.config.GetFuelRangeSafetyMarginLaps()
	fuelStrategyUpdate := remainingLaps > fuelRangeLaps && currentLap%int16(a.config.GetFuelStrategyNotifyLaps()) == 0

	var (
		message        string
		suppressNotify bool
	)

	// Fuel warnings based on estimated range

	switch {
	case fuelEmpty:
		message, suppressNotify = a.fuelEmptyMessage(remainingLaps, currentLap)
	case fuelCritical:
		message, suppressNotify = a.fuelCriticalMessage(remainingLaps, currentLap)
	case fuelEmptyNextLap:
		message, suppressNotify = a.fuelBoxThisLapMessage(currentLap, remainingLaps)
	case fuelEmptySoon:
		message, suppressNotify = a.fuelBoxSoonMessage(fuelRangeLapsUntilBox, currentLap, remainingLaps)
	case fuelStrategyUpdate:
		message, suppressNotify = a.fuelStrategyMessage(fuelRangeLaps, remainingLaps, currentLap)
	default:
		return
	}

	if suppressNotify {
		return
	}

	if a.pitRadio != nil {
		err := a.pitRadio.Send(pitradio.Message{
			MessageType: pitradio.TextMessage,
			Text:        message,
			Lang:        a.i18n.LanguageCode(),
			Accent:      a.config.GetAppAccent(),
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send fuel message")

			return
		}

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

// fuelEmptyMessage generates a fuel empty message based on estimated fuel range.
func (a *App) fuelEmptyMessage(remainingLaps float64, currentLap int16) (message string, suppressNotify bool) {
	if a.pitRadioState.fuelNotifyEmptyIssued {
		message = ""
		suppressNotify = true

		return message, suppressNotify
	}

	if remainingLaps == 0 {
		message = a.i18n.GetString(languagedb.RadioOutOfFuelLastLap)
	} else {
		message = a.i18n.GetString(languagedb.RadioOutOfFuelBox)
	}

	suppressNotify = false
	a.pitRadioState.fuelNotifyEmptyIssued = true
	a.pitRadioState.lastNotifiedLapFuelWarning = currentLap

	return message, suppressNotify
}

// fuelCriticalMessage generates a critical fuel warning message based on estimated fuel range.
func (a *App) fuelCriticalMessage(remainingLaps float64, currentLap int16) (message string, suppressNotify bool) {
	if a.pitRadioState.lastNotifiedLapFuelCritical == currentLap {
		message = ""
		suppressNotify = true

		return message, suppressNotify
	}

	if remainingLaps == 0 {
		message = a.i18n.GetString(languagedb.RadioFuelCritical)
	} else {
		message = a.i18n.GetString(languagedb.RadioFuelCriticalBox)
	}

	suppressNotify = false
	a.pitRadioState.lastNotifiedLapFuelCritical = currentLap

	return message, suppressNotify
}

// fuelBoxThisLapMessage generates a message to box this lap based on estimated fuel range.
func (a *App) fuelBoxThisLapMessage(currentLap int16, remainingLaps float64) (message string, suppressNotify bool) {
	if a.pitRadioState.lastNotifiedLapFuelWarning == currentLap {
		message = ""
		suppressNotify = true

		return message, suppressNotify
	}

	message = a.i18n.GetString(languagedb.RadioBoxForFuel)

	suppressNotify = remainingLaps == 0
	a.pitRadioState.lastNotifiedLapFuelWarning = currentLap

	return message, suppressNotify
}

// fuelBoxSoonMessage generates a fuel pre-warn message based on estimated fuel range.
func (a *App) fuelBoxSoonMessage(
	fuelRangeLapsUntilBox float64,
	currentLap int16,
	remainingLaps float64,
) (message string, suppressNotify bool) {
	if a.pitRadioState.fuelNotifyPrewarnIssued {
		message = ""
		suppressNotify = true

		return message, suppressNotify
	}

	format := a.i18n.GetString(languagedb.RadioFuelPreWarnFmt)
	message = fmt.Sprintf(format, int(fuelRangeLapsUntilBox))

	suppressNotify = remainingLaps == 0
	a.pitRadioState.fuelNotifyPrewarnIssued = true
	a.pitRadioState.lastNotifiedLapFuelWarning = currentLap

	return message, suppressNotify
}

// fuelStrategyMessage generates a fuel strategy message based on estimated fuel range and remaining laps.
func (a *App) fuelStrategyMessage(
	fuelRangeLaps float64,
	remainingLaps float64,
	currentLap int16,
) (message string, suppressNotify bool) {
	if a.pitRadioState.lastNotifiedLapFuelStrategy == currentLap {
		message = ""
		suppressNotify = true

		return message, suppressNotify
	}

	format := a.i18n.GetString(languagedb.RadioFuelRangeFmt)
	message = fmt.Sprintf(format, int(fuelRangeLaps), int(remainingLaps))

	suppressNotify = remainingLaps == 0
	a.pitRadioState.lastNotifiedLapFuelStrategy = currentLap

	if a.pitRadioState.lastNotifiedLapFuelWarning == currentLap {
		suppressNotify = true
	}

	return message, suppressNotify
}

// formatDuration formats a time.Duration value for text and speech output.
func formatDuration(lapTime time.Duration) string {
	minutes := int(lapTime.Minutes())
	lapTime -= time.Duration(minutes) * time.Minute

	seconds := int(lapTime.Seconds())
	lapTime -= time.Duration(seconds) * time.Second

	milliseconds := int(lapTime.Milliseconds())

	minutesStr := strconv.Itoa(minutes)

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

	return pronounceTime(minutesStr, secondsStr, millisecondsStr, false)
}

// pronounceTime formats minutes, seconds and millisecond time components for text and speech output.
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
		char := string(r)

		if char == "0" {
			char = "oh"
		}

		announce = append(announce, char)
	}

	return strings.Join(announce, " ")
}
