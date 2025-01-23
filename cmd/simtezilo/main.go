package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "image/png"

	"github.com/vwhitteron/simtezilo-dev/internal"
)

var Version = "DEV"
var BuildTime string

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan bool, 1)

	go func() {
		sig := <-sigs
		log.Printf("Received %v signal, shutting down\n", sig)
		done <- true
	}()

	var logLevel string
	// var profilingEnabled bool
	var webEnabled bool

	flag.StringVar(&logLevel, "l", "", "Log level. Default is 'warn'")
	// flag.BoolVar(&profilingEnabled, "profiling", false, "Enable profiling. Default is false")
	flag.BoolVar(&webEnabled, "w", false, "Enable web server. Default is false")
	flag.Parse()

	if BuildTime == "" {
		BuildTime = time.Now().Format("2006-01-02_15:04:05")
	}
	fmt.Printf("Simtezilo version %s (built %s)\n", Version, BuildTime)

	profiler, err := internal.NewPyroscopeProfiler(
		"http://10.255.1.128:4040",
		map[string]string{
			"app":      "simtezilo",
			"version":  Version,
			"hostname": os.Getenv("HOSTNAME"),
		},
	)
	if err != nil {
		log.Fatal("Error creating Pyroscope profiler: ", err)
	}

	// if profilingEnabled {
	// 	err = profiler.Start()
	// 	if err != nil {
	// 		log.Fatal("Error starting Pyroscope profiler: ", err)
	// 	}

	// 	log.Println("Pyroscope profiler started with UI at " + profiler.Endpoint())
	// }

	core, err := internal.NewCore(internal.CoreOptions{
		BuildTime:  BuildTime,
		Done:       done,
		LogLevel:   logLevel,
		Version:    Version,
		WebEnabled: webEnabled,
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
