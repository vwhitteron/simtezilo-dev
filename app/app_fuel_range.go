package app

import (
	"time"
)

func (a *App) newLapFuelRangeHandler() {
	a.log.Debug().
		Str("component", "new lap fuel range handler").
		Msg("Start")

	for {
		select {
		case <-a.lapStartEvents:
			currentPos := a.gtClient.Telemetry.PositionalMapCoordinates()
			a.circuit.UpdateCircuitByStartLine(currentPos)
		default:
			time.Sleep(16 * time.Millisecond)
		}
	}
}

// updateFuelConsumption calculates lap distance, vehicle fuel consumption and range
func (a *App) updateFuelConsumption() {
	if !a.sequenceHasAdvanced() {
		return
	}

	if !a.telemetryIsActive() {
		return
	}

	if a.timeOfDayHasReset() {
		a.fuelRange.Reset()
		a.circuit.ResetLapProgress()
	}

	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()
	fuelLevel := a.gtClient.Telemetry.FuelLevelPercent()

	a.fuelRange.Update(coordinates, fuelLevel)

	a.circuit.UpdateCircuitByCoordinates(coordinates)
}
