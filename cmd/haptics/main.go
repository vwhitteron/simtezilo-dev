package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "image/png"

	"github.com/vwhitteron/gt-pi/internal"
)

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan bool, 1)

	go func() {
		sig := <-sigs
		fmt.Printf("Received %v signal, shutting down\n", sig)
		done <- true
	}()

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

	core, err := internal.NewCore(internal.CoreOptions{
		AssetDir:           assetDir,
		Gain:               gain,
		LogLevel:           logLevel,
		Orientation:        orientation,
		PirateAudioEnabled: pirateAudioEnabled,
		ReplayMode:         replayMode,
		Source:             source,
	})
	if err != nil {
		log.Fatal("Error creating core: ", err)
	}

	go core.Run()

	<-done
	core.Close()
}
