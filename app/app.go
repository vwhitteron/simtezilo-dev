// Package app implements the main application logic for Simtezilo, a racing simulator telemetry and haptics engine.
package app

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/cache"
	"github.com/vwhitteron/simtezilo-dev/app/circuit"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/fuelrange"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/console"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/spotpear"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/logstore"
	"github.com/vwhitteron/simtezilo-dev/app/odometer"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio/discord"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/tyres"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

type gameState int

const (
	gameStateUnknown gameState = iota
	gameStateMainMenu
	gameStateRaceMenu
	gameStateOnCircuit
)

// raceState holds transient race data for haptic generation and pit radio notifications.
type raceState struct {
	// Telemetry session information
	sequenceNumber uint32        // Current telemetry sequence number
	sequenceDelta  uint32        // Delta between current and last telemetry sequence number
	timeOfDay      time.Duration // Time of day in the telemetry session

	// Vehicle information
	transmissionGear int // Current transmission gear

	// Race timing information
	lapNumber   int16         // Current lap number
	lastLapTime time.Duration // Last lap time duration
	isLive      bool          // Flag to indicate if the telemetry is live or a replay
	gameState   gameState
}

// appState holds the overall application state.
type appState struct {
	hapticsEnabled   bool           // Flag to indicate if haptics are enabled // TODO: move state to haptics?
	telemetryActive  bool           // Flag to indicate if telemetry is active
	sessionEnded     bool           // Flag to indicate if session end has been handled
	raceCompleteTime time.Duration  // Time of day when the race was completed
	current          raceState      // Race state at the current telemetry sequence
	last             raceState      // Race state at the last telemetry sequence
	engine           engineState    // Engine state for haptic generation
	recorder         recordingState // Telemetry recording state
}

// App is the main application struct holding all components and state.
type App struct {
	log      zerolog.Logger     // Application logger
	logStore *logstore.Store    // In-memory log storage
	config   *config.Config     // Application configuration
	done     chan exitcode.Code // Channel to signal application shutdown with exit code

	cache cache.Cache // Cache manager

	setupMode *setupmode.SetupMode // Setup mode manager

	ui *ui.UserInterface // User interface manager

	i18n    *i18n.I18n       // Language translations
	display hardware.Display // Hardware display interface

	gtClient   *gttelemetry.Client      // GT telemetry client
	pitRadio   pitradio.PitRadio        // Pit radio notification service
	kinematics kinematics.State         // Vehicle kinematics tracker
	synth      *synthesizer.Synthesizer // Audio synthesizer for haptic feedback

	odometer  *odometer.Odometer  // Odometer for distance tracking
	fuelRange fuelrange.Estimator // Fuel range estimator
	circuit   circuit.Manager     // Circuit information and tracking

	transmissionGainMin float64 // Minimum transmission gain based on vehicle type

	state         appState                // Application state tracker
	pitRadioState *pitRadioState          // Current pit radio state
	vehicle       vehicle.Characteristics // Current vehicle information
	tyres         *tyres.Tyre             // Tyre monitoring

	telemetryChartFeed chan map[string]float32 // Channel for sending telemetry data to web UI
	webUI              *webui.WebUI            // Web UI server and handler
	webSequenceID      uint32                  // Last sequence ID sent to the web UI

	lapStartEvents chan uint32 // Channel for notifying new lap starts

	httpServer        *http.Server // Shared HTTP server for both modes
	activeHTTPHandler http.Handler // Current handler (setup mode or run mode)

	// Chassis haptics state
	jerkPeakHold         float64       // Peak hold value for jerk to prevent cancellation
	jerkPeakHoldTime     time.Time     // Time when peak hold was last updated
	jerkPeakHoldDuration time.Duration // Duration to hold peak based on pulse length
}

// Options holds configuration options for initializing the App.
type Options struct {
	ConfigFile string             // Path to configuration file
	Done       chan exitcode.Code // Channel to signal application shutdown with exit code
	Logger     *zerolog.Logger    // Logger instance for application logging
	LogStore   *logstore.Store    // In-memory log storage
}

