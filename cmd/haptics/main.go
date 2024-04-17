package main

import (
	"flag"
	"image"
	"log"
	"strconv"
	"time"

	_ "image/png"

	"github.com/vwhitteron/go-pirateaudio/buttons"
	"github.com/vwhitteron/gt-pi/internal"
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

func main() {
	var assetDir string
	var gain float64
	var replayMode bool
	var rotation int

	flag.StringVar(&assetDir, "d", "./assets", "Path to the assets directory. Default is './assets'")
	flag.Float64Var(&gain, "g", -12, "Gain in decibels. Default is -12")
	flag.BoolVar(&replayMode, "y", false, "Output haptics for replays as well as live sessions. Default is false")
	flag.IntVar(&rotation, "r", 0, "Display rotation. Default is 0 degress")
	flag.Parse()

	display, err := internal.NewPirateAudioDisplay(rotation, assetDir)
	if err != nil {
		log.Fatal("Pirate Audio display init: ", err)
	}
	defer func() {
		display.Close()
	}()

	display.Show("splash")

	audio, err := internal.NewAudioOutputDevice(assetDir)
	if err != nil {
		log.Fatal("Audio output device init: ", err)
	}

	buttons.OnButtonAPressed(func() {
		gain += 1

		go func() {
			audio.Play("gearChange", gain)
		}()

		log.Printf("Gain: %-02.0f dB\n", gain)
	})

	buttons.OnButtonBPressed(func() {
		gain -= 1

		go func() {
			audio.Play("gearChange", gain)
		}()

		log.Printf("Gain: %-02.0f dB\n", gain)
	})

	sprites := []string{"splash", "error", "gear1", "gear2", "gear3", "gear4", "gear5", "gear6", "gear7", "gear8"}
	index := 0
	buttons.OnButtonXPressed(func() {
		index += 1
		if index >= len(sprites) {
			index = len(sprites) - 1
		}
		display.Show(sprites[index])
	})

	buttons.OnButtonYPressed(func() {
		index -= 1
		if index < 0 {
			index = 0
		}
		display.Show(sprites[index])
	})

	gt, err := telemetry_client.NewGTClient(telemetry_client.Config{
		// Source: "file:///Users/vaughan/Workspace/gt7/gt-telemetry/examples/simple/mt-panorama-nissan-r92cp-89.gtz",
	})
	if err != nil {
		display.Show("error")
		log.Fatal("Error creating GT client: ", err)
	}

	go gt.Run()

	lastGear := 1024
	currentVehicleID := uint32(0)
	var volume = float64(0)
	for {
		currentGear := gt.Telemetry.CurrentGear()

		if lastGear == 1024 {
			lastGear = currentGear
			continue
		}

		if currentVehicleID != gt.Telemetry.VehicleID() {
			log.Printf("Vehicle: [%d] %s %s\n",
				gt.Telemetry.VehicleID(),
				gt.Telemetry.VehicleManufacturer(),
				gt.Telemetry.VehicleModel(),
			)
			currentVehicleID = gt.Telemetry.VehicleID()
			switch gt.Telemetry.VehicleType() {
			case "street":
				volume = -3 + gain
			case "race":
				volume = 0 + gain
			default:
				volume = -3 + gain
			}
		}

		if currentGear != lastGear {
			if gt.Telemetry.Flags().Loading == true {
				lastGear = currentGear
				continue
			}

			if replayMode == false && gt.Telemetry.Flags().Live == false {
				continue
			}

			log.Printf("Gear: %d (%0.1f dB)\n", currentGear, volume)
			lastGear = currentGear
			go func() {
				if currentGear != 15 { // Neutral gear
					audio.Play("gearChange", volume)
				}
			}()
			gearName := ""
			switch currentGear {
			case 15:
				gearName = "N"
			case 0:
				gearName = "R"
			default:
				gearName = strconv.Itoa(currentGear)
			}
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			display.ShowTextCentered(canvas, gearName)
		}

		time.Sleep(4 * time.Millisecond)
	}
}
