package app

import (
	"github.com/zetetos/gt-telemetry/pkg/models"
)

// updateCircuit checks for circuit changes and resets odometer and fuel range if needed.
func (a *App) updateCircuit() {
	lap := a.state.current.lapNumber
	odometer := a.odometer.Read()
	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()

	if didUpdate := a.circuit.UpdateCircuit(odometer, lap, coordinates, models.CoordinateTypeCircuit); didUpdate {
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.state.last.lastLapTime = 0
	}
}