// New creates a new App instance and sets up all components based on the provided options.
func New(opts Options) (*App, error) {
	newApp := &App{
		log:      opts.Logger.With().Str("package", "app").Logger(),
		logStore: opts.LogStore,
		done:     opts.Done,
		state: appState{
			current: raceState{
				transmissionGear: kinematics.NullGear,
			},
			last: raceState{
				transmissionGear: kinematics.NullGear,
			},
		},
		kinematics:         kinematics.NewKinematicsState(),
		telemetryChartFeed: make(chan map[string]float32, 600),
		lapStartEvents:     make(chan uint32),
	}

	newApp.initializeConfig(opts)

	err := newApp.initializeI18n(opts)
	if err != nil {
		return nil, err
	}

	hidEvents := make(chan ui.HIDInputEvent, 10)

	err = newApp.initializeHardware(hidEvents)
	if err != nil {
		return nil, err
	}

	// Initialize setupMode after display is created
	newApp.setupMode = setupmode.New(setupmode.Options{
		Config:  newApp.config,
		Done:    newApp.done,
		Logger:  &newApp.log,
		Display: newApp.getDisplayLCD(),
	})

	err = newApp.initializeUI(opts, hidEvents)
	if err != nil {
		return nil, err
	}

	err = newApp.initializeComponents(opts)
	if err != nil {
		return nil, err
	}

	newApp.initializeDiscord(opts)

	newApp.log.Debug().
		Str("component", "app").
		Str("result", "success").
		Msg("init")

	return newApp, nil
}

// RunResult is the result of a mode function.
type RunResult int

const (
	RunResultContinue RunResult = iota
	RunResultSwitchMode
	RunResultExit
)

// Start launches the application and switches between setup and run modes as needed.
func (a *App) Start() {
	for {
		status := a.setupMode.Status(context.Background())
		setupModeActive := status.Available && status.FlagEnabled

		if setupModeActive {
			result := a.runSetupMode()
			if result == RunResultExit {
				return
			}
		} else {
			result := a.runAppMode()
			if result == RunResultExit {
				return
			}
		}
	}
}

// Close performs cleanup and resource deallocation before application exit.
func (a *App) Close() {
	a.log.Info().Msg("Shutting down app")

	err := a.synth.Close()
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "audio output device").
			Str("result", "failure").
			Msg("close")
	}

	if a.pitRadio != nil {
		err = a.pitRadio.Close()
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("result", "failure").
				Msg("close")
		}
	} else {
		a.log.Info().
			Str("component", "discord").
			Str("result", "skipped").
			Str("reason", "object is nil").
			Msg("close")
	}

	err = a.ui.Screen.RenderSplashScreen(a.i18n.GetString("ui.quit"))
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "ui").
			Str("sub", "screen").
			Str("result", "failure").
			Msg("render splash screen")
	}

	time.Sleep(1 * time.Second)
	a.display.Close()
}

// runSetupMode runs setup mode and returns ModeSwitchResult.
func (a *App) runSetupMode() RunResult {
	a.log.Info().Msg("Launching setup mode")

	if a.httpServer == nil {
		a.startHTTPServer()
	}

	a.activeHTTPHandler = a.setupMode.GetHTTPHandler()

	a.setupMode.Run()

	select {
	case code := <-a.done:
		if code == exitcode.Success || code == exitcode.SetupMode {
			a.log.Info().Msg("Setup mode signaled switch to run mode")

			return RunResultSwitchMode
		}

		a.stopHTTPServer()

		a.done <- code

		return RunResultExit
	default:
		status := a.setupMode.Status(context.Background())
		if status.Available && !status.FlagEnabled {
			a.log.Info().Msg("Setup mode completed, switching to run mode")

			return RunResultSwitchMode
		}

		return RunResultContinue
	}
}

// runAppMode runs the main app logic and returns ModeSwitchResult.
func (a *App) runAppMode() RunResult {
	a.log.Info().Msg("Launching run mode")

	// Ensure webUI is initialized before setting handler
	if a.config.GetAppWebUIEnabled() && a.webUI == nil {
		status := a.setupMode.Status(context.Background())

		a.webUI = webui.New(webui.Config{
			Log:                a.log,
			Port:               a.config.GetAppWebUIPort(),
			TelemetryChartFeed: a.telemetryChartFeed,
			Config:             a.config,
			ShutdownChan:       a.done,
			SetupModeAvailable: status.Available,
			LogStore:           a.logStore,
		})
	}

	if a.config.GetAppWebUIEnabled() {
		if a.httpServer == nil {
			a.startHTTPServer()
		}

		if a.webUI != nil {
			a.activeHTTPHandler = a.webUI.GetHTTPHandler()
		}
	} else {
		if a.httpServer != nil {
			a.stopHTTPServer()
		}

		a.activeHTTPHandler = nil
	}

	a.run()

	select {
	case code := <-a.done:
		if code == exitcode.SetupMode {
			a.log.Info().Msg("Setup mode requested from run mode")

			return RunResultSwitchMode
		}

		if code == exitcode.RestartGTClient {
			a.log.Info().Msg("GT client restart requested, reinitializing")

			err := a.reinitializeGTClient()
			if err != nil {
				a.log.Error().Err(err).Msg("Failed to reinitialize GT client")

				a.done <- exitcode.InternalErr

				return RunResultExit
			}

			return RunResultContinue
		}

		a.stopHTTPServer()

		a.done <- code

		return RunResultExit
	default:
		return RunResultContinue
	}
}

