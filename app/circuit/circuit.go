// Package circuit performs race circuit identification and lap tracking based on in-game 3D positional coordinates.
package circuit

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/zetetos/gt-telemetry/pkg/circuits"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

const (
	shortestCircuitLengthMeters int           = 900 // Minimum length in meters (Northern Isle Speedway)
	shortestLapTime             time.Duration = 10 * time.Second
	minConfidenceThreshold      float64       = 0.3 // Minimum confidence threshold (30%) before choosing a circuit
)

// Manager defines the interface for circuit management and lap tracking.
type Manager interface {
	Reset()
	ResetLapProgress()
	Name() string
	LengthMeters() float64
	LapProgress() float64
	LapProgressRemaining() float64
	UpdateCircuit(
		odometerReading float64,
		lap int16,
		lapTime time.Duration,
		coordinate models.Coordinate,
		coordinateType models.CoordinateType,
	) (didUpdate bool)
}

// Circuit provides circuit identification and lap tracking facilities.
// Conforms with the circuit.Manager interface.
type Circuit struct {
	database                circuits.CircuitDB   // Circuit database for track identification
	log                     zerolog.Logger       // Logger instance
	info                    circuits.CircuitInfo // Current circuit information
	lap                     int16                // Current lap number being tracked
	lapStartOdometerReading float64              // Distance at which the current lap started
	lapProgressMeters       float64              // Lap distance tracking for uknown circuits
	bestLapTime             time.Duration        // Time taken to complete the last lap
	observedLength          int                  // Observed circuit length of the circuit
	lastCoordinate          models.Coordinate    // Last known coordinate for distance tracking
	candidates              Candidates           // Circuit candidates with confidence tracking
}

// New creates a new Circuit instance with the provided logger and initializes the circuit database.
func New(db circuits.CircuitDB, logger zerolog.Logger) (*Circuit, error) {
	return &Circuit{
		database:                db,
		log:                     logger.With().Str("package", "circuit").Logger(),
		lapStartOdometerReading: 0,
		lapProgressMeters:       0,
		bestLapTime:             100 * time.Hour,
		info:                    circuitInfoInit(),
		lastCoordinate:          models.Coordinate{},
		candidates:              make(Candidates),
	}, nil
}

// Reset clears the current circuit information and lap start marker distance.
func (c *Circuit) Reset() {
	c.info = circuits.CircuitInfo{
		ID:   circuitInfoInit().ID,
		Name: circuitInfoInit().Name,
	}

	c.candidates = make(Candidates)

	c.ResetLapProgress()

	c.log.Info().
		Msg("Circuit reset")
}

// ResetLapProgress clears the lap progress tracking without resetting the circuit information.
func (c *Circuit) ResetLapProgress() {
	c.lapStartOdometerReading = 0
	c.lapProgressMeters = 0
	c.bestLapTime = 100 * time.Hour
	c.lastCoordinate = models.Coordinate{}

	c.log.Info().
		Msg("Circuit reset")
}

// Name returns the name of the current circuit.
func (c *Circuit) Name() string {
	return c.info.Name
}

// LengthMeters returns the length of the current circuit in meters.
func (c *Circuit) LengthMeters() float64 {
	if c.observedLength > shortestCircuitLengthMeters {
		return float64(c.observedLength)
	}

	return float64(c.info.Length)
}

// LapProgress returns the progress through the current lap as a value between 0 and 1.
func (c *Circuit) LapProgress() float64 {
	progress := c.lapProgressMeters / float64(c.info.Length)

	return max(min(progress, 1), 0)
}

// LapProgressRemaining returns the remaining progress through the current lap as a value between 0 and 1.
func (c *Circuit) LapProgressRemaining() float64 {
	progress := c.LapProgress()

	return 1.0 - progress
}

// UpdateCircuit updates the current circuit information by matching the provided coordinate with a circuit DB entry
// The updateType flag indicates if the coordinate is from a start line crossing or general positional update.
func (c *Circuit) UpdateCircuit(
	odometerReading float64,
	lap int16,
	lapTime time.Duration,
	coordinate models.Coordinate,
	coordinateType models.CoordinateType,
) (didUpdate bool) {
	c.updateDistanceTravelled(odometerReading, lap, coordinateType)

	if coordinateType == models.CoordinateTypeStartLine {
		c.setCircuitLength(lapTime)
		c.setLapStartMarker(odometerReading)
	}

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
			Int("length_meters", c.info.Length).
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
func (c *Circuit) setLapStartMarker(odomoterReading float64) {
	c.lapStartOdometerReading = odomoterReading

	if c.circuitIsKnown() {
		return
	}

	c.lapProgressMeters = 0

	c.log.Info().
		Float64("odometer", odomoterReading).
		Msg("Lap start marker set")
}

// setCircuitLength sets the circuit length if it can be determined from the distance travelled.
func (c *Circuit) setCircuitLength(lapTime time.Duration) {
	if lapTime < shortestLapTime || lapTime >= c.bestLapTime {
		return
	}

	circuitLength := int(c.lapProgressMeters)

	if circuitLength <= shortestCircuitLengthMeters {
		return
	}

	c.observedLength = circuitLength
	c.bestLapTime = lapTime

	c.log.Info().
		Int("length_meters", circuitLength).
		Str("lap_time", lapTime.String()).
		Msg("Observed circuit length updated")
}

// Initial unknown circuit info.
func circuitInfoInit() circuits.CircuitInfo {
	return circuits.CircuitInfo{
		ID:        "unknown",
		Name:      "unknown",
		Length:    0,
		StartLine: models.CoordinateNorm{X: 0, Y: 0, Z: 0},
	}
}

// circuitIsKnown returns true if the current circuit is known.
func (c *Circuit) circuitIsKnown() bool {
	return c.info.ID != circuitInfoInit().ID
}
