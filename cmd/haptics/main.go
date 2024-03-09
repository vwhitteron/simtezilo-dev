package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

func main() {
	f, err := os.Open("assets/audio/thump-s-curve.ogg")
	if err != nil {
		log.Fatal(err)
	}

	streamer, format, err := vorbis.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))

	buffer := beep.NewBuffer(format)
	buffer.Append(streamer)
	streamer.Close()

	gt, err := telemetry_client.NewGTClient(telemetry_client.Config{})
	if err != nil {
		fmt.Println("Error creating GT client: ", err)
		os.Exit(1)
	}

	go gt.Run()

	lastGear := 20
	lastVehicleModel := ""
	for {
		currentGear := gt.Telemetry.CurrentGear()

		if lastGear == 20 {
			lastGear = currentGear
			continue
		}

		if lastVehicleModel != gt.Telemetry.VehicleModel() {
			fmt.Printf("Vehicle: %s %s\n", gt.Telemetry.VehicleManufacturer(), gt.Telemetry.VehicleModel())
			lastVehicleModel = gt.Telemetry.VehicleModel()
		}

		if currentGear == 15 { // Neutral
			continue
		}

		if currentGear != lastGear {
			fmt.Printf("Gear: %d\n", currentGear)
			lastGear = currentGear
			go func() {
				thump := buffer.Streamer(0, buffer.Len())
				speaker.Play(thump)
			}()

			// debounce
			time.Sleep(200 * time.Millisecond)
		}

		time.Sleep(8 * time.Millisecond)
	}
}
