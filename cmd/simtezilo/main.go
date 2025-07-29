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

	"github.com/vwhitteron/simtezilo-dev/app"
)

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
	var profilerEndpoint string
	var webEnabled bool

	flag.StringVar(&logLevel, "l", "", "Log level. Default is 'warn'")
	flag.StringVar(&profilerEndpoint, "p", "", "Send profiles to this Pyroscope endpoint (http://host:port). Default is off")
	flag.BoolVar(&webEnabled, "w", false, "Enable web server. Default is false")
	flag.Parse()

	if app.BuildTime == "" {
		app.BuildTime = time.Now().Format("2006-01-02_15:04:05")
	}
	fmt.Printf("Simtezilo version %s (built %s)\n", app.Version, app.BuildTime)

	profiler, err := startPyroscope(profilerEndpoint)
	if err != nil {
		log.Fatalf("Failed to setup Pyroscope profiler: %s", err.Error())
	}

	app, err := app.NewApp(app.AppOptions{
		Done:       done,
		LogLevel:   logLevel,
		WebEnabled: webEnabled,
	})
	if err != nil {
		log.Fatal("Error creating app: ", err)
	}

	go app.Run()

	<-done
	app.Close()
	if profiler != nil {
		err = profiler.Shutdown()
		if err != nil {
			log.Fatalf("Error shutting down Pyroscope profiler: %s", err)
		}
	}
}

func startPyroscope(endpoint string) (*app.PyroscopeProfiler, error) {
	if endpoint == "" {
		return nil, nil
	}

	profiler, err := app.NewPyroscopeProfiler(
		endpoint,
		map[string]string{
			"app":       "simtezilo",
			"version":   app.Version,
			"buildTime": app.BuildTime,
			"hostname":  os.Getenv("HOSTNAME"),
		},
	)
	if err != nil {
		log.Fatal("create Pyroscope profiler: ", err)
	}

	err = profiler.Start()
	if err != nil {
		log.Fatal("start Pyroscope profiler: ", err)
	}

	log.Println("View profiling data inPyroscope at " + profiler.Endpoint())

	return profiler, nil
}
