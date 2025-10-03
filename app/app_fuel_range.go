package app

// updateFuelRange calculates lap distance, vehicle fuel consumption and range.
func (a *App) updateFuelRange() {
	if !a.sequenceHasAdvanced() || !a.telemetryIsActive() {
		return
	}

	// lap := a.state.current.lapNumber
	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()
	odometerReading := a.odometer.Add(coordinates)

	fuelLevel := a.gtClient.Telemetry.FuelLevelPercent()
	a.fuelRange.Update(odometerReading, fuelLevel)

	// a.circuit.UpdateDistanceTravelled(odometerReading, lap, models.CoordinateTypeCircuit)
}
