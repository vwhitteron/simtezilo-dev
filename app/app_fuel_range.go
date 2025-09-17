package app

import (
	"fmt"
	"math"
	"time"

	telemetry_client "github.com/zetetos/gt-telemetry"
)

const (
	// Fuel range
	samplingInitialState        int16   = -2           // Lap number to signify an initialised sampling state
	fuelRangeSamples            int     = 600          // Number of samples for rolling average
	fuelRangeLapsInitialValue   float64 = 10000        // Initial high lap value to indicate uninitialized state
	fuelRangeMetersInitialValue float64 = 1000 * 10000 // Initial high distance value to indicate uninitialized state

	// Distance
	shortestSampleDistance float64 = 300       // Minimum distance in meters to be considered for range calculations
	shortestTrackDistance  float64 = 900 * 0.9 // Shortest known track length in meters (Northern Isle Speedway)
)

type fuelRangeEstimation struct {
	samples    []float64 // Rolling buffer for moving average fuel range calculations
	lastUpdate time.Time // Last time the fuel range buffer was updated

	initialFuelLevelPercent        float32 // Fuel percentage from the first sample
	lastFuelLevelPercent           float32 // Fuel percentage from the last sample
	sampledFuelLevelPercent        float64 // Fuel percentage when distance was sampled
	sampledDistanceTravelledMeters float64 // Total distance when fuel was last sampled

	usageRatePerKm   float64 // Fuel usage rate in percent per kilometer
	usageRatePerKmMA float64 // Exponential moving average of fuel usage rate in percent per kilometer

	distanceMeters   float64 // Current fuel range in meters
	distanceMetersMA float64 // Exponential moving average of current fuel range in meters
	distanceLaps     float64 // Fuel range in laps
	distanceLapsMA   float64 // Exponential moving average of current fuel range in laps
}

type lapDistanceEstimation struct {
	lapBeingSampled         int16                   // Lap number currently being sampled for determining lap distance
	lapStartMarker          float64                 // Total distance travelled at the start of the current lap
	lapDistanceMeters       float64                 // Track lap distance in meters
	distanceTravelledMeters float64                 // Total distance travelled in the session in meters
	circuitPosition         telemetry_client.Vector // Last known circuit position for distance calculations
	startLinePosition       struct{ X, Y, Z int16 } // Position of the start/finish line
	lapProgress             float64                 // Current lap progress
}

func (a *App) resetFuelRange() {
	a.fuelRange = fuelRangeEstimation{
		initialFuelLevelPercent:        a.gtClient.Telemetry.FuelLevelPercent(),
		lastFuelLevelPercent:           a.gtClient.Telemetry.FuelLevelPercent(),
		samples:                        make([]float64, 0, fuelRangeSamples),
		sampledFuelLevelPercent:        -1,
		sampledDistanceTravelledMeters: -1,
		lastUpdate:                     time.Now(),
		distanceMeters:                 fuelRangeMetersInitialValue,
		distanceLaps:                   fuelRangeLapsInitialValue,
		distanceMetersMA:               fuelRangeMetersInitialValue,
		distanceLapsMA:                 fuelRangeLapsInitialValue,
	}
}

func (a *App) resetLapDistance() {
	a.circuit = lapDistanceEstimation{
		lapBeingSampled:         -2,
		lapStartMarker:          -1,
		lapDistanceMeters:       0,
		distanceTravelledMeters: 0,
		circuitPosition:         telemetry_client.Vector{},
		lapProgress:             0,
	}
}

