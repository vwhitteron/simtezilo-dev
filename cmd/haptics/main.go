package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"time"

	_ "image/png"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	"github.com/vwhitteron/go-pirateaudio/buttons"
	"github.com/vwhitteron/go-pirateaudio/textview"
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

	display, err := internal.NewPirateAudioDisplay(rotation)
	// lcd, err := display.Init()
	if err != nil {
		log.Fatal("Display init: ", err)
	}
	defer func() {
		// lcd.PowerOff()
		display.Close()
	}()

	spritesData, err := os.Open(assetDir + "/image/sprites.png")
	if err != nil {
		log.Fatal("sprites.png: ", err)
	}
	defer spritesData.Close()

	testImg, err := os.Open(assetDir + "/image/testimage.png")
	if err != nil {
		log.Fatal("testimage.png: ", err)
	}
	defer testImg.Close()

	sprites, _, err := image.Decode(spritesData)
	if err != nil {
		log.Fatal("Decode sprites.png: ", err)
	}

	spritesPaletted := sprites.(*image.Paletted)
	fmt.Printf("Sprite image bounds: %+v\n", spritesPaletted.Bounds())

	spriteMap := map[string]image.Rectangle{
		"splash": image.Rect(0*240, 0, 1*240, 240),
		"error":  image.Rect(1*240, 0, 2*240, 240),
		"gear1":  image.Rect(2*240, 0, 3*240, 240),
		"gear2":  image.Rect(3*240, 0, 4*240, 240),
		"gear3":  image.Rect(4*240, 0, 5*240, 240),
		"gear4":  image.Rect(5*240, 0, 6*240, 240),
		"gear5":  image.Rect(6*240, 0, 7*240, 240),
		"gear6":  image.Rect(7*240, 0, 8*240, 240),
		"gear7":  image.Rect(8*240, 0, 9*240, 240),
		"gear8":  image.Rect(9*240, 0, 10*240, 240),
	}

	splashImg := spritesPaletted.SubImage(spriteMap["splash"])
	errImg := spritesPaletted.SubImage(spriteMap["error"])

	fmt.Printf("Error image bounds: %+v\n", errImg.Bounds())
	// errImg, err := os.Open(assetDir + "/image/error.png")
	// if err != nil {
	// 	log.Fatal("error.png: ", err)
	// }
	// defer errImg.Close()

	// img, err := os.Open(assetDir + "/image/gt-telemetry.png")
	// if err != nil {
	// 	lcd.DrawImage(errImg)
	// 	log.Fatal("gt-telemetry.png: ", err)
	// }
	// defer img.Close()

	// lcd.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})

	// switch rotation {
	// case 90:
	// 	lcd.Rotate(display.ROTATION_90)
	// case 180:
	// 	lcd.Rotate(display.ROTATION_180)
	// case 270:
	// 	lcd.Rotate(display.ROTATION_270)
	// default:
	// 	lcd.Rotate(display.NO_ROTATION)
	// }

	display.LCD.DrawRAW(splashImg)

	thump, err := os.Open(assetDir + "/audio/thump.ogg")
	if err != nil {
		display.LCD.DrawRAW(errImg)
		log.Fatal("thump.ogg: ", err)
	}

	streamer, format, err := vorbis.Decode(thump)
	if err != nil {
		display.LCD.DrawRAW(errImg)
		log.Fatal(err)
	}

	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))

	buffer := beep.NewBuffer(format)
	buffer.Append(streamer)
	streamer.Close()

	buttons.OnButtonAPressed(func() {
		gain += 1

		go func() {
			thump := buffer.Streamer(0, buffer.Len())
			intensity := &effects.Volume{
				Streamer: thump,
				Base:     10,
				Volume:   gain / 10,
			}
			speaker.Play(intensity)
		}()

		display.LCD.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
		opts := textview.DefaultOpts
		opts.FGColor = textview.GREEN
		opts.FontSize = 64
		tv := textview.NewWithOptions(opts)
		tv.DrawChars(fmt.Sprintf("%-02.0f dB", gain))
	})

	buttons.OnButtonBPressed(func() {
		gain -= 1

		go func() {
			thump := buffer.Streamer(0, buffer.Len())
			intensity := &effects.Volume{
				Streamer: thump,
				Base:     10,
				Volume:   gain / 10,
			}
			speaker.Play(intensity)
		}()

		display.LCD.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
		opts := textview.DefaultOpts
		opts.FGColor = textview.GREEN
		opts.FontSize = 64
		tv := textview.NewWithOptions(opts)
		// tv.DrawFrames([]string{"Volume down", fmt.Sprintf("%0.0f dB", gain)})
		// tv.Draw("Volume down")
		// tv.Draw(fmt.Sprintf("%0.0f dB", gain))
		tv.DrawChars(fmt.Sprintf("%-02.0f dB", gain))
	})

	buttons.OnButtonXPressed(func() {
		display.LCD.DrawImage(testImg)
	})

	buttons.OnButtonYPressed(func() {
		display.LCD.DrawRAW(splashImg)
	})

	gt, err := telemetry_client.NewGTClient(telemetry_client.Config{
		// Source: "file:///Users/vaughan/Workspace/gt7/gt-telemetry/examples/simple/mt-panorama-nissan-r92cp-89.gtz",
	})
	if err != nil {
		display.LCD.DrawRAW(errImg)
		log.Fatal("Error creating GT client: ", err)
	}

	go gt.Run()

	lastGear := 20
	lastVehicleModel := ""
	// rideHeight := float32(150)
	// lastSuspensionHeight := telemetry_client.CornerSet{}
	// lastPacketTime := Time.Duration(0)
	var volume = float64(0)
	for {
		currentGear := gt.Telemetry.CurrentGear()

		if lastGear == 20 {
			lastGear = currentGear
			continue
		}

		if lastVehicleModel != gt.Telemetry.VehicleModel() {
			fmt.Printf("Vehicle: [%d] %s %s\n",
				gt.Telemetry.VehicleID(),
				gt.Telemetry.VehicleManufacturer(),
				gt.Telemetry.VehicleModel(),
			)
			lastVehicleModel = gt.Telemetry.VehicleModel()
			// rideHeight = gt.Telemetry.RideHeightMillimeters()
			// rideHeight = 1
			switch gt.Telemetry.VehicleType() {
			case "street":
				volume = -3 + gain
			case "race":
				volume = 0 + gain
			default:
				volume = -3 + gain
			}
		}

		if currentGear == 15 { // Neutral
			continue
		}

		if currentGear != lastGear {
			if gt.Telemetry.Flags().Loading == true {
				lastGear = currentGear
				continue
			}

			if replayMode == false && gt.Telemetry.Flags().Live == false {
				continue
			}

			fmt.Printf("Gear: %d (%0.1f dB)\n", currentGear, volume)
			lastGear = currentGear
			go func() {
				thump := buffer.Streamer(0, buffer.Len())
				if volume == 1 {
					speaker.Play(thump)
				} else {
					intensity := &effects.Volume{
						Streamer: thump,
						Base:     10,
						Volume:   volume / 10,
					}
					speaker.Play(intensity)
				}
			}()

			// debounce
			// time.Sleep(200 * time.Millisecond)
		}

		// if lastPacketTime > 0 {
		// 	timeDiff := (gt.Telemetry.TimeOfDay() - lastPacketTime)

		// 	shockFL := (lastSuspensionHeight.FrontLeft - gt.Telemetry.SuspensionHeightMillimeters().FrontLeft) / timeDiff
		// 	shockFR := (lastSuspensionHeight.FrontRight - gt.Telemetry.SuspensionHeightMillimeters().FrontRight) / timeDiff
		// 	shockRL := (lastSuspensionHeight.RearLeft - gt.Telemetry.SuspensionHeightMillimeters().RearLeft) / timeDiff
		// 	shockRR := (lastSuspensionHeight.RearRight - gt.Telemetry.SuspensionHeightMillimeters().RearRight) / timeDiff
		// 	fmt.Printf("Suspension: %0.2f %0.2f %0.2f %0.2f [%0.02f]\n", shockFL, shockFR, shockRL, shockRR, shockHeight)
		// }
		// if (impulseFL + impulseFR + impulseRL + impulseRR) > 2 {
		// 	fmt.Printf("Suspension: %0.2f %0.2f %0.2f %0.2f [%0.02f]\n", impulseFL, impulseFR, impulseRL, impulseRR, rideHeight)
		// }

		// lastSuspensionHeight = gt.Telemetry.SuspensionHeightMillimeters()
		time.Sleep(4 * time.Millisecond)
	}
}
