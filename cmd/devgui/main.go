package main

import (
	"flag"
	"image"
	"log"
	"os"

	_ "image/png"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"github.com/vwhitteron/gt-pi/internal"
)

func main() {
	var assetDir string
	var gain float64
	var logLevel string
	var orientation int
	var pirateAudioEnabled bool
	var replayMode bool
	var source string

	flag.StringVar(&assetDir, "a", "./assets", "Path to the assets directory. Default is './assets'")
	flag.Float64Var(&gain, "g", -12, "Gain in decibels. Default is -12")
	flag.StringVar(&logLevel, "l", "warn", "Log level. Default is 'warn'")
	flag.IntVar(&orientation, "o", 0, "Display orientation. Default is 0 degrees")
	flag.BoolVar(&pirateAudioEnabled, "p", false, "Enable Pirate Audio features. Default is false")
	flag.BoolVar(&replayMode, "r", false, "Output haptics for replays as well as live sessions. Default is false")
	flag.StringVar(&source, "s", "udp://255.255.255.255:33739", "Telemetry data source. Default is udp://255.255.255.255:33739")
	flag.Parse()

	// opts := internal.CoreOptions{
	// 	AssetDir:           assetDir,
	// 	Gain:               gain,
	// 	LogLevel:           logLevel,
	// 	Orientation:        orientation,
	// 	PirateAudioEnabled: pirateAudioEnabled,
	// 	ReplayMode:         replayMode,
	// 	Source:             source,
	// }

	path := assetDir + "/image/pirateaudio.png"
	data, err := os.Open(path)
	if err != nil {
		log.Panicf("failed to load background %q: %e", path, err)
	}

	background, _, err := image.Decode(data)
	if err != nil {
		log.Panicf("failed to decode image %q: %e", path, err)
	}
	data.Close()

	myApp := app.New()
	window := myApp.NewWindow("GT Telemetry")

	// baseScreen := image.NewRGBA(image.Rect(0, 0, lcdWidth, lcdHeight))
	// lcdRect := image.Rect(lcdBGOffsetX, lcdBGOffsetY, lcdBGOffsetX+lcdWidth, lcdBGOffsetY+lcdHeight)
	// blue := color.RGBA{0, 0, 255, 255}

	screen := background.(*image.NRGBA)

	// draw.Draw(screen, lcdRect.Bounds(), &image.Uniform{blue}, image.ZP, draw.Src)

	img := canvas.NewImageFromImage(screen)
	img.FillMode = canvas.ImageFillOriginal
	window.SetContent(img)

	window.ShowAndRun()

}

func RunCore(opts internal.CoreOptions) {
	core, err := internal.NewCore(opts)
	if err != nil {
		log.Fatal("Error creating core: ", err)
	}

	go core.Run()
}
