package odometer

import (
	"math"

	"github.com/rs/zerolog"
	gtmodels "github.com/zetetos/gt-telemetry/pkg/models"
)

const (
	teleportDistanceMax float64 = 500 // Maximum distance in meters to consider between updates (filter out teleports)
)

type Meter struct {
	log            zerolog.Logger      // Logger instance
	distanceMeters float64             // Total distance in meters
	lastCoordinate gtmodels.Coordinate // Last known coordinate for distance tracking
}

func New(logger zerolog.Logger) *Meter {
	m := Meter{
		log:            logger.With().Str("package", "odometer").Logger(),
		distanceMeters: 0,
		lastCoordinate: gtmodels.Coordinate{},
	}

	return &m
}

// Reset clears the odometer distance and last known coordinate.
func (m *Meter) Reset() {
	m.distanceMeters = 0
	m.lastCoordinate = gtmodels.Coordinate{}

	m.log.Info().
		Msg("Odometer reset")
}

// Add updates the odometer with the current position and returns the distance travelled since the last update.
func (m *Meter) Add(currentPos gtmodels.Coordinate) float64 {
	dx := float64(currentPos.X - m.lastCoordinate.X)
	dy := float64(currentPos.Y - m.lastCoordinate.Y)
	dz := float64(currentPos.Z - m.lastCoordinate.Z)
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)

	// drop unreasonable distance increments (teleports/glitches)
	if distance > teleportDistanceMax {
		m.log.Debug().
			Float64("distance", distance).
			Int("Min", 0).
			Int("Max", int(teleportDistanceMax)).
			Msg("Distance travelled out of acceptable range")

		m.lastCoordinate = currentPos

		return m.distanceMeters
	}

	m.distanceMeters += distance
	m.lastCoordinate = currentPos

	return m.distanceMeters
}

// Read returns the current odometer reading in meters.
func (m *Meter) Read() float64 {
	if math.IsNaN(m.distanceMeters) {
		return 0
	}

	if math.IsInf(m.distanceMeters, 0) {
		return 999999.9
	}

	return m.distanceMeters
}
