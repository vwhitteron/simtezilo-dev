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
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/profiler"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
)

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan exitcode.ExitCode, 1)

	go func() {
		sig := <-sigs
		log.Printf("Received %v signal, shutting down\n", sig)

		done <- exitcode.Success
	}()

	var (
		vehicleDB        string
		logLevelArg      string
		profilerEndpoint string
		setupMode        bool
		version          bool
	)

	flag.StringVar(&vehicleDB, "d", "", "Path to vehicle database file")
	flag.StringVar(&logLevelArg, "l", "info", "Log level. Default is 'info'")
	flag.StringVar(&profilerEndpoint, "p", "", "Send profiles to this Pyroscope endpoint (http://host:port). Default is off")
	flag.BoolVar(&version, "v", false, "Print version information and exit")

	platform := hardware.NewPlatform(app.Platform)

	if platform.SupportsSetupMode() {
		flag.BoolVar(&setupMode, "s", false, "Run in setup mode")
	}

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

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel)

	// If setup wizard mode is requested, run it and exit
	if platform.SupportsSetupMode() && setupMode {
		go setupmode.Run(done, &logger)

		exitCode := <-done

		os.Exit(int(exitCode))
	}

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

	app, err := app.New(app.Options{
		VehicleDB: vehicleDB,
		Done:      done,
		Logger:    &logger,
		Platform:  platform,
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("Error creating app")
	}

	go app.Run()

	exitCode := <-done

	app.Close()

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
