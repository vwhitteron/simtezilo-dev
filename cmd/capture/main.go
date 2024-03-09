package main

import (
	"compress/gzip"
	"fmt"
	"log"
	"os"
	"time"

	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

type packet struct {
	data []byte
}

func main() {
	f, err := os.Create("assets/replay/test.gtz")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	buffer, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		log.Fatal(err)
	}
	defer buffer.Close()

	buffer.Comment = "Gran Turismo 7 Telemetry Replay"

	gt, err := telemetry_client.NewGTClient(telemetry_client.Config{})
	if err != nil {
		fmt.Println("Error creating GT client: ", err)
		os.Exit(1)
	}

	go gt.Run()

	fmt.Println("Waiting for replay to start")

	framesCaptured := -1
	lastTimeOfDay := time.Duration(0)
	sequenceID := ^uint32(0)
	startTime := time.Duration(0)
	diff := uint32(0)
	for {
		// ignore packets that have aldready been processed
		if sequenceID == gt.Telemetry.SequenceID() {
			time.Sleep(4 * time.Millisecond)
			continue
		}

		diff = gt.Telemetry.SequenceID() - sequenceID
		sequenceID = gt.Telemetry.SequenceID()

		// Set the last time seen when the first frame is received
		if lastTimeOfDay == time.Duration(0) {
			lastTimeOfDay = gt.Telemetry.TimeOfDay()
			continue
		}

		// Finish recording when the replay restarts
		if gt.Telemetry.TimeOfDay() == startTime {
			if framesCaptured < 60 {
				continue
			}

			fmt.Println("Replay restart detected")
			if err := buffer.Flush(); err != nil {
				log.Fatal(err)
			}
			break
		}

		// Start recording when the replay starts
		if framesCaptured == -1 && gt.Telemetry.TimeOfDay() != lastTimeOfDay {
			fmt.Printf("Starting capture: frame size: %d\n", len(gt.DecipheredPacket))

			startTime = gt.Telemetry.TimeOfDay()
			framesCaptured = 0

			extraData := fmt.Sprintf("Time of day: %+v, Manufacturer: %s, Model: %s",
				startTime,
				gt.Telemetry.VehicleManufacturer(),
				gt.Telemetry.VehicleModel(),
			)
			buffer.Extra = []byte(extraData)
		} else {
			time.Sleep(4 * time.Millisecond)
		}

		// Write the frame to the gzip buffer
		if framesCaptured >= 0 {
			if diff > 1 {
				fmt.Printf("Dropped %d frames\n", diff-1)
			}

			_, err := buffer.Write(gt.DecipheredPacket)
			if err != nil {
				log.Fatal(err)
			}
			framesCaptured++
			lastTimeOfDay = gt.Telemetry.TimeOfDay()
		}

		time.Sleep(4 * time.Millisecond)
	}

	fmt.Printf("Capture complete, total frames: %d\n", framesCaptured)
}
