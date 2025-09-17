package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Coordinate struct {
	X int `json:"x"`
	Y int `json:"y"`
	Z int `json:"z"`
}

type TrackFile struct {
	TrackName         string       `json:"track_name"`
	TrackLengthMeters float64      `json:"track_length_meters"`
	StartingLine      Coordinate   `json:"starting_line"`
	Coordinates       []Coordinate `json:"coordinates"`
}

func coordKey(c Coordinate) string {
	return fmt.Sprintf("x:%d,y:%d,z:%d", c.X, c.Y, c.Z)
}

func startLineKey(c Coordinate) string {
	return coordKey(c)
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <tracks_directory> <output_file>\n", os.Args[0])
		os.Exit(1)
	}

	tracksDir := os.Args[1]
	outputFile := os.Args[2]

	coordinateMap := make(map[string][]string)
	startLineMap := make(map[string][]string)
	trackInfoMap := make(map[string]map[string]interface{})
	trackStartLines := make(map[string]Coordinate) // Store starting lines for analysis

	err := filepath.Walk(tracksDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		// Ignore the output file if it exists in the same dir
		if strings.HasSuffix(info.Name(), "track_inventory.json") || strings.HasSuffix(info.Name(), filepath.Base(outputFile)) {
			return nil
		}

		// region is the parent directory name under tracks
		rel, _ := filepath.Rel(tracksDir, path)
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) < 2 {
			return nil
		}
		region := parts[0]
		trackID := strings.TrimSuffix(parts[1], ".json")

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read %s: %v\n", path, err)
			return nil
		}
		var tf TrackFile
		if err := json.Unmarshal(data, &tf); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse %s: %v\n", path, err)
			return nil
		}

		// 1. Coordinate map
		for _, c := range tf.Coordinates {
			k := coordKey(c)
			coordinateMap[k] = append(coordinateMap[k], trackID)
		}

		// 2. Start line map
		sk := startLineKey(tf.StartingLine)
		startLineMap[sk] = append(startLineMap[sk], trackID)

		// Store starting line for analysis
		trackStartLines[trackID] = tf.StartingLine

		// 3. Track info map
		trackInfoMap[trackID] = map[string]interface{}{
			"track_name": tf.TrackName,
			"length":     uint16(tf.TrackLengthMeters),
			"region":     region,
		}

		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking tracks dir: %v\n", err)
		os.Exit(1)
	}

	// Analysis: Find tracks without unique coordinates
	fmt.Println("\n=== ANALYSIS: Track Coordinate Uniqueness ===")

	type TrackStats struct {
		TrackID         string
		TrackName       string
		Region          string
		TotalCoords     int
		UniqueCoords    int
		UniquePercent   float64
		StartLineUnique bool
	}

	var trackStats []TrackStats
	tracksWithoutUniqueCoords := make([]string, 0)

	// Build a map of track -> coordinates for easier counting
	trackCoordinates := make(map[string][]string)
	for coord, trackList := range coordinateMap {
		for _, trackID := range trackList {
			trackCoordinates[trackID] = append(trackCoordinates[trackID], coord)
		}
	}

	// For each track, calculate uniqueness stats
	for trackID := range trackInfoMap {
		totalCoords := len(trackCoordinates[trackID])
		uniqueCoords := 0

		// Count unique coordinates for this track
		for _, coord := range trackCoordinates[trackID] {
			if len(coordinateMap[coord]) == 1 {
				uniqueCoords++
			}
		}

		var uniquePercent float64
		if totalCoords > 0 {
			uniquePercent = float64(uniqueCoords) / float64(totalCoords) * 100
		}

		trackName := trackInfoMap[trackID]["track_name"].(string)
		region := trackInfoMap[trackID]["region"].(string)

		// Check if starting line coordinate is unique
		startLineCoord := trackStartLines[trackID]
		startLineKey := startLineKey(startLineCoord)
		startLineUnique := len(startLineMap[startLineKey]) == 1

		trackStats = append(trackStats, TrackStats{
			TrackID:         trackID,
			TrackName:       trackName,
			Region:          region,
			TotalCoords:     totalCoords,
			UniqueCoords:    uniqueCoords,
			UniquePercent:   uniquePercent,
			StartLineUnique: startLineUnique,
		})

		if uniqueCoords == 0 {
			tracksWithoutUniqueCoords = append(tracksWithoutUniqueCoords, trackID)
		}
	}

	// Sort by track name (alphabetically)
	for i := 0; i < len(trackStats)-1; i++ {
		for j := i + 1; j < len(trackStats); j++ {
			if trackStats[i].TrackName > trackStats[j].TrackName {
				trackStats[i], trackStats[j] = trackStats[j], trackStats[i]
			}
		}
	}

	// Calculate the maximum track name length for proper column alignment
	maxTrackNameLen := 0
	for _, stats := range trackStats {
		if len(stats.TrackName) > maxTrackNameLen {
			maxTrackNameLen = len(stats.TrackName)
		}
	}
	// Add some padding for the column
	trackNameColWidth := maxTrackNameLen + 2

	// Display results
	headerFormat := fmt.Sprintf("%%-%ds %%-%ds %%8s %%8s %%8s %%10s\n", trackNameColWidth, 12)
	separatorFormat := fmt.Sprintf("%%-%ds %%-%ds %%8s %%8s %%8s %%10s\n", trackNameColWidth, 12)
	dataFormat := fmt.Sprintf("%%s%%-%ds %%-%ds %%8d %%8d %%7.1f%%%% %%8s\n", trackNameColWidth-2, 12) // -2 for marker space

	fmt.Printf(headerFormat, "Track Name", "Region", "Total", "Unique", "% Unique", "Start Uniq")
	fmt.Printf(separatorFormat, strings.Repeat("-", trackNameColWidth), strings.Repeat("-", 12), "--------", "--------", "--------", "----------")

	for _, stats := range trackStats {
		marker := "  "
		if stats.UniqueCoords == 0 {
			marker = "⚠️ "
		}

		startLineMarker := "✅"
		if !stats.StartLineUnique {
			startLineMarker = "❌"
		}

		fmt.Printf(dataFormat,
			marker,
			stats.TrackName,
			stats.Region,
			stats.TotalCoords,
			stats.UniqueCoords,
			stats.UniquePercent,
			startLineMarker)
	}

	if len(tracksWithoutUniqueCoords) > 0 {
		fmt.Printf("\n⚠️  %d tracks have ZERO unique coordinates and are completely composed of shared coordinates.\n", len(tracksWithoutUniqueCoords))
	} else {
		fmt.Println("\n✅ All tracks have at least one unique coordinate.")
	}

	// Count tracks with non-unique starting lines
	nonUniqueStartLines := 0
	for _, stats := range trackStats {
		if !stats.StartLineUnique {
			nonUniqueStartLines++
		}
	}

	if nonUniqueStartLines > 0 {
		fmt.Printf("❌ %d tracks have non-unique starting line positions.\n", nonUniqueStartLines)
	} else {
		fmt.Println("✅ All tracks have unique starting line positions.")
	}

	out := map[string]interface{}{
		"coordinate_map": coordinateMap,
		"start_line_map": startLineMap,
		"track_info_map": trackInfoMap,
	}

	outData, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal output: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outputFile, outData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write output: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Wrote inventory to %s\n", outputFile)
}