// run starts the main application loop and associated goroutines.
func (a *App) run() {
	a.startBackgroundTasks()

	a.startAudioOutput()

	a.mainLoop()
}

// initializeConfig sets up configuration and logging.
func (a *App) initializeConfig(opts Options) {
	// load config from file
	a.config = config.New(config.Options{
		ConfigFile: opts.ConfigFile,
		Logger:     *opts.Logger,
	})

	zerolog.FloatingPointPrecision = 5

	// update to configured log level when greater than current
	configLogLevel, err := zerolog.ParseLevel(a.config.GetAppLogLevel())
	if err != nil {
		a.log.Error().Int("config value", int(configLogLevel)).Msg("invalid log level")
	}

	if configLogLevel < a.log.GetLevel() || configLogLevel >= zerolog.NoLevel {
		a.log = a.log.Level(configLogLevel).With().Logger()
		a.log.Debug().Str("level", configLogLevel.String()).Str("source", "config").Msg("log level update")
	}

	cacheDir := filepath.Join(a.config.GetAppBaseDir(), "data", "cache")
	a.cache = cache.New(cacheDir, *opts.Logger)
}

// initializeI18n sets up language translations.
func (a *App) initializeI18n(opts Options) error {
	var err error

	a.i18n, err = i18n.New(
		a.config.GetAppLanguage(),
		*opts.Logger,
	)
	if err != nil {
		a.log.Error().Err(err).Str("component", "i18n").Str("result", "failure").Msg("init")

		return err
	}

	a.config.SetI18n(a.i18n)
	a.log.Debug().Str("language", a.i18n.LanguageCode()).Str("result", "success").Msg("init language")

	return nil
}

// initializeHardware sets up display and HID hardware based on configuration.
func (a *App) initializeHardware(hidEvents chan ui.HIDInputEvent) error {
	var err error

	// initialise display and button hardware
	switch a.config.GetHardwareModel() {
	case "pirateaudio":
		err = a.initializePirateAudio(hidEvents)
	case "spotpear":
		err = a.initializeSpotpear(hidEvents)
	case "waveshare":
		err = a.initializeWaveshare(hidEvents)
	default:
		a.initializeConsole(hidEvents)
	}

	return err
}

// initializePirateAudio sets up Pirate Audio hardware.
func (a *App) initializePirateAudio(hidEvents chan ui.HIDInputEvent) error {
	hardware.Init()

	orientation := a.config.GetDisplayOrientation()

	var err error

	a.display, err = pirateaudio.NewDisplay(pirateaudio.DisplayOptions{
		Orientation: orientation,
		I18n:        a.i18n,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "pirate audio").
			Str("sub", "display").
			Str("result", "failure").
			Msg("init")

		return err
	}

	a.log.Debug().
		Str("component", "pirate audio").
		Str("sub", "display").
		Str("result", "success").
		Msg("init")

	pirateaudio.SetupHID(orientation, hidEvents)
	a.log.Debug().
		Str("component", "pirate audio").
		Str("sub", "hid").
		Msg("init")

	return nil
}

// initializeSpotpear sets up Spotpear hardware.
func (a *App) initializeSpotpear(hidEvents chan ui.HIDInputEvent) error {
	hardware.Init()

	var err error

	a.display, err = spotpear.NewDisplay(spotpear.DisplayOptions{
		Orientation: a.config.GetDisplayOrientation(),
		I18n:        a.i18n,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "spotpear game 1.3").
			Str("sub", "display").
			Str("result", "failure").
			Msg("init")

		return err
	}

	a.log.Debug().
		Str("component", "spotpear game 1.3").
		Str("sub", "display").
		Str("result", "success").
		Msg("init")

	spotpear.SetupHID(hidEvents)
	log.Debug().
		Str("component", "spotpear game 1.3").
		Str("sub", "hid").
		Msg("init")

	return nil
}

// initializeWaveshare sets up Waveshare hardware.
func (a *App) initializeWaveshare(hidEvents chan ui.HIDInputEvent) error {
	hardware.Init()

	orientation := a.config.GetDisplayOrientation()

	var err error

	a.display, err = waveshare.NewDisplay(waveshare.DisplayOptions{
		Orientation: orientation,
		I18n:        a.i18n,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "waveshare 14972").
			Str("sub", "display").
			Str("result", "failure").
			Msg("init")

		return err
	}

	a.log.Debug().
		Str("component", "waveshare 14972").
		Str("sub", "display").
		Str("result", "success").
		Msg("init")

	waveshare.SetupHID(orientation, hidEvents)
	log.Debug().
		Str("component", "waveshare 14972").
		Str("sub", "hid").
		Msg("init")

	return nil
}

