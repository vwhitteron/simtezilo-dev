package circuit

import (
	"fmt"
	"math"

	"github.com/rs/zerolog"
	gttelemetry "github.com/zetetos/gt-telemetry"
)

const (
	shortestCircuitLengthMeters int = 900 // Minimum length in meters (Northern Isle Speedway)
)

var circuitInfoInit = CircuitInfo{
	ID:        "unknown",
	Name:      "unknown",
	Length:    0,
	StartLine: gttelemetry.Vector{X: 0, Y: 0, Z: 0},
}

// CircuitInfo holds information about a racing circuit.
type Circuit struct { // TODO: avoid stuttering Circuit.Circuit
	log                     zerolog.Logger     // Logger instance
	info                    CircuitInfo        // Current circuit information
	database                *CircuitDB         // Circuit database
	lapStartDistanceMeters  float64            // Distance at which the current lap started
	distanceTravelledMeters float64            // Lap distance tracking for uknown circuits
	lastCoordinate          gttelemetry.Vector // Last known coordinate for distance tracking
}

// New creates a new Circuit instance with the provided logger and initializes the circuit database.
func New(logger zerolog.Logger) (*Circuit, error) {
	database, err := NewDB() // TODO: support loading db from file
	if err != nil {
		return nil, fmt.Errorf("create circuit database: %w", err)
	}

	return &Circuit{
		log:                     logger.With().Str("package", "circuit").Logger(),
		lapStartDistanceMeters:  0,
		distanceTravelledMeters: 0,
		info:                    circuitInfoInit,
		database:                database,
		lastCoordinate:          gttelemetry.Vector{},
	}, nil
}

// Reset clears the current circuit information and lap start marker distance.
func (c *Circuit) Reset() {
	c.info = CircuitInfo{
		ID:   circuitInfoInit.ID,
		Name: circuitInfoInit.Name,
	}

	c.ResetLapProgress()

	c.log.Info().
		Msg("Circuit reset")
}

// ResetLapProgress clears the lap progress tracking without resetting the circuit information.
func (c *Circuit) ResetLapProgress() {
	c.lapStartDistanceMeters = 0
	c.distanceTravelledMeters = 0
	c.lastCoordinate = circuitInfoInit.StartLine

	c.log.Info().
		Msg("Circuit reset")
}

// LengthMeters returns the length of the current circuit in meters.
func (c *Circuit) LengthMeters() float64 {
	return float64(c.info.Length)
}

// LapProgress returns the progress through the current lap as a value between 0 and 1
func (c *Circuit) LapProgress() float64 {
	lapDistance := c.distanceTravelledMeters - c.lapStartDistanceMeters

	if lapDistance < 0 || int(lapDistance) > c.info.Length {
		return 0
	}

	return lapDistance / float64(c.info.Length)
}

// LapProgressRemaining returns the remaining progress through the current lap as a value between 0 and 1
func (c *Circuit) LapProgressRemaining() float64 {
	progress := c.LapProgress()

	return 1.0 - progress
}

// UpdateCircuitByStartLine updates the current circuit information based on the provided start line coordinate.
func (c *Circuit) UpdateCircuitByStartLine(coordinate gttelemetry.Vector) {
	c.setLapStartMarker()

	if c.database == nil {
		return
	}

	// Only update the circuit by start line once after init/reset
	if coordinate == c.info.StartLine {
		return
	}

	circuitIDs, found := c.database.GetTracksAtStartLine(coordinate)
	if !found || len(circuitIDs) == 0 {
		c.log.Debug().
			Str("coordinate", fmt.Sprintf("(x: %.0f, y: %.0f, z: %.0f)", coordinate.X, coordinate.Y, coordinate.Z)).
			Str("source", "start line").
			Msg("No coordinate matched")

		return
	}

	if len(circuitIDs) > 1 {
		return
	}

	circuitID := circuitIDs[0]

	c.info, found = c.database.GetTrackByID(circuitID)
	if !found {
		c.log.Error().
			Str("track_id", circuitID).
			Str("source", "start line").
			Msg("Circuit not found in inventory")

		return
	}

	c.info.StartLine = coordinate

	c.log.Info().
		Str("track", c.info.Name).
		Str("source", "start line").
		Msg("Circuit updated")
}

// UpdateCircuitByCoordinates updates the current circuit information based on the provided positional coordinate
// This feature provides faster circuit identification after initial start line detection.
// TODO: need start line coordinates are returned in CircuitInfo to stop start line re-udpating c.info
func (c *Circuit) UpdateCircuitByCoordinates(coordinate gttelemetry.Vector) {
	c.updateDistanceTravelled(coordinate)

	if c.database == nil {
		return
	}

	// Only update the circuit by coordinate after init/reset
	if c.info != circuitInfoInit {
		return
	}

	circuitIDs, found := c.database.GetTracksAtCoordinate(coordinate)
	if !found || len(circuitIDs) == 0 {
		c.log.Debug().
			Str("coordinate", fmt.Sprintf("(x: %.0f, y: %.0f, z: %.0f)", coordinate.X, coordinate.Y, coordinate.Z)).
			Str("source", "circuit coordinate").
			Msg("No coordinate matched")

		return
	}

	if len(circuitIDs) > 1 {
		return
	}

	circuitID := circuitIDs[0]

	c.info, found = c.database.GetTrackByID(circuitID)
	if !found {
		c.log.Error().
			Str("track_id", circuitID).
			Str("source", "circuit coordinate").
			Msg("Circuit not found in inventory")

		c.info.ID = "unknown" // inhibit re-updating until init/reset

		return
	}

	c.log.Info().
		Str("track", c.info.Name).
		Str("source", "circuit coordinate").
		Msg("Circuit updated")
}

// setLapStartMarker sets the distance at which the current lap started.
func (c *Circuit) setLapStartMarker() {
	c.lapStartDistanceMeters = c.distanceTravelledMeters

	if c.circuitIsKnown() {
		return
	}

	c.setCircuitLength()

}

// setCircuitLength sets the circuit length if it can be determined from the distance travelled
func (c *Circuit) setCircuitLength() {
	if c.distanceTravelledMeters <= 0 {
		return
	}

	circuitLength := int(c.distanceTravelledMeters - c.lapStartDistanceMeters)

	if circuitLength > shortestCircuitLengthMeters {
		c.info.Length = circuitLength
	}
}

// updateDistanceTravelled updates the total distance travelled based on the provided coordinate.
// TODO: this is duplicated in fuelrange.distanceTravelled
func (c *Circuit) updateDistanceTravelled(coordinate gttelemetry.Vector) {
	if c.lastCoordinate.X == 0 {
		c.lastCoordinate = coordinate

		return
	}

	// Calculate distance between current and last position
	dx := float64(coordinate.X - c.lastCoordinate.X)
	dy := float64(coordinate.Y - c.lastCoordinate.Y)
	dz := float64(coordinate.Z - c.lastCoordinate.Z)
	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)

	// Only add reasonable distance increments (filter out teleports/glitches)
	if distance > 0 && distance < 500 { // Max ~500m between updates
		c.distanceTravelledMeters += distance
	}

	c.lastCoordinate = coordinate
}

// circuitIsKnown returns true if the current circuit is known
func (c *Circuit) circuitIsKnown() bool {
	return c.info.ID != circuitInfoInit.ID
}
