package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"time"

	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

func main() {
	config := telemetry_client.GTClientOpts{
		StatsEnabled: true,
	}

	client, err := telemetry_client.NewGTClient(config)
	if err != nil {
		log.Fatalf("Failed to create GT client: %s", err.Error())
	}

	go client.Run()

	fmt.Println("Waiting for data...    Press Ctrl+C to exit")

	file, err := os.Create("telemetry3.raw")
	if err != nil {
		log.Fatalf("Failed to create file: %s", err.Error())
	}
	defer file.Close()

	buffer := bufio.NewWriter(file)

	// lastHeight := float32(0)

	sequenceId := uint32(0)
	for {
		if sequenceId == client.Telemetry.SequenceID() {
			time.Sleep(4 * time.Millisecond)
			continue
		}

		sequenceId = client.Telemetry.SequenceID()

		height := client.Telemetry.SuspensionHeightMillimeters().FrontLeft
		// delta := height - lastHeight
		// value := byte(int16(60000 * (delta / client.Telemetry.RideHeightMillimeters())))
		// value := byte(int16(60000 * (delta / 100)))
		value := byte(int16(60000 * (height / 100)))

		err = buffer.WriteByte(value)
		if err != nil {
			log.Fatal(err)
		}

		// client.Telemetry.SuspensionHeightMillimeters().FrontLeft,
		// client.Telemetry.SuspensionHeightMillimeters().FrontRight,
		// client.Telemetry.SuspensionHeightMillimeters().RearLeft,
		// client.Telemetry.SuspensionHeightMillimeters().RearRight,
		// client.Telemetry.RideHeightMillimeters(),

		if sequenceId%60 == 0 {
			err = buffer.Flush()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("Flushed 60 bytes")
		}

		time.Sleep(4 * time.Millisecond)
	}
}
