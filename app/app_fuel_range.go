package app

// updateFuelRange calculates lap distance, vehicle fuel consumption and range
func (a *App) updateFuelRange() {
	if !a.sequenceHasAdvanced() || !a.telemetryIsActive() {
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

	a.circuit.UpdateDistanceTravelled(odometerReading, lap, circuit.CoordinateTypeGeneral)
}

func (a *App) updateCircuit() {
	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()

	if didUpdate := a.circuit.UpdateCircuit(coordinates, circuit.CoordinateTypeGeneral); didUpdate {
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.state.last.lastLapTime = 0
	}
}
