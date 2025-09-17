package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	telemetry_client "github.com/zetetos/gt-telemetry"
)

const (
	// The FIA rules state that the starting grid has a min width of 15 meters.
	// 32m resolution should provide sufficient accuracy for most tracks.
	xResStartLine int16 = 32
	yResStartLine int16 = 4
	zResStartLine int16 = 32

	// Track map resultion is lower to reduce file size.
	// 64m resolution should be sufficient for most tracks.
	// Y (vertical) resolution is higher since elevation changes are much smaller than X/Z.
	xRestrack int16 = 64
	yResTrack int16 = 8
	zResTrack int16 = 64
)

type NormalisedPos struct {
	X int16 `json:"x"`
	Y int16 `json:"y"`
	Z int16 `json:"z"`
}

type TrackMap struct {
	TrackName         string          `json:"track_name"`
	TrackLengthMeters float64         `json:"track_length_meters"`
	StartingLine      NormalisedPos   `json:"starting_line"`
	Coordinates       []NormalisedPos `json:"coordinates"`
}

func main() {
	var outFile string
	var trackName string
	flag.StringVar(&outFile, "o", "track_map.json", "Output file name. Default: track_map.json")
	flag.StringVar(&trackName, "track", "", "Track name (optional)")
	flag.Parse()

	gt, err := telemetry_client.NewGTClient(telemetry_client.GTClientOpts{})
	if err != nil {
		log.Fatalf("Error creating GT client: %v", err)
	}

	go func() {
		for {
			err, recoverable := gt.Run()
			if err != nil {
				if recoverable {
					log.Printf("GT client error (recoverable): %v", err)
				} else {
					log.Fatalf("GT client error (non-recoverable): %v", err)
				}
			}
		}
	}()

	var (
		track             TrackMap
		lastLap           = gt.Telemetry.CurrentLap()
		lapStarted        bool
		seenCoords        = make(map[NormalisedPos]struct{})
		lastPos           *telemetry_client.Vector
		distanceTravelled float64
		minX, maxX        float32
		minY, maxY        float32
		minZ, maxZ        float32
		extentsInit       bool
	)
	track.TrackName = trackName

	// Handle Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nRecording interrupted, track save aborted.")

		os.Exit(0)
	}()

	ready := false
	// lapWasActive removed (was unused)
	for {
		if !lapStarted && !ready {
			fmt.Println("Ready...")
			ready = true
		}

		if gt.Telemetry.Flags().GamePaused {
			time.Sleep(8 * time.Millisecond)

			continue
		}

		currentLap := gt.Telemetry.CurrentLap()
		vehicleOnTrack := gt.Telemetry.RaceEntrants() > -1

		// Lap start detection
		if vehicleOnTrack && currentLap != lastLap {
			if lapStarted {
				// Lap has completed, save and exit
				fmt.Println("Lap complete. Saving track map...")

				track.TrackLengthMeters = distanceTravelled
				saveTrackMap(outFile, &track, minX, maxX, minY, maxY, minZ, maxZ, extentsInit)

				os.Exit(0)
			} else {
				fmt.Println("Lap start detected.")
				lapStarted = true
				lastLap = currentLap
			}
		}

		if lapStarted {
			pos := gt.Telemetry.PositionalMapCoordinates()
			norm := NormalisedPos{
				X: int16(pos.X/float32(xRestrack)) * xRestrack,
				Y: int16(pos.Y/float32(yResTrack)) * yResTrack, // Y is the vertical axis in telemetry
				Z: int16(pos.Z/float32(zResTrack)) * zResTrack,
			}

			if track.StartingLine == (NormalisedPos{}) {
				track.StartingLine = NormalisedPos{
					X: int16(pos.X/float32(xResStartLine)) * xResStartLine,
					Y: int16(pos.Y/float32(yResStartLine)) * yResStartLine, // Y is the vertical axis in telemetry
					Z: int16(pos.Z/float32(zResStartLine)) * zResStartLine,
				}

			}

			if _, exists := seenCoords[norm]; !exists {
				track.Coordinates = append(track.Coordinates, norm)
				seenCoords[norm] = struct{}{}

				// Update min/max extents as new points are added
				if !extentsInit {
					minX, maxX = pos.X, pos.X
					minY, maxY = pos.Y, pos.Y
					minZ, maxZ = pos.Z, pos.Z
					extentsInit = true
				} else {
					minX = min(minX, pos.X)
					maxX = max(maxX, pos.X)
					minY = min(minY, pos.Y)
					maxY = max(maxY, pos.Y)
					minZ = min(minZ, pos.Z)
					maxZ = max(maxZ, pos.Z)
				}

			}

			// Distance calculation (in meters)
			if lastPos != nil {
				dx := float64(pos.X - lastPos.X)
				dy := float64(pos.Y - lastPos.Y)
				dz := float64(pos.Z - lastPos.Z)
				dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
				if dist > 0 && dist < 500 {
					distanceTravelled += dist
				}
			}
			lastPos = &pos
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func saveTrackMap(filename string, track *TrackMap, minX, maxX, minY, maxY, minZ, maxZ float32, extentsInit bool) {
	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(track); err != nil {
		log.Fatalf("Failed to write JSON: %v", err)
	}
	fmt.Printf("Track map saved to %s\n\n", filename)
	fmt.Printf("Points: %d\n", len(track.Coordinates))
	fmt.Printf("Starting line: X = %d, Y = %d, Z = %d\n", track.StartingLine.X, track.StartingLine.Y, track.StartingLine.Z)
	fmt.Printf("Track length: %.2f meters\n", track.TrackLengthMeters)

	// Print map extents and size in xyz if available
	if extentsInit {
		fmt.Printf("Map extents: X = [%.0f, %.0f], Y = [%.0f, %.0f], Z = [%.0f, %.0f]\n", minX, maxX, minY, maxY, minZ, maxZ)
		fmt.Printf("Map size: X = %.0f, Y = %.0f, Z = %.0f\n", maxX-minX, maxY-minY, maxZ-minZ)
	}
}
