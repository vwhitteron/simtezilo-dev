package fuelrange

import (
	"github.com/rs/zerolog"
)

const (
	// Initial fuel level in percent
	initialFuelLevel float64 = -1.0

	// Initial odometer reading in meters
	initialOdometerReading float64 = -1.0

	// Default (very high) range in laps when unknown
	rangeLapsUnknown float64 = 10000

	// Default (unknown) range in meters
	rangeDistanceUnknown float64 = rangeLapsUnknown * 1000

	// Percentile fuel consumption rate to use for range estimation
	fuelRatePercentile int = 50

	// Minimum number of samples required to provide a reliable range estimate
	fuelRangeMinSamples int = 1000

	// Number of samples to store in the buffer
	fuelRangeMaxSamples int = 6000
)

type FuelRange struct {
	log                     zerolog.Logger
	lastOdometerReading     float64   // Last processed odometer reading in meters
	fuelLevelAtLastUpdate   float64   // Last processed fuel level in percent
	distanceSinceLastUpdate float64   // Distance in meters travelled since last fuel range update
	fuelRateSamples         []float64 // Buffer of fuel consumption samples in percent per km
	fuelRate                float64   // Moving average fuel consumption rate in percent per km
	distanceMeters          float64   // Estimated distance in meters that can be travelled with current fuel level
	refueling               bool      // Flag to indicate if refuelling is in progress
	isLive                  bool      // Flag to indicate if a session is live or a replay
	maxSamples              int       // Maximum number of samples to store in the buffer
	minSamples              int       // Minimum number of samples required to provide a reliable range estimate
}

// New creates a new fuel range estimator.
func New(logger zerolog.Logger) *FuelRange {
	fuelRange := FuelRange{
		log: logger.With().Str("package", "fuel").Logger(),
	}

	fuelRange.Reset()

	return &fuelRange
}

// Reset clears the internal state of the fuel range estimator.
func (r *FuelRange) Reset() {
	r.fuelLevelAtLastUpdate = initialFuelLevel
	r.distanceSinceLastUpdate = 0
	r.fuelRate = 0
	r.distanceMeters = 0
	r.refueling = false

	// Replays reduce fuel samples by ~100x compared to a live session
	minSamples := fuelRangeMinSamples
	maxSamples := fuelRangeMaxSamples

	if !r.isLive {
		minSamples = minSamples / 100
		maxSamples = maxSamples / 100
	}

	r.minSamples = minSamples
	r.maxSamples = maxSamples
	r.fuelRateSamples = make([]float64, 0, r.maxSamples)

	r.log.Info().
		Msg("Fuel range reset")
}

// ResetEstimate resets only the fuel range estimate but retains odometer and samples.
func (r *FuelRange) ResetEstimate() {
	r.distanceMeters = rangeDistanceUnknown
	r.distanceSinceLastUpdate = 0
	r.fuelRate = 0

	r.log.Info().
		Msg("Fuel range estimate reset")
}

// SetLive sets the replaying flag to indicate if the current session is a replay.
func (r *FuelRange) SetLive(isLive bool) {
	if r.isLive == isLive {
		return
	}

	r.isLive = isLive

	r.log.Info().
		Bool("is_live", isLive).
		Msg("Set fuel range sample granularity")

	r.Reset()
}

// Update updates fuel consumption based on the current coordinate and fuel level.
func (r *FuelRange) Update(odometerReading float64, fuelLevel float32) {
	// Initialise Range after init/reset
	if r.fuelLevelAtLastUpdate == initialFuelLevel || r.lastOdometerReading == initialOdometerReading {
		r.fuelLevelAtLastUpdate = float64(fuelLevel)
		r.lastOdometerReading = odometerReading

		return
	}

	// Reset fuel range when odometer is rolled back or reset
	if odometerReading < r.lastOdometerReading {
		r.log.Info().
			Float64("last_odometer", r.lastOdometerReading).
			Float64("current_odometer", odometerReading).
			Msg("Odometer reset detected")

		r.Reset()

		return
	}

	r.distanceSinceLastUpdate += odometerReading - r.lastOdometerReading
	r.lastOdometerReading = odometerReading

	consumed := r.fuelConsumed(float64(fuelLevel))

	samples := len(r.fuelRateSamples)

	if consumed > 0 && r.distanceSinceLastUpdate > 0 {
		if samples >= fuelRangeMaxSamples {
			r.fuelRateSamples = r.fuelRateSamples[1:]
		}

		fuelPerMeter := consumed / r.distanceSinceLastUpdate

		r.fuelRateSamples = append(r.fuelRateSamples, fuelPerMeter)

		// r.fuelRate = r.fuelRatePercentile(fuelRatePercentile)
		r.fuelRate = r.fuelRateMA()

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
		// Detect refuelling (-1 gives small margin to avoid occasional noise)
		if !r.refueling {
			r.ResetEstimate()
			r.refueling = true

			r.log.Debug().
				Float32("fuel_level", fuelLevel).
				Float64("last_fuel_level", r.fuelLevelAtLastUpdate).
				Msg("Refuel detected")
		}
	}
}

// DistanceMeters returns the estimated distance in meters that can be travelled with current fuel level.
func (r *FuelRange) DistanceMeters() float64 {
	// Fuel rate not available
	if r.fuelRate <= 0 {
		return rangeDistanceUnknown
	}

	// Not enough samples to provide a reliable estimate
	if len(r.fuelRateSamples) < r.minSamples {
		return rangeDistanceUnknown
	}

	return r.distanceMeters - r.distanceSinceLastUpdate
}

// RangeLaps returns the estimated number of laps that can be completed on a circuit of given length.
func (r *FuelRange) DistanceLaps(lengthMeters float64) float64 {
	// Invalid circuit length
	if lengthMeters <= 0 {
		return rangeLapsUnknown
	}

	distanceMeters := r.DistanceMeters()

	return distanceMeters / lengthMeters
}

// UsageRatePerKm returns the current fuel consumption rate in percent per km.
func (r *FuelRange) UsageRatePerKm() float64 {
	return r.fuelRate * 1000
}

// fuelConsumed returns the fuel consumed since the last update.
func (r *FuelRange) fuelConsumed(currentFuelLevel float64) float64 {
	consumed := r.fuelLevelAtLastUpdate - currentFuelLevel

	return consumed
}

// fuelRateMA returns the moving average fuel consumption rate in percent per km.
func (r *FuelRange) fuelRateMA() float64 {
	var sum float64

	for _, sample := range r.fuelRateSamples {
		sum += sample
	}

	return sum / float64(len(r.fuelRateSamples))
}
