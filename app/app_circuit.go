package app

import (
	"github.com/zetetos/gt-telemetry/pkg/models"
)

// updateCircuit checks for circuit changes and resets odometer and fuel range if needed.
func (a *App) updateCircuit() {
	lap := a.state.current.lapNumber
	lapTime := a.state.current.lastLapTime
	odometer := a.odometer.Read()
	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()

	if didUpdate := a.circuit.UpdateCircuit(odometer, lap, lapTime, coordinates, models.CoordinateTypeCircuit); didUpdate {
		a.state.last.lastLapTime = 0
	}
}
