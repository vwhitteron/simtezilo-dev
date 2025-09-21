package app

import (
	"github.com/vwhitteron/simtezilo-dev/app/circuit"
)

// func (a *App) newLapFuelRangeHandler() {
// 	a.log.Debug().
// 		Str("component", "new lap fuel range handler").
// 		Msg("Start")

// 	for {
// 		select {
// 		case <-a.lapStartEvents:
// 			coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()
// 			odometerReading := a.odometer.Add(coordinates)
// 			lap := a.state.current.lapNumber
// 			a.circuit.UpdateDistanceTravelled(odometerReading, lap, circuit.StartLineCoordinate)

// 			currentPos := a.gtClient.Telemetry.PositionalMapCoordinates()
// 			if didUpdate := a.circuit.UpdateCircuit(currentPos, circuit.StartLineCoordinate); didUpdate {
// 				a.odometer.Reset()
// 				a.fuelRange.Reset()
// 				a.state.last.lastLapTime = 0
// 			}
// 		default:
// 			time.Sleep(16 * time.Millisecond)
// 		}
// 	}
// }

// updateFuelConsumption calculates lap distance, vehicle fuel consumption and range
func (a *App) updateFuelConsumption() {
	if !a.sequenceHasAdvanced() {
		return
	}

	if !a.telemetryIsActive() {
		return
	}

	if a.timeOfDayHasReset() {
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.circuit.ResetLapProgress()
	}

	lap := a.state.current.lapNumber
	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()
	odometerReading := a.odometer.Add(coordinates)

	fuelLevel := a.gtClient.Telemetry.FuelLevelPercent()
	a.fuelRange.Update(odometerReading, fuelLevel)

	a.circuit.UpdateDistanceTravelled(odometerReading, lap, circuit.GeneralCoordinate)

	if didUpdate := a.circuit.UpdateCircuit(coordinates, circuit.GeneralCoordinate); didUpdate {
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.state.last.lastLapTime = 0
	}
}