// initializeConsole sets up console display.
func (a *App) initializeConsole(hidEvents chan ui.HIDInputEvent) {
	a.display = console.New()
	a.log.Debug().
		Str("component", "console").
		Str("sub", "display").
		Str("result", "success").
		Msg("init")

	go console.SetupHID(hidEvents)

	a.log.Debug().
		Str("component", "console").
		Str("sub", "hid").
		Msg("init")
}

// initializeUI sets up the user interface.
func (a *App) initializeUI(opts Options, hidEvents chan ui.HIDInputEvent) error {
	a.ui = ui.NewUserInterface(&ui.Config{
		I18n:             a.i18n,
		HIDEvents:        hidEvents,
		Display:          a.display,
		LiveData:         &ui.LiveData{Gear: kinematics.NullGear},
		Log:              *opts.Logger,
		SettingsCallback: a.settingAction,
		Done:             a.done,
	})

	err := a.ui.Screen.RenderSplashScreen(Version)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "ui").
			Str("sub", "screen").
			Str("result", "failure").
			Msg("render splash screen")

		return fmt.Errorf("render splash screen: %w", err)
	}

	return nil
}

// initializeComponents sets up synthesizer, GT client, and other core components.
func (a *App) initializeComponents(opts Options) error {
	var err error

	// initialise synthesizer
	a.synth, err = synthesizer.New(&synthesizer.SynthOpts{
		Config:     a.config.GetSynthesizer(),
		Logger:     *opts.Logger,
		Kinematics: &a.kinematics,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "synth").
			Str("result", "failure").
			Msg("init")

		_ = a.ui.Screen.RenderErrorScreen("Synth init")

		return err
	}

	// initialise GT telemetry client
	gtClientLogger := opts.Logger.With().Str("component", "gt client").Logger()

	a.gtClient, err = gttelemetry.New(gttelemetry.Options{
		Source:    a.config.GetTelemetrySource(),
		Logger:    &gtClientLogger,
		LogLevel:  a.config.GetAppLogLevel(),
		VehicleDB: a.config.GetAppVehicleDBFile(),
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "gt client").
			Str("result", "failure").
			Msg("init")

		_ = a.ui.Screen.RenderErrorScreen("GT client init")

		return err
	}

	a.odometer = odometer.New(*opts.Logger)

	a.fuelRange = fuelrange.New(*opts.Logger)

	a.tyres = tyres.New(
		a.config.GetTyreTemperatureOptimalCelsius(),
		a.config.GetTyreTemperatureOperatingWindow(),
		a.config.GetTyreTemperatureMarginCelsius(),
		models.CornerSet{},
	)

	a.circuit, err = circuit.New(*a.gtClient.CircuitDB, *opts.Logger)
	if err != nil {
		// TODO: fatal error?
		a.log.Error().
			Err(err).
			Str("package", "circuit").
			Str("result", "failure").
			Msg("init")
	}

	return nil
}

// reinitializeGTClient reinitializes the GT telemetry client with current config settings.
func (a *App) reinitializeGTClient() error {
	gtClientLogger := a.log.With().Str("component", "gt client").Logger()

	var err error

	a.gtClient, err = gttelemetry.New(gttelemetry.Options{
		Source:    a.config.GetTelemetrySource(),
		Logger:    &gtClientLogger,
		LogLevel:  a.config.GetAppLogLevel(),
		VehicleDB: a.config.GetAppVehicleDBFile(),
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "gt client").
			Str("result", "failure").
			Msg("reinit")

		_ = a.ui.Screen.RenderErrorScreen("GT client reinit")

		return err
	}

	// Reinitialize circuit with new GT client
	a.circuit, err = circuit.New(*a.gtClient.CircuitDB, a.log)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("package", "circuit").
			Str("result", "failure").
			Msg("reinit")
	}

	a.log.Info().
		Str("source", a.config.GetTelemetrySource()).
		Msg("GT client reinitialized")

	return nil
}

// initializeDiscord sets up Discord pit radio if enabled.
func (a *App) initializeDiscord(opts Options) {
	if !a.config.GetDiscordEnabled() {
		return
	}

	discordBotConfig := discord.Config{
		Token:          a.config.GetDiscordToken(),
		ChannelID:      a.config.GetDiscordChannelID(),
		VoiceChannelID: a.config.GetDiscordVoiceChannelID(),
		GuildID:        a.config.GetDiscordGuildID(),
		MessageGap:     time.Duration(a.config.GetMessageSendIntervalMs()) * time.Millisecond,
		Cache:          &a.cache,
		SampleBank:     a.synth.EffectSampleBank(),
		Logger:         *opts.Logger,
	}

	var err error

	a.pitRadio, err = discord.New(discordBotConfig)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "discord").
			Str("result", "failure").
			Msg("init")
	}

	a.resetPitRadioState()
}

