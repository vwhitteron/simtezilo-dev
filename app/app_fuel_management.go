package app

import (
	"fmt"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
)

// updateFuelRange calculates lap distance, vehicle fuel consumption and range.
func (a *App) updateFuelRange() {
	if !a.sequenceHasAdvanced() || !a.telemetryIsActive() {
		return
	}

	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()
	odometerReading := a.odometer.Add(coordinates)

	fuelLevel := a.gtClient.Telemetry.FuelLevelPercent()
	a.fuelRange.Update(odometerReading, fuelLevel)
}

// notifyFuelWarnings sends fuel warning notifications over the pit radio.
func (a *App) notifyFuelWarnings() {
	if !a.config.GetFuelMonitoringEnabled() {
		return
	}

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
	fuelRangeMeters := a.fuelRange.DistanceMeters()
	safetyMarginMeters := a.config.GetFuelRangeSafetyMarginLaps() * circuitLengthMeters
	FuelRangeMetersSafe := fuelRangeMeters - safetyMarginMeters

	lapProgressRemaining := a.circuit.LapProgressRemaining()
	distanceToPitBox := lapProgressRemaining * circuitLengthMeters
	distanceToPitBoxNextLap := distanceToPitBox + circuitLengthMeters
	preWarnNotifyDistance := distanceToPitBox + ((a.config.GetFuelPreWarnNotifyLaps() + 1) * circuitLengthMeters)
	preWarnNotifyLap := float64(currentLap) + a.config.GetFuelPreWarnNotifyLaps()
	boxNotifyDistance := min(2000, circuitLengthMeters*0.2)

	fuelEmpty := a.gtClient.Telemetry.FuelLevelPercent() <= 0
	fuelCritical := fuelRangeMeters <= distanceToPitBox
	boxthislap := FuelRangeMetersSafe <= distanceToPitBoxNextLap && distanceToPitBox <= boxNotifyDistance
	refuelPreWarn := FuelRangeMetersSafe <= preWarnNotifyDistance
	fuelStrategyUpdate := a.fuelRange.IsReady() &&
		remainingLaps > fuelRangeLaps &&
		currentLap%int16(a.config.GetFuelStrategyNotifyLaps()) == 0

	var (
		message        string
		suppressNotify bool
	)

	switch {
	case fuelEmpty:
		message, suppressNotify = a.fuelEmptyMessage(remainingLaps, currentLap)
	case fuelCritical:
		message, suppressNotify = a.fuelCriticalMessage(remainingLaps, currentLap)
	case boxthislap:
		message, suppressNotify = a.fuelBoxThisLapMessage(currentLap, remainingLaps)
	case refuelPreWarn:
		message, suppressNotify = a.fuelBoxPreWarnMessage(preWarnNotifyLap, currentLap, remainingLaps)
	case fuelStrategyUpdate:
		message, suppressNotify = a.fuelStrategyMessage(fuelRangeLaps, currentLap, remainingLaps)
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
			Float32("fuel_rate", float32(a.fuelRange.UsageRatePerKm())).
			Float64("lap_meters", circuitLengthMeters).
			Float64("range_meters", fuelRangeMeters).
			Float64("range_meters_safe", FuelRangeMetersSafe).
			Float64("distance_to_pit", distanceToPitBox).
			Int("lap_progress_remaining", int(lapProgressRemaining*100)).
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

	message = a.i18n.GetString(languagedb.RadioBoxThisLap)

	suppressNotify = remainingLaps == 0
	a.pitRadioState.lastNotifiedLapFuelWarning = currentLap

	return message, suppressNotify
}

// fuelBoxPreWarnMessage generates a fuel pre-warn message based on estimated fuel range.
func (a *App) fuelBoxPreWarnMessage(
	fuelRangeLapsSafe float64,
	currentLap int16,
	remainingLaps float64,
) (message string, suppressNotify bool) {
	if a.pitRadioState.fuelNotifyPrewarnIssued {
		message = ""
		suppressNotify = true

		return message, suppressNotify
	}

	format := a.i18n.GetString(languagedb.RadioFuelPreWarnFmt)
	message = fmt.Sprintf(format, int(fuelRangeLapsSafe))

	suppressNotify = remainingLaps == 0
	a.pitRadioState.fuelNotifyPrewarnIssued = true
	a.pitRadioState.lastNotifiedLapFuelWarning = currentLap

	return message, suppressNotify
}

// fuelStrategyMessage generates a fuel strategy message based on estimated fuel range and remaining laps.
func (a *App) fuelStrategyMessage(
	fuelRangeLaps float64,
	currentLap int16,
	remainingLaps float64,
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
