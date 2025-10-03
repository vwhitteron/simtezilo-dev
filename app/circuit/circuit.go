package circuit

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/zetetos/gt-telemetry/pkg/circuits"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

const (
	shortestCircuitLengthMeters int     = 900 // Minimum length in meters (Northern Isle Speedway)
	minConfidenceThreshold      float64 = 0.3 // Minimum confidence threshold (30%) before choosing a circuit
)

// Initial unknown circuit info.
var circuitInfoInit = circuits.CircuitInfo{
	ID:        "unknown",
	Name:      "unknown",
	Length:    0,
	StartLine: models.CoordinateNorm{X: 0, Y: 0, Z: 0},
}

// TODO: add godoc.
type Circuit struct {
	database                circuits.CircuitDB   // Circuit database for track identification
	log                     zerolog.Logger       // Logger instance
	info                    circuits.CircuitInfo // Current circuit information
	lap                     int16                // Current lap number being tracked
	lapStartOdometerReading float64              // Distance at which the current lap started
	lapProgressMeters       float64              // Lap distance tracking for uknown circuits
	lastCoordinate          models.Coordinate    // Last known coordinate for distance tracking
	candidates              CircuitCandidates    // Circuit candidates with confidence tracking
}

// New creates a new Circuit instance with the provided logger and initializes the circuit database.
func New(db circuits.CircuitDB, logger zerolog.Logger) (*Circuit, error) {
	return &Circuit{
		database:                db,
		log:                     logger.With().Str("package", "circuit").Logger(),
		lapStartOdometerReading: 0,
		lapProgressMeters:       0,
		info:                    circuitInfoInit,
		lastCoordinate:          models.Coordinate{},
		candidates:              make(CircuitCandidates),
	}, nil
}

// Reset clears the current circuit information and lap start marker distance.
func (c *Circuit) Reset() {
	c.info = circuits.CircuitInfo{
		ID:   circuitInfoInit.ID,
		Name: circuitInfoInit.Name,
	}

	c.candidates = make(CircuitCandidates)

	c.ResetLapProgress()

	c.log.Info().
		Msg("Circuit reset")
}

// ResetLapProgress clears the lap progress tracking without resetting the circuit information.
func (c *Circuit) ResetLapProgress() {
	c.lapStartOdometerReading = 0
	c.lapProgressMeters = 0
	c.lastCoordinate = models.Coordinate{}

	c.log.Info().
		Msg("Circuit reset")
}

// CircuitName returns the name of the current circuit.
func (c *Circuit) Name() string {
	return c.info.Name
}

// LengthMeters returns the length of the current circuit in meters.
func (c *Circuit) LengthMeters() float64 {
	return float64(c.info.Length)
}

// LapProgress returns the progress through the current lap as a value between 0 and 1.
func (c *Circuit) LapProgress() float64 {
	lapDistance := c.lapProgressMeters - c.lapStartOdometerReading

	if lapDistance < 0 || int(lapDistance) > c.info.Length {
		return 0
	}

	return lapDistance / float64(c.info.Length)
}

// LapProgressRemaining returns the remaining progress through the current lap as a value between 0 and 1.
func (c *Circuit) LapProgressRemaining() float64 {
	progress := c.LapProgress()

	return 1.0 - progress
}

// UpdateCircuit updates the current circuit information by matching the provided coordinate with a circuit DB entry
// The updateType flag indicates if the coordinate is from a start line crossing or general positional update.
func (c *Circuit) UpdateCircuit(odometerReading float64, lap int16, coordinate models.Coordinate, coordinateType models.CoordinateType) (didUpdate bool) {
	c.updateDistanceTravelled(odometerReading, lap, coordinateType)
	c.setLapStartMarker()

	var matchingCircuits []string

	circuitID, found := c.database.GetCircuitAtCoordinate(coordinate, coordinateType)
	if found {
		matchingCircuits = append(matchingCircuits, circuitID)
	}

	coordinateNorm := circuits.NormaliseStartLineCoordinate(coordinate)
	key := circuits.CoordinateNormToKey(coordinateNorm)

	if len(matchingCircuits) == 0 {
		c.log.Debug().
			Str("coordinate", key).
			Msg("No coordinate matched")

		return false
	}

	for _, circuitID := range matchingCircuits {
		c.updateCandidateConfidence(circuitID, key)
	}

	bestCandidate := c.bestCandidate()
	if bestCandidate == nil {
		return false
	}

	if bestCandidate.info.ID != c.info.ID {
		c.info = bestCandidate.info

		if coordinateType == models.CoordinateTypeStartLine {
			c.info.StartLine = coordinateNorm
		}

		c.log.Info().
			Str("track", c.info.Variation).
			Str("confidence", fmt.Sprintf("%.0f%%", bestCandidate.confidence*100)).
			Int("matches", len(bestCandidate.matchedCoords)).
			Msg("Circuit updated")

		return true
	}

	return false
}

// updateDistanceTravelled updates the total distance travelled based on the provided coordinate.
// TODO: this is duplicated in fuelrange.distanceTravelled.
func (c *Circuit) updateDistanceTravelled(odometerReading float64, lap int16, coordinateType models.CoordinateType) {
	// New lap started
	if coordinateType == models.CoordinateTypeStartLine {
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

// setLapStartMarker sets the distance at which the current lap started.
func (c *Circuit) setLapStartMarker() {
	c.lapStartOdometerReading = c.lapProgressMeters

	if c.circuitIsKnown() {
		return
	}

	c.setCircuitLength()
}

// setCircuitLength sets the circuit length if it can be determined from the distance travelled.
func (c *Circuit) setCircuitLength() {
	if c.lapProgressMeters <= 0 {
		return
	}

	circuitLength := int(c.lapProgressMeters - c.lapStartOdometerReading)

	if circuitLength > shortestCircuitLengthMeters {
		c.info.Length = circuitLength
	}
}

// circuitIsKnown returns true if the current circuit is known.
func (c *Circuit) circuitIsKnown() bool {
	return c.info.ID != circuitInfoInit.ID
}