// startHTTPServer creates and starts the HTTP server.
func (a *App) startHTTPServer() {
	port := a.config.GetAppWebUIPort()
	a.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           http.HandlerFunc(a.handleHTTP),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	go func() {
		a.log.Info().Msgf("Starting HTTP server on port %d", port)

		err := a.httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			a.log.Error().Err(err).Msg("HTTP server error")
		}
	}()
}

// stopHTTPServer gracefully shuts down the HTTP server.
func (a *App) stopHTTPServer() {
	if a.httpServer == nil {
		return
	}

	a.log.Info().Msg("Stopping HTTP server")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := a.httpServer.Shutdown(ctx)
	if err != nil {
		a.log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	a.httpServer = nil
}

// handleHTTP delegates requests to the current handler based on mode.
func (a *App) handleHTTP(writer http.ResponseWriter, request *http.Request) {
	if a.activeHTTPHandler == nil {
		http.NotFound(writer, request)

		return
	}

	a.activeHTTPHandler.ServeHTTP(writer, request)
}

// getDisplayLCD returns the underlying ST7789LCD display if available, otherwise returns nil.
func (a *App) getDisplayLCD() *display.ST7789LCD {
	if lcd, ok := a.display.(*display.ST7789LCD); ok {
		return lcd
	}

	return nil
}

// startBackgroundTasks launches all necessary background goroutines for the application.
func (a *App) startBackgroundTasks() {
	go a.ui.HIDEventHandler()

	go a.newLapHandler()

	if a.pitRadio != nil {
		go a.pitRadio.BackgroundTask()
	}

	go func() {
		for {
			recoverable, err := a.gtClient.Run()
			if err != nil {
				_ = a.ui.Screen.RenderSplashScreen("GT client error")

				if recoverable {
					a.log.Error().
						Err(err).
						Str("component", "gt client").
						Str("result", "failure").
						Msg("run")

					continue
				}

				a.log.Fatal().
					Err(err).
					Str("component", "gt client").
					Str("result", "failure").
					Msg("run")
			}
		}
	}()
}

func (a *App) startAudioOutput() {
	outputSampleRate := beep.SampleRate(a.config.GetOutputSampleRateHz())
	hapticStreamer := synthesizer.NewHapticStream(a.synth, outputSampleRate)

	err := speaker.Init(
		outputSampleRate,
		outputSampleRate.N(time.Second/15),
	)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "audio output device").
			Str("result", "failure").
			Msg("init")
		_ = a.ui.Screen.RenderErrorScreen("Audio output init")

		return
	}

	go speaker.Play(hapticStreamer)
}

// mainLoop is the primary application loop handling telemetry updates, haptics, UI updates, and pit radio
// notifications.
func (a *App) mainLoop() {
	tickerHaptics := time.NewTicker((1000 / hapticFrameRate) * time.Millisecond)
	tickerGeneral := time.NewTicker((1000 / telemetryFrameRate) * time.Millisecond)
	tickerEngineHaptics := time.NewTicker((1000 / engineHapticFrameRate) * time.Millisecond)
	tickerDisplay := time.NewTicker((1000 / displayFrameRate) * time.Millisecond)
	tickerPitRadio := time.NewTicker((1000 / pitRadioFrameRate) * time.Millisecond)
	tickerDebug := time.NewTicker(30 * time.Second)

	a.log.Debug().Str("component", "app").Str("result", "success").Msg("main loop started")

	for {
		// wait for the GT telemetry client to start receiveing telemetry
		if a.gtClient.Telemetry.SequenceID() == 0 {
			time.Sleep(8 * time.Millisecond)

			continue
		}

		select {
		case <-a.done:
			return
		case <-tickerHaptics.C:
			a.handleHapticsTick()
		case <-tickerGeneral.C:
			a.handleGeneralTick()
		case <-tickerEngineHaptics.C:
			a.handleEngineHapticsTick()
		case <-tickerDisplay.C:
			a.handleDisplayTick()
		case <-tickerPitRadio.C:
			a.handlePitRadioTick()
		case <-tickerDebug.C:
			a.handleDebugTick()
		}
	}
}

// handleHapticsTick processes haptics-related updates.
func (a *App) handleHapticsTick() {
	a.handleGameStateChange()

	stateChanged := a.updateState()

	if a.gtClient.Telemetry.IsInMainMenu() {
		return
	}

	if stateChanged {
		a.handleVehicleChange()
		a.generateForceHaptics()
		a.detectRecordingTrigger()
	}

	a.checkRaceComplete()
	a.checkForNewLap()
}

