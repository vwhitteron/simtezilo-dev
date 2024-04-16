package main

import (
	"flag"
	"image/color"
	"log"
	"os"
	"time"

	"github.com/vwhitteron/go-pirateaudio/buttons"
	"github.com/vwhitteron/go-pirateaudio/display"
	"github.com/vwhitteron/go-pirateaudio/textview"
)

func main() {
	var image string
	var rotation int
	var off bool

	flag.StringVar(&image, "i", "assets/image/gt-telemetry.png", "Image to display. Default is gt-telemetry.png")
	flag.IntVar(&rotation, "r", 0, "Display rotation. Default is 0 degress")
	flag.BoolVar(&off, "o", false, "Turn off the display. Default is false")
	flag.Parse()

	dsp, err := display.Init()
	if err != nil {
		panic(err)
	}
	defer dsp.Close()

	if off {
		dsp.PowerOff()
		os.Exit(0)
	}

	img, err := os.Open(image)
	if err != nil {
		log.Fatal(err)
	}
	defer img.Close()

	// Set the screen color to white
	dsp.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})

	// Rotate before pushing pixels, so the image appears rotated
	switch rotation {
	case 90:
		dsp.Rotate(display.ROTATION_90)
	case 180:
		dsp.Rotate(display.ROTATION_180)
	case 270:
		dsp.Rotate(display.ROTATION_270)
	default:
		dsp.Rotate(display.NO_ROTATION)
	}

	dsp.DrawImage(img)

	buttons.OnButtonAPressed(func() {
		dsp.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
		opts := textview.DefaultOpts
		opts.FGColor = textview.GREEN
		tv := textview.NewWithOptions(opts)
		tv.Draw("")
		time.Sleep(3 * time.Second)
		tv.DrawChars("Wake up, Neo...")
		time.Sleep(3 * time.Second)
		tv.DrawChars("The Matrix has you...")
		time.Sleep(3 * time.Second)
		tv.DrawChars("Follow the white rabbit.")
	})

	buttons.OnButtonXPressed(func() {
		dsp.PowerOff()
		os.Exit(0)
	})

	for {
		time.Sleep(1)
	}
}
