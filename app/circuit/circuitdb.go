package circuit

import (
	_ "embed"
	"encoding/json"
	"fmt"

	gttelemetry "github.com/zetetos/gt-telemetry"
)

// CircuitInfo represents information about a specific race circuit
type CircuitInfo struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Region    string             `json:"region"`
	Length    int                `json:"length"`
	StartLine gttelemetry.Vector `json:"startline"`
}

// CircuitInventory represents the complete JSON structure from the embedded circuit inventory data
type CircuitInventory struct {
	Coordinates map[string][]string    `json:"coordinates"`
	StartLines  map[string][]string    `json:"startlines"`
	Circuits    map[string]CircuitInfo `json:"circuits"`
}

// CircuitDB provides an object and methods to access circuit information from the embedded inventory
type CircuitDB struct {
	inventory *CircuitInventory
}

//go:embed circuits.json
var inventoryJSON []byte

// NewDB creates a new CircuitDB instance by loading the circuit inventory from embedded JSON data
func NewDB() (*CircuitDB, error) {
	inventory := CircuitInventory{}

	err := json.Unmarshal([]byte(inventoryJSON), &inventory)
	if err != nil {
		return &CircuitDB{}, fmt.Errorf("unmarshall inventory JSON: %w", err)
	}

	// Populate start line lookup tables
	inventory.StartLines = make(map[string][]string)
	for _, circuit := range inventory.Circuits {
		normalisedCoordinate := NormaliseStartLineCoordinate(circuit.StartLine)
		key := CoordinateToKey(normalisedCoordinate)

		inventory.StartLines[key] = append(inventory.StartLines[key], circuit.ID)
	}

	return &CircuitDB{
		inventory: &inventory,
	}, nil
}

// GetTracksAtCoordinate returns the list of tracks at a given coordinate
func (c *CircuitDB) GetTracksAtCoordinate(coordinate gttelemetry.Vector) (trackIDs []string, found bool) {
	if c.inventory == nil {
		return nil, false
	}

	normalisedPos := NormaliseCircuitCoordinate(coordinate)
	key := CoordinateToKey(normalisedPos)

	trackIDs, found = c.inventory.Coordinates[key]

	return trackIDs, found
}

// GetTracksAtStartLine returns the list of tracks at a given start line coordinate
func (c *CircuitDB) GetTracksAtStartLine(coordinate gttelemetry.Vector) (trackIDs []string, found bool) {
	if c.inventory == nil {
		return nil, false
	}

	normalisedPos := NormaliseStartLineCoordinate(coordinate)
	key := CoordinateToKey(normalisedPos)

	trackIDs, found = c.inventory.StartLines[key]

	return trackIDs, found
}

// GetTrackInfo returns the track information for a given track ID
func (c *CircuitDB) GetTrackByID(trackID string) (track CircuitInfo, found bool) {
	if c.inventory == nil {
		return CircuitInfo{}, false
	}

	track, found = c.inventory.Circuits[trackID]
	track.ID = trackID

	return track, found
}

// GetAllTrackIDs returns all available track IDs
func (c *CircuitDB) GetAllTrackIDs() (trackIDs []string) {
	if c.inventory == nil {
		return nil
	}

	trackIDs = make([]string, 0, len(c.inventory.Circuits))
	for trackID := range c.inventory.Circuits {
		trackIDs = append(trackIDs, trackID)
	}

	return trackIDs
}

// GetTracksByRegion returns all tracks in a specific region
func (c *CircuitDB) GetTracksInRegion(region string) (tracks map[string]CircuitInfo) {
	if c.inventory == nil {
		return nil
	}

	tracks = make(map[string]CircuitInfo)
	for trackID, trackInfo := range c.inventory.Circuits {
		if trackInfo.Region == region {
			tracks[trackID] = trackInfo
		}
	}

	return tracks
}

// NormaliseStartLineCoordinate normalises a start line coordinate to reduce precision for location matching
func NormaliseStartLineCoordinate(coordinate gttelemetry.Vector) (normalised struct{ X, Y, Z int16 }) {
	normalised = struct{ X, Y, Z int16 }{
		X: int16(coordinate.X/32) * 32,
		Y: int16(coordinate.Y/4) * 4,
		Z: int16(coordinate.Z/32) * 32,
	}

	return normalised
}

// NormaliseCircuitCoordinate normalises a circuit coordinate to reduce precision for location matching
func NormaliseCircuitCoordinate(coordinate gttelemetry.Vector) (normalised struct{ X, Y, Z int16 }) {
	normalised = struct{ X, Y, Z int16 }{
		X: int16(coordinate.X/64) * 64,
		Y: int16(coordinate.Y/8) * 8,
		Z: int16(coordinate.Z/64) * 64,
	}

	return normalised
}

func CoordinateToKey(normalised struct{ X, Y, Z int16 }) string {
	return fmt.Sprintf("x:%d,y:%d,z:%d", normalised.X, normalised.Y, normalised.Z)
}
