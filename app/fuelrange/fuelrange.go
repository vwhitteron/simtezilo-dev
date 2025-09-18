package fuelrange

import (
	"math"
	"sort"

	"github.com/rs/zerolog"
	telemetry "github.com/zetetos/gt-telemetry"
)

const (
	initialFuelLevel     float64 = -1.0                    // Initial fuel level in percent
	sampleCount          int     = 60                      // Number of samples to store in the buffer
	rangeLapsUnknown     float64 = 10000                   // Default (very high) range in laps when unknown
	rangeDistanceUnknown float64 = rangeLapsUnknown * 1000 // Default (unknown) range in meters
	fuelRatePercentile   int     = 50                      // Percentile fuel consumption rate to use for range estimation
	fuelRangeMinSamples  int     = 10                      // Minimum number of samples required to provide a reliable range estimate
	teleportDistanceMax  float64 = 500                     // Maximum distance in meters to consider between updates (filter out teleports)
)

var (
	initialCoordinate = telemetry.Vector{X: -1, Y: -1, Z: -1}
)

type Range struct {
	log                     zerolog.Logger
	lastCoordinate          telemetry.Vector // Last processed track coordinate
	fuelLevelAtLastUpdate   float64          // Last processed fuel level in percent
	distanceSinceLastUpdate float64          // Distance in meters travelled since last fuel range update
	fuelRateSamples         []float64        // Buffer of fuel consumption samples in percent per km
	fuelRate                float64          // Moving average fuel consumption rate in percent per km
	distanceMeters          float64          // Estimated distance in meters that can be travelled with current fuel level
	refueling               bool             // Flag to indicate if refuelling is in progress
}

// New creates a new fuel range estimator
func New(logger zerolog.Logger) *Range {
	r := Range{
		log: logger.With().Str("package", "fuel").Logger(),
	}

	r.Reset()

	return &r
}

// Reset clears the internal state of the fuel range estimator
func (r *Range) Reset() {
	r.lastCoordinate = initialCoordinate
	r.fuelLevelAtLastUpdate = initialFuelLevel
	r.distanceSinceLastUpdate = float64(0)
	r.fuelRateSamples = make([]float64, 0, sampleCount)
	r.fuelRate = float64(0)
	r.refueling = false

	r.log.Debug().
		Msg("Fuel range reset")
}

// DistanceMeters returns the estimated distance in meters that can be travelled with current fuel level
func (r *Range) DistanceMeters() float64 {
	// Fuel rate not available
	if r.fuelRate <= 0 {
		return rangeDistanceUnknown
	}

	// Not enough samples to provide a reliable estimate
	if len(r.fuelRateSamples) < fuelRangeMinSamples {
		return rangeDistanceUnknown
	}

	return r.distanceMeters - r.distanceSinceLastUpdate
}

// RangeLaps returns the estimated number of laps that can be completed on a circuit of given length
func (r *Range) DistanceLaps(lengthMeters float64) float64 {
	// Invalid circuit length
	if lengthMeters <= 0 {
		return rangeLapsUnknown
	}

	distanceMeters := r.DistanceMeters()

	return distanceMeters / lengthMeters
}

// UsageRatePerKm returns the current fuel consumption rate in percent per km
func (r *Range) UsageRatePerKm() float64 {
	return r.fuelRate * 1000
}

// Update updates fuel consumption basedon the current coordinate and fuel level
func (r *Range) Update(coordinate telemetry.Vector, fuelLevel float32) {
	// Initialise Range after init/reset
	if r.fuelLevelAtLastUpdate == initialFuelLevel || r.lastCoordinate == initialCoordinate {
		r.fuelLevelAtLastUpdate = float64(fuelLevel)
		r.lastCoordinate = coordinate

		return
	}

	r.distanceSinceLastUpdate += r.distanceTravelled(coordinate)

	consumed := r.fuelConsumed(float64(fuelLevel))

	length := len(r.fuelRateSamples)

	if consumed > 0 && r.distanceSinceLastUpdate > 0 {
		if length >= sampleCount {
			r.fuelRateSamples = r.fuelRateSamples[1:]
		}

		fuelPerMeter := consumed / r.distanceSinceLastUpdate

		r.fuelRateSamples = append(r.fuelRateSamples, fuelPerMeter)

		r.fuelRate = r.fuelRatePercentile(fuelRatePercentile)

		r.distanceMeters = float64(fuelLevel) / r.fuelRate

		r.log.Debug().
			Float64("fuel_rate_ma", r.fuelRateMA()).
			Float64("fuel_rate_p80", r.fuelRate).
			Float64("consumed", consumed).
			Float64("distance_m", r.distanceSinceLastUpdate).
			Float64("range_m", r.distanceMeters).
			Int("samples", len(r.fuelRateSamples)).
			Msg("Update estimated fuel range")

		r.distanceSinceLastUpdate = 0
		r.fuelLevelAtLastUpdate = float64(fuelLevel)
		r.refueling = false
	} else if consumed < -1 {
		// Detect refuelling (small margin to avoid occasional noise)
		if !r.refueling {
			r.log.Debug().
				Float32("fuel_level", fuelLevel).
				Float64("last_fuel_level", r.fuelLevelAtLastUpdate).
				Msg("Refuel detected")
			r.refueling = true
		}

		r.Reset()

		r.refueling = true
	}

	r.lastCoordinate = coordinate
}

// distanceTravelled returns the distance travelled since the last update
func (r *Range) distanceTravelled(currentPos telemetry.Vector) float64 {
	dx := float64(currentPos.X - r.lastCoordinate.X)
	dy := float64(currentPos.Y - r.lastCoordinate.Y)
	dz := float64(currentPos.Z - r.lastCoordinate.Z)
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)

	// drop unreasonable distance increments (teleports/glitches)
	if distance < 0 || distance > teleportDistanceMax {
		r.log.Debug().
			Float64("distance", distance).
			Int("Min", 0).
			Int("Max", int(teleportDistanceMax)).
			Msg("Distance travelled out of acceptable range")

		return 0
	}

	return distance
}

// fuelConsumed returns the fuel consumed since the last update
func (r *Range) fuelConsumed(currentFuelLevel float64) float64 {
	consumed := r.fuelLevelAtLastUpdate - currentFuelLevel

	return consumed
}

// fuelRateMA returns the moving average fuel consumption rate in percent per km
func (r *Range) fuelRateMA() float64 {
	var sum float64

	for _, sample := range r.fuelRateSamples {
		sum += sample
	}

	return sum / float64(len(r.fuelRateSamples))
}

// fuelRatePercentile returns the specified percentile fuel range in percent per km
func (r *Range) fuelRatePercentile(percentile int) float64 {
	if len(r.fuelRateSamples) == 0 {
		return 0
	}

	percentileFraction := float64(percentile) / 100.0
	index := int(float64(len(r.fuelRateSamples)) * percentileFraction)

	// Calculate the 80th percentile fuel range
	sort.Float64s(r.fuelRateSamples)

	return r.fuelRateSamples[index]
}
