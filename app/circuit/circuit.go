package circuit

import (
	"fmt"

	"github.com/rs/zerolog"
	gttelemetry "github.com/zetetos/gt-telemetry"
)

type updateType bool

const (
	shortestCircuitLengthMeters int = 900 // Minimum length in meters (Northern Isle Speedway)

	StartLineCoordinate updateType = true
	GeneralCoordinate   updateType = false
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
	lap                     int16              // Current lap number being tracked
	lapStartOdometerReading float64            // Distance at which the current lap started
	lapProgressMeters       float64            // Lap distance tracking for uknown circuits
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
		lapStartOdometerReading: 0,
		lapProgressMeters:       0,
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
	c.lapStartOdometerReading = 0
	c.lapProgressMeters = 0
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
	lapDistance := c.lapProgressMeters - c.lapStartOdometerReading

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

// UpdateCircuit updates the current circuit information by matching the provided coordinate with a circuit DB entry
// The isStartLine flag indicates if the coordinate is from a start line crossing or general positional update
// TODO: need start line coordinates are returned in CircuitInfo to avoid start line re-udpating c.info
func (c *Circuit) UpdateCircuit(coordinate gttelemetry.Vector, updateType updateType) {
	c.setLapStartMarker()

	if c.database == nil {
		return
	}

	var matchingCircuitIDs []string
	if updateType == StartLineCoordinate {
		// Only update the circuit by start line once after init/reset
		if coordinate == c.info.StartLine {
			return
		}

		var found bool
		matchingCircuitIDs, found = c.database.GetTracksAtStartLine(coordinate)
		if !found || len(matchingCircuitIDs) == 0 {
			c.log.Debug().
				Str("coordinate", fmt.Sprintf("(x: %.0f, y: %.0f, z: %.0f)", coordinate.X, coordinate.Y, coordinate.Z)).
				Str("type", "start line").
				Msg("No coordinate matched")

			return
		}
	} else {
		// Only update the circuit by coordinate after init/reset
		if c.info != circuitInfoInit {
			return
		}

		var found bool
		matchingCircuitIDs, found = c.database.GetTracksAtCoordinate(coordinate)
		if !found || len(matchingCircuitIDs) == 0 {
			c.log.Debug().
				Str("coordinate", fmt.Sprintf("(x: %.0f, y: %.0f, z: %.0f)", coordinate.X, coordinate.Y, coordinate.Z)).
				Str("type", "circuit coordinate").
				Msg("No coordinate matched")

			return
		}
	}

	if len(matchingCircuitIDs) != 1 {
		return
	}

	circuitID := matchingCircuitIDs[0]

	var found bool
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

// setLapStartMarker sets the distance at which the current lap started.
func (c *Circuit) setLapStartMarker() {
	c.lapStartOdometerReading = c.lapProgressMeters

	if c.circuitIsKnown() {
		return
	}

	c.setCircuitLength()

}

// setCircuitLength sets the circuit length if it can be determined from the distance travelled
func (c *Circuit) setCircuitLength() {
	if c.lapProgressMeters <= 0 {
		return
	}

	circuitLength := int(c.lapProgressMeters - c.lapStartOdometerReading)

	if circuitLength > shortestCircuitLengthMeters {
		c.info.Length = circuitLength
	}
}

// updateDistanceTravelled updates the total distance travelled based on the provided coordinate.
// TODO: this is duplicated in fuelrange.distanceTravelled
func (c *Circuit) UpdateDistanceTravelled(odometerReading float64, lap int16, updateType updateType) {
	// New lap started
	if updateType == StartLineCoordinate {
		c.lapStartOdometerReading = odometerReading
		c.lapProgressMeters = 0
		c.lap = lap

		return
	}

	// Ignore updates until lap start marker is set
	if c.lapStartOdometerReading < 0 {
		return
	}

	// Wait for next lap start when odometer rolled back/reset or unexpected lap change
	if odometerReading < c.lapProgressMeters || lap != c.lap {
		c.lapStartOdometerReading = -1
		c.lapProgressMeters = 0
		c.lap = lap
	}

	c.lapProgressMeters = odometerReading - c.lapStartOdometerReading
}

// circuitIsKnown returns true if the current circuit is known
func (c *Circuit) circuitIsKnown() bool {
	return c.info.ID != circuitInfoInit.ID
}