// handleGeneralTick processes general telemetry updates.
func (a *App) handleGeneralTick() {
	if a.gtClient.Telemetry.IsOnCircuit() {
		a.updateFuelRange()
		a.updateTyreTemperature()
	}

	a.updateCircuit()
	a.sendTelemetryChartData()
}

// handleEngineHapticsTick processes engine haptics updates.
func (a *App) handleEngineHapticsTick() {
	if a.gtClient.Telemetry.IsOnCircuit() {
		a.generateEngineHaptic()
	}
}

// handleDisplayTick processes display updates.
func (a *App) handleDisplayTick() {
	a.ui.UpdateDisplay(ui.LiveData{
		Gear:            a.kinematics.Current.TransmissionGear,
		TelemetryActive: a.state.telemetryActive,
	})
}

// handlePitRadioTick processes pit radio updates.
func (a *App) handlePitRadioTick() {
	if a.gtClient.Telemetry.IsOnCircuit() {
		a.sendPitRadioMessage()
	}
}

// handleDebugTick processes debug logging.
func (a *App) handleDebugTick() {
	if a.log.GetLevel() > zerolog.DebugLevel {
		return
	}

	if !a.gtClient.Telemetry.IsOnCircuit() {
		return
	}

	a.log.Debug().
		Int("lap", int(a.state.current.lapNumber)).
		Float32("percent", a.gtClient.Telemetry.FuelLevelPercent()).
		Int("odometer", int(a.odometer.Read())).
		Float64("rate", a.fuelRange.UsageRatePerKm()).
		Int("range_meters", int(a.fuelRange.DistanceMeters())).
		Int("lap_remaining_pc", int(a.circuit.LapProgressRemaining()*100)).
		Int("circuit_length", int(a.circuit.LengthMeters())).
		Msg("debug fuel range")

	averageTemp := (a.gtClient.Telemetry.TyreTemperatureCelsius().FrontLeft +
		a.gtClient.Telemetry.TyreTemperatureCelsius().FrontRight +
		a.gtClient.Telemetry.TyreTemperatureCelsius().RearLeft +
		a.gtClient.Telemetry.TyreTemperatureCelsius().RearRight) / 4

	a.log.Debug().
		Float32("temp_avg", averageTemp).
		Float32("temp_fl", a.gtClient.Telemetry.TyreTemperatureCelsius().FrontLeft).
		Float32("temp_fr", a.gtClient.Telemetry.TyreTemperatureCelsius().FrontRight).
		Float32("temp_rl", a.gtClient.Telemetry.TyreTemperatureCelsius().RearLeft).
		Float32("temp_rr", a.gtClient.Telemetry.TyreTemperatureCelsius().RearRight).
		Msg("debug tyre temp")
}

func (a *App) newLapHandler() bool {
	a.log.Debug().
		Str("handler", "new lap events").
		Msg("Start")

	for {
		select {
		case <-a.lapStartEvents:
			lap := a.state.current.lapNumber
			lapTime := a.state.current.lastLapTime
			coordinate := a.gtClient.Telemetry.PositionalMapCoordinates()
			odometerReading := a.odometer.Add(coordinate)

			didUpdate := a.circuit.UpdateCircuit(odometerReading, lap, lapTime, coordinate, models.CoordinateTypeStartLine)
			if didUpdate {
				a.state.last.lastLapTime = 0
			}

			a.notifyLapTime()
			a.notifyLapNumber()
		default:
			time.Sleep(8 * time.Millisecond)
		}
	}
}

// sessionIsComplete checks if the session has completed.
// Returns true if the session is complete.
func (a *App) sessionIsComplete() bool {
	if a.gtClient.Finished {
		a.log.Debug().Msg("session finished")

		return true
	}

	return false
}

// resetAppState resets the application state.
func (a *App) resetAppState() {
	a.state.last = raceState{
		transmissionGear: kinematics.NullGear,
		isLive:           true,
		gameState:        a.state.current.gameState,
	}

	a.state.current = raceState{
		transmissionGear: kinematics.NullGear,
		isLive:           true,
		gameState:        a.getGameState(),
	}

	a.synth.Silence()

	a.kinematics = kinematics.NewKinematicsState()

	a.state.sessionEnded = false

	a.log.Debug().Msg("App state reset")
}

func (a *App) getGameState() gameState {
	switch {
	case a.gtClient.Telemetry.IsInMainMenu():
		return gameStateMainMenu

	case a.gtClient.Telemetry.IsInRaceMenu():
		return gameStateRaceMenu

	case a.gtClient.Telemetry.IsOnCircuit():
		return gameStateOnCircuit

	default:
		return gameStateUnknown
	}
}

