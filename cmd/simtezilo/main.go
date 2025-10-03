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

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app"
	"github.com/vwhitteron/simtezilo-dev/app/profiler"
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

	var (
		vehicleDB        string
		logLevelArg      string
		profilerEndpoint string
		webEnabled       bool
	)

	flag.StringVar(&vehicleDB, "d", "", "Path to vehicle database file")
	flag.StringVar(&logLevelArg, "l", "info", "Log level. Default is 'info'")
	flag.StringVar(&profilerEndpoint, "p", "", "Send profiles to this Pyroscope endpoint (http://host:port). Default is off")
	flag.BoolVar(&webEnabled, "w", false, "Enable web server. Default is false")
	flag.Parse()

	logLevel, err := zerolog.ParseLevel(logLevelArg)
	if err != nil {
		log.Fatalf("Invalid log level: %s", err)
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel)

	if app.BuildTime == "" {
		app.BuildTime = time.Now().Format("2006-01-02_15:04:05")
	}

	logger.Info().Str("Version", app.Version).Str("BuildTime", app.BuildTime).Msg("Starting Simtezilo")

	profiler, err := startPyroscope(profilerEndpoint, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to setup Pyroscope profiler")
	}

	app, err := app.New(app.AppOptions{
		VehicleDB:  vehicleDB,
		Done:       done,
		Logger:     &logger,
		WebEnabled: webEnabled,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("Error creating app")
	}

	go app.Run()

	<-done
	app.Close()

	if profiler != nil {
		err = profiler.Shutdown()
		if err != nil {
			logger.Fatal().Err(err).Msg("Error shutting down Pyroscope profiler")
		}
	}
}

func startPyroscope(endpoint string, logger *zerolog.Logger) (*profiler.PyroscopeProfiler, error) {
	if endpoint == "" {
		return nil, nil
	}

	profiler, err := profiler.NewPyroscopeProfiler(
		endpoint,
		map[string]string{
			"app":       "simtezilo",
			"version":   app.Version,
			"buildTime": app.BuildTime,
			"hostname":  os.Getenv("HOSTNAME"),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create profiler: %w", err)
	}

	err = profiler.Start()
	if err != nil {
		return nil, fmt.Errorf("start profiler: %w", err)
	}

	logger.Info().Str("Component", "pyroscope").Str("endpoint", profiler.Endpoint()).Msg("profiler started")

	return profiler, nil
}
