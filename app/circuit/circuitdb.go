package circuit

import (
	_ "embed"
	"encoding/json"
	"fmt"

	gttelemetry "github.com/zetetos/gt-telemetry"
)

// CircuitInfo represents information about a specific race track
type CircuitInfo struct {
	ID        string             `json:"-"`
	Name      string             `json:"track_name"`
	Region    string             `json:"region"`
	Length    int                `json:"length"`
	StartLine gttelemetry.Vector `json:"-"` // TODO: add field to JSON DB
}

// CircuitInventory represents the complete JSON structure from racetrack_inventory.json
type CircuitInventory struct {
	Coordinates map[string][]string    `json:"coordinates"`
	StartLines  map[string][]string    `json:"startlines"`
	Circuits    map[string]CircuitInfo `json:"circuits"`
}

type CircuitDB struct {
	inventory *CircuitInventory
}

//go:embed circuits.json
var inventoryJSON []byte

func NewDB() (*CircuitDB, error) {
	inventory := CircuitInventory{}

	err := json.Unmarshal([]byte(inventoryJSON), &inventory)
	if err != nil {
		return &CircuitDB{}, fmt.Errorf("unmarshall inventory JSON: %w", err)
	}

	return &CircuitDB{
		inventory: &inventory,
	}, nil
}

// GetTracksAtCoordinate returns the list of tracks at a given coordinate
func (t *CircuitDB) GetTracksAtCoordinate(coordinate gttelemetry.Vector) (trackIDs []string, found bool) {
	if t.inventory == nil {
		return nil, false
	}

	normalisedPos := NormaliseTrackCoordinate(coordinate)
	key := NormalisedToKey(normalisedPos)

	trackIDs, found = t.inventory.Coordinates[key]

	return trackIDs, found
}

// GetTracksAtStartLine returns the list of tracks at a given start line coordinate
func (t *CircuitDB) GetTracksAtStartLine(coordinate gttelemetry.Vector) (trackIDs []string, found bool) {
	if t.inventory == nil {
		return nil, false
	}

	normalisedPos := NormaliseStartLineCoordinate(coordinate)
	key := NormalisedToKey(normalisedPos)

	trackIDs, found = t.inventory.StartLines[key]

	return trackIDs, found
}

// GetTrackInfo returns the track information for a given track ID
func (t *CircuitDB) GetTrackByID(trackID string) (track CircuitInfo, found bool) {
	if t.inventory == nil {
		return CircuitInfo{}, false
	}

	track, found = t.inventory.Circuits[trackID]
	track.ID = trackID

	return track, found
}

// GetAllTrackIDs returns all available track IDs
func (t *CircuitDB) GetAllTrackIDs() (trackIDs []string) {
	if t.inventory == nil {
		return nil
	}

	trackIDs = make([]string, 0, len(t.inventory.Circuits))
	for trackID := range t.inventory.Circuits {
		trackIDs = append(trackIDs, trackID)
	}

	return trackIDs
}

// GetTracksByRegion returns all tracks in a specific region
func (t *CircuitDB) GetTracksInRegion(region string) (tracks map[string]CircuitInfo) {
	if t.inventory == nil {
		return nil
	}

	tracks = make(map[string]CircuitInfo)
	for trackID, trackInfo := range t.inventory.Circuits {
		if trackInfo.Region == region {
			tracks[trackID] = trackInfo
		}
	}

	return tracks
}

func NormaliseStartLineCoordinate(coordinate gttelemetry.Vector) (normalised struct{ X, Y, Z int16 }) {
	normalised = struct{ X, Y, Z int16 }{
		X: int16(coordinate.X/32) * 32,
		Y: int16(coordinate.Y/4) * 4,
		Z: int16(coordinate.Z/32) * 32,
	}

	return normalised
}

func NormaliseTrackCoordinate(coordinate gttelemetry.Vector) (normalised struct{ X, Y, Z int16 }) {
	normalised = struct{ X, Y, Z int16 }{
		X: int16(coordinate.X/64) * 64,
		Y: int16(coordinate.Y/8) * 8,
		Z: int16(coordinate.Z/64) * 64,
	}

	return normalised
}

func NormalisedToKey(normalised struct{ X, Y, Z int16 }) string {
	return fmt.Sprintf("x:%d,y:%d,z:%d", normalised.X, normalised.Y, normalised.Z)
}