func (a *App) handleGameStateChange() {
	// Check if telemetry stream has ended (disconnect, crash, shutdown)
	if a.sessionIsComplete() {
		// Only handle session completion once to prevent log spam
		if !a.state.sessionEnded {
			a.disableHaptics("telemetry stream ended")
			a.resetPitRadioState()
			a.vehicle = vehicle.Characteristics{}
			a.odometer.Reset()
			a.fuelRange.Reset()
			a.circuit.Reset()
			a.resetAppState()
			a.stopRecording()
			a.log.Info().Msg("Telemetry stream ended")
			a.state.sessionEnded = true
		}

		return
	}

	if a.state.current.gameState == a.state.last.gameState || a.state.current.gameState == gameStateUnknown {
		return
	}

	switch {
	case a.state.current.gameState == gameStateMainMenu:
		a.disableHaptics("main menu")
		a.resetPitRadioState()
		a.vehicle = vehicle.Characteristics{}
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.circuit.Reset()
		a.resetAppState()
		a.stopRecording()

		a.log.Debug().Msg("Entered main menu")

	case a.state.current.gameState == gameStateRaceMenu:
		a.disableHaptics("race menu")
		a.resetPitRadioState()
		a.odometer.Reset()
		a.fuelRange.ResetEstimate()
		a.circuit.ResetLapProgress()
		a.stopRecording()

		a.log.Debug().Msg("Entered race menu")

	case a.state.current.gameState == gameStateOnCircuit:
		a.log.Debug().Msg("Vehicle on circuit")

	case a.liveFlagHasChanged():
		a.resetPitRadioState()
		a.vehicle = vehicle.Characteristics{}
		a.fuelRange.ResetEstimate()
		a.fuelRange.SetLive(a.state.current.isLive)
		a.circuit.Reset()
		a.resetAppState()

		a.log.Info().Bool("is_live", a.state.current.isLive).Msg("Live flag change")

	case a.timeOfDayHasReset():
		a.disableHaptics("time of day reset")
		a.resetPitRadioState()
		a.fuelRange.ResetEstimate()
		a.circuit.ResetLapProgress()

		a.log.Debug().Msg("Time of day reset")

	// Assume vehicle pit stop
	case a.gtClient.Telemetry.Flags().Loading:
		a.fuelRange.ResetEstimate()

		a.log.Debug().Msg("Loading flag")
	}
}

// handleVehicleChange checks for changes in the vehicle and updates vehicle data accordingly.
func (a *App) handleVehicleChange() {
	if a.vehicleHasChanged() {
		a.disableHaptics("vehicle changed")

		a.updateVehicle()
	}
}

// updateVehicle updates the vehicle characteristics from the current telemetry vehicle ID.
func (a *App) updateVehicle() {
	vehicleType := vehicle.DetermineVehicleType(a.gtClient.Telemetry.VehicleType())
	engine := a.getEngineData()
	revLimit := a.gtClient.Telemetry.EngineRPMLight().Max

	a.adjustEngineHaptics(&engine, revLimit)

	var wheelbaseMeters float32

	var trackFrontMeters float32

	var trackRearMeters float32

	wheelbase := a.gtClient.Telemetry.VehicleWheelbaseMillimeters()

	if wheelbase > 0 {
		wheelbaseMeters = float32(wheelbase) / 1000
	} else {
		wheelbaseMeters = (float32(a.gtClient.Telemetry.VehicleLengthMillimeters()) / 1000) * 0.55
	}

	trackFront := a.gtClient.Telemetry.VehicleTrackFrontMillimeters()
	trackRear := a.gtClient.Telemetry.VehicleTrackRearMillimeters()

	if trackFront > 0 || trackRear > 0 {
		trackFrontMeters = float32(trackFront) / 1000
		trackRearMeters = float32(trackRear) / 1000
	} else {
		trackFrontMeters = (float32(a.gtClient.Telemetry.VehicleWidthMillimeters()) / 1000) * 0.85
		trackRearMeters = trackFrontMeters
	}

	trackWidthMeters := (trackFrontMeters + trackRearMeters) / 2

	a.vehicle = vehicle.Characteristics{
		ID:          a.gtClient.Telemetry.VehicleID(),
		VehicleType: vehicleType,
		Engine:      engine,
		RevLimit:    a.normalizeRevLimit(revLimit),
		Dimensions: vehicle.Dimensions{
			WheelbaseMeters:    wheelbaseMeters,
			TrackWidthMeters:   trackWidthMeters,
			LongitudinalRadius: wheelbaseMeters / 2,
			TransverseRadius:   trackWidthMeters / 2,
		},
	}

	a.setTransmissionGain(vehicleType)
	a.resetVehicleState()
	a.logVehicleUpdate(engine, revLimit)
}