func (a *App) newLapFuelRangeHandler() {
	a.log.Debug().
		Str("component", "new lap fuel range handler").
		Msg("Start")

	for {
		select {
		case <-a.lapStartEvents:
			a.updateStartLinePosition()
			a.udpateLapDistance()
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

	a.checkForRefuel()

	a.updateDistanceTravelled()

	a.updateLapProgess()

	a.updateFuelRange()

	// TODO: remove debug logging
	if a.state.current.sequenceNumber%600 == 0 {
		fmt.Printf("DISTANCE: lap=%.0fm, race=%.2fkm, travelled=%.0fm, marker=%.0fm, sampling=%d\n",
			a.circuit.lapDistanceMeters,
			a.circuit.lapDistanceMeters*float64(a.gtClient.Telemetry.RaceLaps())/1000,
			a.circuit.distanceTravelledMeters,
			a.circuit.lapStartMarker,
			a.circuit.lapBeingSampled,
		)

		fmt.Printf("FUEL %%: current=%.2f%%, last=%.2f%%, initial=%.2f%%\n",
			a.gtClient.Telemetry.FuelLevelPercent(),
			a.fuelRange.lastFuelLevelPercent,
			a.fuelRange.initialFuelLevelPercent,
		)

		fuelUsagePerLap := float32(0)
		if a.circuit.lapDistanceMeters > 0 {
			fuelUsagePerLap = float32(a.fuelRange.usageRatePerKm * a.circuit.lapDistanceMeters / 1000)
		}

		fmt.Printf("FUEL l: rate=%.2f%%/lap, range=%.2f laps, range safe=%.2f laps, range ma=%.2f laps, range safe ma=%.2f laps, range box=%.2f laps\n",
			fuelUsagePerLap,
			a.fuelRange.distanceLaps,
			max(a.fuelRange.distanceLaps-a.config.GetFuelRangeSafetyMarginLaps(), 0),
			a.fuelRange.distanceLapsMA,
			a.getFuelRangeLapsSafe(),
			a.getFuelRangeLapsUntilBox(),
		)

		fmt.Printf("FUEL m: rate=%.2f%%/km, rate ma=%.2f%%/km, range=%.2fkm, range safe=%.2fkm, range ma=%.2fkm, range safe ma=%.2fkm\n",
			a.fuelRange.usageRatePerKm,
			a.fuelRange.usageRatePerKmMA,
			a.fuelRange.distanceMeters/1000,
			max((a.fuelRange.distanceMeters-a.config.GetFuelRangeSafetyMarginMeters())/1000, 0),
			a.fuelRange.distanceMetersMA/1000,
			a.getFuelRangeMetersSafe()/1000,
		)
	}
}

// checkForRefuel resets fuel range calculations when the vehicle has been refueled
func (a *App) checkForRefuel() {
	if a.gtClient.Telemetry.FuelLevelPercent() <= a.fuelRange.lastFuelLevelPercent {
		return
	}

	// Ignore updates while vehicle in the pits being refueled
	if a.gtClient.Telemetry.GroundSpeedMetersPerSecond() == 0 {
		return
	}

	a.log.Info().
		Str("component", "fuel").
		Int16("lap", a.state.current.lapNumber).
		Float32("previous_fuel_level_percent", a.fuelRange.lastFuelLevelPercent).
		Float32("current_fuel_level_percent", a.gtClient.Telemetry.FuelLevelPercent()).
		Msg("Refuel detected")

	a.resetFuelRange()
}

// updateDistanceTravelled tracks the total and lap distances covered in the current session
func (a *App) updateDistanceTravelled() {
	currentPos := a.gtClient.Telemetry.PositionalMapCoordinates()

	if a.circuit.circuitPosition.X != 0 {
		// Calculate distance between current and last position
		dx := float64(currentPos.X - a.circuit.circuitPosition.X)
		dy := float64(currentPos.Y - a.circuit.circuitPosition.Y)
		dz := float64(currentPos.Z - a.circuit.circuitPosition.Z)
		distance := math.Sqrt(dx*dx + dy*dy + dz*dz)

		// Only add reasonable distance increments (filter out teleports/glitches)
		if distance > 0 && distance < 500 { // Max ~500m between updates
			a.circuit.distanceTravelledMeters += distance
		}
	}

	a.circuit.circuitPosition = currentPos
}

func (a *App) updateStartLinePosition() {
	currentPos := a.gtClient.Telemetry.PositionalMapCoordinates()

	// 10m precision for X and Y, 3m for Z
	normalisedPos := struct{ X, Y, Z int16 }{
		X: int16(currentPos.X / 20),
		Y: int16(currentPos.Y / 20),
		Z: int16(currentPos.Z / 5),
	}

	xDelta := normalisedPos.X - a.circuit.startLinePosition.X
	yDelta := normalisedPos.Y - a.circuit.startLinePosition.Y
	zDelta := normalisedPos.Z - a.circuit.startLinePosition.Z

	if xDelta+yDelta+zDelta == 0 {
		return
	}

	initialUpdate := false
	if a.circuit.startLinePosition.X == 0 && a.circuit.startLinePosition.Y == 0 && a.circuit.startLinePosition.Z == 0 {
		initialUpdate = true
	}

	if xDelta != 0 || yDelta != 0 || zDelta != 0 {
		a.log.Warn().
			Str("component", "fuel").
			Bool("first_update", initialUpdate).
			Int16("x_delta", xDelta).
			Int16("y_delta", yDelta).
			Int16("z_delta", zDelta).
			Str("position", fmt.Sprintf("(x: %d, y: %d, z: %d)", normalisedPos.X, normalisedPos.Y, normalisedPos.Z)).
			Msg("Start/finish line position updated")
	}

	a.resetLapDistance()

	a.circuit.startLinePosition = normalisedPos

}

func (a *App) updateLapProgess() {
	if a.circuit.lapDistanceMeters <= 0 || a.circuit.lapStartMarker <= 0 {
		a.circuit.lapProgress = 0

		return
	}

	a.circuit.lapProgress = math.Min((a.circuit.distanceTravelledMeters-a.circuit.lapStartMarker)/a.circuit.lapDistanceMeters, 1)
}

// updateLapDistance updates the lap distance when a fully sampled lap has been completed
func (a *App) udpateLapDistance() {
	if a.pitRadioState == nil {
		return
	}

	currentLap := a.gtClient.Telemetry.CurrentLap()
	previousLap := currentLap - 1

	lapDistance := a.circuit.distanceTravelledMeters - a.circuit.lapStartMarker

	a.fuelRange.lastFuelLevelPercent = a.gtClient.Telemetry.FuelLevelPercent()

	invalidSampleLap := false

	isInitState := a.circuit.lapBeingSampled == samplingInitialState

	if isInitState {
		invalidSampleLap = true
	}

	if !isInitState && a.circuit.lapBeingSampled != previousLap {
		invalidSampleLap = true

		a.log.Debug().
			Str("component", "fuel").
			Int16("last_lap", previousLap).
			Int16("sampled_lap", a.circuit.lapBeingSampled).
			Msg("Sampled lap out of sync")
	}

	if !isInitState && a.circuit.lapStartMarker < 0 {
		invalidSampleLap = true

		a.log.Debug().
			Str("component", "fuel").
			Int16("lap", previousLap).
			Float64("total_distance", a.circuit.distanceTravelledMeters).
			Float64("lap_start_marker", a.circuit.lapStartMarker).
			Msg("Lap starting marker not set")
	}

	a.circuit.lapBeingSampled = currentLap

	if !isInitState && lapDistance < shortestTrackDistance {
		invalidSampleLap = true

		a.log.Debug().
			Str("component", "fuel").
			Int16("lap", previousLap).
			Float64("lap_distance", lapDistance).
			Msg("Lap distance too short")
	}

	a.circuit.lapStartMarker = a.circuit.distanceTravelledMeters

	if invalidSampleLap {
		return
	}

	// Update lap distance TODO: track this in a slice to calculate percentile lap distance
	a.circuit.lapDistanceMeters = lapDistance

	a.log.Info().
		Str("component", "fuel").
		Int16("lap", previousLap).
		Float64("lap_distance", lapDistance).
		Msg("Lap distance updated")
}

// updateFuelRange estimates the fuel range in both laps and distance and also applies smoothing to both
func (a *App) updateFuelRange() {
	a.updateFuelUsageRate()

	now := time.Now()
	// if now.Sub(a.fuelRange.lastUpdate) < fuelRangeUpdateInterval {
	// 	return
	// }

	a.updateFuelRangeDistance()
	a.updateFuelRangeLaps()

	a.fuelRange.lastUpdate = now
}

func (a *App) updateFuelUsageRate() {
	currentFuelPercent := float64(a.gtClient.Telemetry.FuelLevelPercent())
	sampledFuelPercent := a.fuelRange.sampledFuelLevelPercent

	// Prime initial sample
	if a.fuelRange.sampledFuelLevelPercent == -1 || a.fuelRange.sampledDistanceTravelledMeters == -1 {
		a.fuelRange.sampledDistanceTravelledMeters = a.circuit.distanceTravelledMeters
		a.fuelRange.sampledFuelLevelPercent = currentFuelPercent

		return
	}

	fuelConsumed := float64(sampledFuelPercent) - currentFuelPercent
	distanceTravelledMeters := a.circuit.distanceTravelledMeters - a.fuelRange.sampledDistanceTravelledMeters

	if fuelConsumed <= 0 || distanceTravelledMeters <= 0 {
		return
	}

	fuelUsagePerMeter := fuelConsumed / distanceTravelledMeters

	a.fuelRange.usageRatePerKm = fuelUsagePerMeter * 1000

	a.fuelRange.distanceMeters = float64(currentFuelPercent) / fuelUsagePerMeter

	if len(a.fuelRange.samples) >= fuelRangeSamples {
		a.fuelRange.samples = a.fuelRange.samples[1:]
	}

	a.fuelRange.samples = append(a.fuelRange.samples, fuelUsagePerMeter)

	a.fuelRange.sampledDistanceTravelledMeters = a.circuit.distanceTravelledMeters
	a.fuelRange.sampledFuelLevelPercent = currentFuelPercent

	a.updateFuelUsageRateMA()
}

// updateFuelUsageRateMA updates the fuel usage rate with moving average smoothing applied
func (a *App) updateFuelUsageRateMA() {
	var sum, count float64
	for _, sample := range a.fuelRange.samples {
		sum += sample

		if sample > 0 {
			count++
		}
	}

	if count == 0 {
		a.log.Debug().
			Str("component", "fuel").
			Msg("No valid fuel usage samples")

		return
	}

	a.fuelRange.usageRatePerKmMA = (sum / count) * 1000

}

// updateFuelRangeDistance calculates fuel range in distance even without complete lap data
func (a *App) updateFuelRangeDistance() {
	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	if a.circuit.distanceTravelledMeters < shortestSampleDistance {
		return
	}

	a.fuelRange.distanceMeters = (float64(currentFuelPercent) / a.fuelRange.usageRatePerKm) * 1000
	a.fuelRange.distanceMetersMA = (float64(currentFuelPercent) / a.fuelRange.usageRatePerKmMA) * 1000
}

// updateFuelRangeLaps calculates how many complete laps can be completed with current fuel
func (a *App) updateFuelRangeLaps() {
	if a.circuit.lapDistanceMeters <= 0 {
		a.fuelRange.distanceLaps = fuelRangeLapsInitialValue

		return
	}

	a.fuelRange.distanceLaps = a.fuelRange.distanceMeters / a.circuit.lapDistanceMeters
	a.fuelRange.distanceLapsMA = a.fuelRange.distanceMetersMA / a.circuit.lapDistanceMeters
}

// getFuelRangeDistanceSafe returns the fuel range in meters with safety margin applied
func (a *App) getFuelRangeMetersSafe() (rangeMeters float64) {
	rangeMeters = max(a.fuelRange.distanceMetersMA-a.config.GetFuelRangeSafetyMarginMeters(), 0)

	return min(rangeMeters, fuelRangeMetersInitialValue)
}

// getFuelRangeLapsSafe returns the fuel range in laps with safety margin applied
// The range returned is capped between 0 and fuelRangeLapsInitialValue
func (a *App) getFuelRangeLapsSafe() (rangeLaps float64) {
	rangeLaps = max(a.fuelRange.distanceLapsMA-a.config.GetFuelRangeSafetyMarginLaps(), 0)

	return min(rangeLaps, fuelRangeLapsInitialValue)
}

// getFuelRangeLapsUntilBox returns the effective fuel range in laps until a pitstop is required.
// This range takes into account the current lap progress
// The range returned is capped between 0 and fuelRangeLapsInitialValue
func (a *App) getFuelRangeLapsUntilBox() (effectiveRange float64) {
	lapProgressRemaining := 1.0 - a.circuit.lapProgress

	// Effective range considering we need to complete this lap
	effectiveRange = a.getFuelRangeLapsSafe() - lapProgressRemaining

	return max(effectiveRange, 0)
}
