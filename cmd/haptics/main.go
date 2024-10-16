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

var build string

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
	var hardware string
	var profilingEnabled bool
	var replayMode bool
	var source string
	var webEnabled bool

	flag.StringVar(&assetDir, "a", "./assets", "Path to the assets directory. Default is './assets'")
	flag.Float64Var(&gain, "g", -14.5, "Gain in decibels. Default is -14.5")
	flag.StringVar(&logLevel, "l", "warn", "Log level. Default is 'warn'")
	flag.IntVar(&orientation, "o", 0, "Display orientation. [*0, 90, 180, 270] degrees")
	flag.StringVar(&hardware, "h", "null", "Enable RPi hardware HAT. [pirateaudio, waveshare, *null]")
	flag.BoolVar(&profilingEnabled, "profiling", false, "Enable profiling. Default is false")
	flag.BoolVar(&replayMode, "r", false, "Output haptics for replays as well as live sessions. Default is false")
	flag.StringVar(&source, "s", "udp://255.255.255.255:33739", "Telemetry data source. Default is udp://255.255.255.255:33739")
	flag.BoolVar(&webEnabled, "w", false, "Enable web server. Default is false")
	flag.Parse()

	log.Printf("GT-Pi version %s\n", build)

	profiler, err := internal.NewPyroscopeProfiler(
		"http://10.255.1.128:4040",
		map[string]string{"hostname": os.Getenv("HOSTNAME")},
	)
	if err != nil {
		log.Fatal("Error creating Pyroscope profiler: ", err)
	}

	if profilingEnabled {
		err = profiler.Start()
		if err != nil {
			log.Fatal("Error starting Pyroscope profiler: ", err)
		}

		log.Println("Pyroscope profiler started with UI at " + profiler.Endpoint())
	}

	core, err := internal.NewCore(internal.CoreOptions{
		Done:        done,
		AssetDir:    assetDir,
		Gain:        gain,
		LogLevel:    logLevel,
		Orientation: orientation,
		Hardware:    hardware,
		ReplayMode:  replayMode,
		Source:      source,
		WebEnabled:  webEnabled,
	})
	if err != nil {
		log.Fatal("Error creating core: ", err)
	}

	go core.Run()

	<-done
	core.Close()
	err = profiler.Shutdown()
	if err != nil {
		log.Fatal("Error shutting down Pyroscope profiler: ", err)
	}
}