// getEngineData retrieves and processes engine characteristics.
func (a *App) getEngineData() vehicle.EngineCharacteristics {
	engineLayout := a.gtClient.Telemetry.VehicleEngineLayout()
	bankAngle := a.gtClient.Telemetry.VehicleEngineBankAngle()
	crankPlaneAngle := a.gtClient.Telemetry.VehicleEngineCrankPlaneAngle()
	revLimit := a.gtClient.Telemetry.EngineRPMLight().Max

	engine, err := a.getEngineCharacteristics(engineLayout, bankAngle, crankPlaneAngle, revLimit)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("engine_layout", engineLayout).
			Float32("cylinder_angle", bankAngle).
			Float32("crank_plane_angle", crankPlaneAngle).
			Msg("failed to get engine characteristics")
	}

	return engine
}

// adjustEngineHaptics adjusts engine haptics based on pulse rate limits.
func (a *App) adjustEngineHaptics(engine *vehicle.EngineCharacteristics, revLimit uint16) {
	peakNaturalPulseRate := float64(revLimit) * engine.FiringFrequency
	peakPulseRate := peakNaturalPulseRate * engine.Haptics.PulseScale

	if peakPulseRate > maxPulseRate {
		engine.Haptics.PulseScale = (maxPulseRate / peakPulseRate) * engine.Haptics.PulseScale
	}
}

// normalizeRevLimit sets a default rev limit if not available.
func (a *App) normalizeRevLimit(revLimit uint16) uint16 {
	if revLimit == 0 {
		return 8000
	}

	return revLimit
}

// setTransmissionGain sets the transmission gain based on vehicle type.
func (a *App) setTransmissionGain(vehicleType vehicle.TypeName) {
	switch vehicleType {
	case vehicle.TypeRace, vehicle.TypeTuned:
		a.transmissionGainMin = a.config.GetTransmissionGain() + a.config.GetTransmissionGainMinRace()
	case vehicle.TypeStreet:
		fallthrough
	default:
		a.transmissionGainMin = a.config.GetTransmissionGain() + a.config.GetTransmissionGainMinStreet()
	}
}

// resetVehicleState resets vehicle-related state.
func (a *App) resetVehicleState() {
	a.state.last.transmissionGear = a.state.current.transmissionGear
	a.odometer.Reset()
	a.fuelRange.Reset()
}

// logVehicleUpdate logs vehicle update information.
func (a *App) logVehicleUpdate(engine vehicle.EngineCharacteristics, revLimit uint16) {
	bankAngle := a.gtClient.Telemetry.VehicleEngineBankAngle()
	crankPlaneAngle := a.gtClient.Telemetry.VehicleEngineCrankPlaneAngle()
	peakNaturalPulseRate := float64(revLimit) * engine.FiringFrequency
	peakPulseRate := peakNaturalPulseRate * engine.Haptics.PulseScale
	a.log.Debug().
		Str("engine_layout", a.vehicle.Engine.Layout).
		Str("resolved_engine", a.vehicle.Engine.DBEntry).
		Float32("crank_plane_angle", crankPlaneAngle).
		Float32("cylinder_bank_angle", bankAngle).
		Uint16("rev_limit", a.vehicle.RevLimit).
		Float64("peak_natural_pulse_rate", peakNaturalPulseRate).
		Float64("pulse_scale", a.vehicle.Engine.Haptics.PulseScale).
		Float64("peak_pulse_rate", peakPulseRate).
		Float64("pulse_overlap", a.vehicle.Engine.PulseOverlap).
		Msg("engine characteristics")

	a.log.Info().
		Uint32("ID", a.vehicle.ID).
		Str("manufacturer", a.gtClient.Telemetry.VehicleManufacturer()).
		Str("model", a.gtClient.Telemetry.VehicleModel()).
		Str("type", string(a.vehicle.VehicleType)).
		Str("engine", a.vehicle.Engine.DBEntry).
		Str("wheelbase", fmt.Sprintf("%.2f m", a.vehicle.Dimensions.WheelbaseMeters)).
		Str("track_width", fmt.Sprintf("%.2f m", a.vehicle.Dimensions.TrackWidthMeters)).
		Msg("vehicle update")
}

// gearHasChanged checks if the gear has changed based on telemetry data.
func (a *App) gearHasChanged() bool {
	// ignore gear change events from initial unset state
	if a.kinematics.Current.TransmissionGear == kinematics.NullGear ||
		a.kinematics.Last.TransmissionGear == kinematics.NullGear {
		return false
	}

	if a.kinematics.Current.TransmissionGear == a.kinematics.Last.TransmissionGear {
		return false
	}

	return true
}

// raceComplete returns true 5 seconds after the race has completed.
func (a *App) raceHasFinished() bool {
	currentTime := a.gtClient.Telemetry.TimeOfDay()

	return a.state.raceCompleteTime > currentTime+5*time.Second
}
