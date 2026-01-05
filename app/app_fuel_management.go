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
	if !a.shouldNotifyFuelWarning() {
		return
	}

	context := a.buildFuelWarningContext()
	if context == nil {
		return
	}

	message, suppressNotify := a.determineFuelWarningMessage(context)
	if suppressNotify || message == "" {
		return
	}

	a.sendFuelWarningMessage(message, context)
}

// shouldNotifyFuelWarning checks if conditions are met to send fuel warning notifications.
func (a *App) shouldNotifyFuelWarning() bool {
	if !a.pitRadioIsActive() {
		return false
	}

	if !a.config.GetPitRadioFuelMonitoringEnabled() {
		return false
	}

	if a.fuelRange == nil {
		return false
	}

	circuitLengthMeters := a.circuit.LengthMeters()
	if circuitLengthMeters <= 0 {
		return false
	}

	remainingLaps := float64(a.gtClient.Telemetry.RaceLaps()) - float64(a.gtClient.Telemetry.CurrentLap())

	return remainingLaps >= 0
}

// fuelWarningContext holds all the calculated values needed for fuel warnings.
type fuelWarningContext struct {
	currentLap              int16
	remainingLaps           float64
	circuitLengthMeters     float64
	fuelRangeLaps           float64
	fuelRangeMeters         float64
	fuelRangeMetersSafe     float64
	distanceToPitBox        float64
	distanceToPitBoxNextLap float64
	preWarnNotifyDistance   float64
	preWarnNotifyLap        float64
	boxNotifyDistance       float64
	lapProgressRemaining    float64
}

// buildFuelWarningContext creates a context with all calculated fuel warning values.
func (a *App) buildFuelWarningContext() *fuelWarningContext {
	circuitLengthMeters := a.circuit.LengthMeters()
	currentLap := a.gtClient.Telemetry.CurrentLap()
	remainingLaps := float64(a.gtClient.Telemetry.RaceLaps()) - float64(currentLap)

	fuelRangeLaps := a.fuelRange.DistanceLaps(circuitLengthMeters)
	fuelRangeMeters := a.fuelRange.DistanceMeters()
	safetyMarginMeters := a.config.GetPitRadioFuelRangeSafetyMarginLaps() * circuitLengthMeters
	fuelRangeMetersSafe := fuelRangeMeters - safetyMarginMeters

	lapProgressRemaining := a.circuit.LapProgressRemaining()
	distanceToPitBox := lapProgressRemaining * circuitLengthMeters
	distanceToPitBoxNextLap := distanceToPitBox + circuitLengthMeters
	preWarnNotifyDistance := distanceToPitBox + ((a.config.GetPitRadioFuelPreWarnNotifyLaps() + 1) * circuitLengthMeters)
	preWarnNotifyLap := float64(currentLap) + a.config.GetPitRadioFuelPreWarnNotifyLaps()
	boxNotifyDistance := min(2000, circuitLengthMeters*0.2)

	return &fuelWarningContext{
		currentLap:              currentLap,
		remainingLaps:           remainingLaps,
		circuitLengthMeters:     circuitLengthMeters,
		fuelRangeLaps:           fuelRangeLaps,
		fuelRangeMeters:         fuelRangeMeters,
		fuelRangeMetersSafe:     fuelRangeMetersSafe,
		distanceToPitBox:        distanceToPitBox,
		distanceToPitBoxNextLap: distanceToPitBoxNextLap,
		preWarnNotifyDistance:   preWarnNotifyDistance,
		preWarnNotifyLap:        preWarnNotifyLap,
		boxNotifyDistance:       boxNotifyDistance,
		lapProgressRemaining:    lapProgressRemaining,
	}
}

// determineFuelWarningMessage determines what fuel warning message to send based on current conditions.
func (a *App) determineFuelWarningMessage(context *fuelWarningContext) (string, bool) {
	fuelEmpty := a.gtClient.Telemetry.FuelLevelPercent() <= 0
	fuelCritical := context.fuelRangeMeters <= context.distanceToPitBox
	boxthislap := context.fuelRangeMetersSafe <= context.distanceToPitBoxNextLap && context.distanceToPitBox <= context.boxNotifyDistance
	refuelPreWarn := context.fuelRangeMetersSafe <= context.preWarnNotifyDistance
	fuelStrategyUpdate := a.fuelRange.IsReady() &&
		context.remainingLaps > context.fuelRangeLaps &&
		context.currentLap%int16(a.config.GetPitRadioFuelStrategyNotifyLaps()) == 0

	switch {
	case fuelEmpty:
		return a.fuelEmptyMessage(context.remainingLaps, context.currentLap)
	case fuelCritical:
		return a.fuelCriticalMessage(context.remainingLaps, context.currentLap)
	case boxthislap:
		return a.fuelBoxThisLapMessage(context.currentLap, context.remainingLaps)
	case refuelPreWarn:
		return a.fuelBoxPreWarnMessage(context.preWarnNotifyLap, context.currentLap, context.remainingLaps)
	case fuelStrategyUpdate:
		return a.fuelStrategyMessage(context.fuelRangeLaps, context.currentLap, context.remainingLaps)
	default:
		return "", true
	}
}

// sendFuelWarningMessage sends the fuel warning message via pit radio.
func (a *App) sendFuelWarningMessage(message string, context *fuelWarningContext) {
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

	a.log.Debug().
		Str("message", message).
		Int16("lap", a.state.current.lapNumber).
		Float32("fuel_percent", a.gtClient.Telemetry.FuelLevelPercent()).
		Float32("fuel_rate", float32(a.fuelRange.UsageRatePerKm())).
		Float64("lap_meters", context.circuitLengthMeters).
		Float64("range_meters", context.fuelRangeMeters).
		Float64("range_meters_safe", context.fuelRangeMetersSafe).
		Float64("distance_to_pit", context.distanceToPitBox).
		Int("lap_progress_remaining", int(context.lapProgressRemaining*100)).
		Msg("Send fuel message")
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
