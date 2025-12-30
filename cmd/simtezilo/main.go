package main

import (
	"flag"
	"fmt"
	_ "image/png"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/logstore"
	"github.com/vwhitteron/simtezilo-dev/app/profiler"
)

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan exitcode.Code, 1)

	go func() {
		sig := <-sigs
		log.Printf("Received %v signal, shutting down\n", sig)

		done <- exitcode.Success
	}()

	var (
		configFile       string
		logLevelArg      string
		profilerEndpoint string
		version          bool
	)

	flag.StringVar(&configFile, "c", "", "Configuration file to load")
	flag.StringVar(&logLevelArg, "l", "info", "Log level. Default is 'info'")
	flag.StringVar(&profilerEndpoint, "p", "", "Send profiles to this Pyroscope endpoint (http://host:port). Default is off")
	flag.BoolVar(&version, "v", false, "Print version information and exit")

	flag.Parse()

	if version {
		fmt.Printf("Version: %s  Build Time: %s  Platform: %s\n", app.Version, app.BuildTime, app.Platform) //nolint:forbidigo // Allow for version output

		os.Exit(0)
	}

	// Initialize logger early so setup wizard can use it
	logLevel, err := zerolog.ParseLevel(logLevelArg)
	if err != nil {
		log.Fatalf("Invalid log level: %s", err)
	}

	// Create logger with in-memory store (max 5000 entries)
	loggerWithStore := logstore.NewLoggerWithStore(logLevel, 5000)
	logger := loggerWithStore.Logger

	if app.BuildTime == "" {
		app.BuildTime = time.Now().Format("2006-01-02_15:04:05")
	}

	logger.Info().
		Str("Version", app.Version).
		Str("BuildTime", app.BuildTime).
		Str("Platform", app.Platform).
		Msg("Starting Simtezilo")

	profiler, err := startPyroscope(profilerEndpoint, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to setup Pyroscope profiler")
	}

	var exitCode exitcode.Code

	app, err := app.New(app.Options{
		ConfigFile: configFile,
		Done:       done,
		Logger:     &logger,
		LogStore:   loggerWithStore.Store,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("Error creating app")
	}

	go app.Start()

	exitCode = <-done

	app.Close()

	if exitCode == exitcode.RestartApp {
		logger.Info().Msg("Restarting application - exiting to allow process restart")

		// Exit with success code so systemd will restart the process
		// This ensures all resources (speaker, sockets) are properly released
		exitCode = exitcode.Success
	}

	if profiler != nil {
		err = profiler.Shutdown()
		if err != nil {
			logger.Fatal().Err(err).Msg("Error shutting down Pyroscope profiler")
		}
	}

	logger.Info().Int("exitCode", int(exitCode)).Msg("Exiting app")

	os.Exit(int(exitCode))
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
