package app

import (
	"fmt"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/cache"
	"github.com/vwhitteron/simtezilo-dev/app/circuit"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/fuelrange"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/console"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/spotpear"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/translations"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/odometer"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
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

type vehicleTypeName string

const (
	vehicleTypeStreet vehicleTypeName = "street"
	vehicleTypeTuned  vehicleTypeName = "tuned"
	vehicleTypeRace   vehicleTypeName = "race"
)

// vehicleRecord holds static vehicle data loaded from the GT vehicle database.
type vehicleRecord struct {
	ID          uint32                // Unique vehicle ID from telemetry
	vehicleType vehicleTypeName       // Vehicle type
	engine      engineCharacteristics // Engine characteristics
	revLimit    uint16                // Engine rev limit in RPM
}

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
	hapticsEnabled   bool          // Flag to indicate if haptics are enabled // TODO: move state to haptics?
	telemetryActive  bool          // Flag to indicate if telemetry is active
	raceCompleteTime time.Duration // Time of day when the race was completed
	current          raceState     // Race state at the current telemetry sequence
	last             raceState     // Race state at the last telemetry sequence
	engine           engineState   // Engine state for haptic generation
}

// App is the main application struct holding all components and state.
type App struct {
	log    zerolog.Logger // Application logger
	config *config.Config // Application configuration
	done   chan bool      // Channel to signal application shutdown

	cache cache.Cache // Cache manager

	ui *ui.UserInterface // User interface manager

	i18n    *i18n.Language   // Language translations
	display hardware.Display // Hardware display interface

	gtClient   *gttelemetry.Client      // GT telemetry client
	pitRadio   pitradio.PitRadio        // Pit radio notification service
	kinematics kinematics.State         // Vehicle kinematics tracker
	synth      *synthesizer.Synthesizer // Audio synthesizer for haptic feedback

	odometer  *odometer.Odometer   // Odometer for distance tracking
	fuelRange *fuelrange.FuelRange // Fuel range estimator
	circuit   *circuit.Circuit     // Circuit information and tracking

	transmissionGainMin float64 // Minimum transmission gain based on vehicle type

	state         appState       // Application state tracker
	pitRadioState *pitRadioState // Current pit radio state
	vehicle       vehicleRecord  // Current vehicle information

	telemetryChartFeed chan map[string]float32 // Channel for sending telemetry data to web UI
	webEnabled         bool                    // Flag to enable or disable the web UI
	webUI              *webui.WebUI            // Web UI server and handler
	webSequenceID      uint32                  // Last sequence ID sent to the web UI

	lapStartEvents chan uint32 // Channel for notifying new lap starts
}

// Options holds configuration options for initializing the App.
type Options struct {
	VehicleDB  string          // Path to an external vehicle database file
	Done       chan bool       // Channel to signal application shutdown
	Logger     *zerolog.Logger // Logger instance for application logging
	WebEnabled bool            // Flag to enable or disable the web UI
}

// New creates a new App instance and sets up all components based on the provided options.
func New(opts Options) (*App, error) {
	app := &App{
		log:  opts.Logger.With().Str("package", "app").Logger(),
		done: opts.Done,
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
		webEnabled:         opts.WebEnabled,
		lapStartEvents:     make(chan uint32),
	}

	// load config from file
	app.config = config.New("simtezilo.conf", *opts.Logger)

	zerolog.FloatingPointPrecision = 5

	// update to configured log level when greater than current
	configLogLevel, err := zerolog.ParseLevel(app.config.GetAppLogLevel())
	if err != nil {
		app.log.Error().Int("config value", int(configLogLevel)).Msg("invalid log level")
	}

	if configLogLevel < app.log.GetLevel() || configLogLevel >= zerolog.NoLevel {
		app.log = app.log.Level(configLogLevel).With().Logger()

		app.log.Debug().Str("level", configLogLevel.String()).Str("source", "config").Msg("log level update")
	}

	app.cache = cache.New(app.config.GetAppCacheDir(), *opts.Logger)

	// load language translations
	app.i18n = i18n.NewLanguage(
		app.config.GetAppLanguage(),
		*opts.Logger,
	)
	app.log.Debug().Str("language", app.i18n.Code).Str("result", "success").Msg("init language")

	hidEvents := make(chan ui.HIDInputEvent, 10)

	// initialise display and button hardware
	switch app.config.GetHardwareModel() {
	case "pirateaudio":
		hardware.Init()

		orientation := app.config.GetDisplayOrientation()

		app.display, err = pirateaudio.NewDisplay(pirateaudio.DisplayOptions{
			Orientation: orientation,
			I18n:        app.i18n,
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("component", "pirate audio").
				Str("sub", "display").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}

		app.log.Debug().
			Str("component", "pirate audio").
			Str("sub", "display").
			Str("result", "success").
			Msg("init")

		pirateaudio.SetupHID(orientation, hidEvents)
		app.log.Debug().
			Str("component", "pirate audio").
			Str("sub", "hid").
			Msg("init")
	case "spotpear":
		hardware.Init()

		app.display, err = spotpear.NewDisplay(spotpear.DisplayOptions{
			Orientation: app.config.GetDisplayOrientation(),
			I18n:        app.i18n,
		})
		if err != nil {
			app.log.Error().
				Err(err).
				Str("component", "spotpear game 1.3").
				Str("sub", "display").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}

		app.log.Debug().
			Str("component", "spotpear game 1.3").
			Str("sub", "display").
			Str("result", "success").
			Msg("init")

		spotpear.SetupHID(hidEvents)
		log.Debug().
			Str("component", "spotpear game 1.3").
			Str("sub", "hid").
			Msg("init")
	case "waveshare":
		hardware.Init()

		orientation := app.config.GetDisplayOrientation()

		app.display, err = waveshare.NewDisplay(waveshare.DisplayOptions{
			Orientation: orientation,
			I18n:        app.i18n,
		})
		if err != nil {
			app.log.Error().
				Err(err).
				Str("component", "waveshare 14972").
				Str("sub", "display").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}

		app.log.Debug().
			Str("component", "waveshare 14972").
			Str("sub", "display").
			Str("result", "success").
			Msg("init")

		waveshare.SetupHID(orientation, hidEvents)
		log.Debug().
			Str("component", "waveshare 14972").
			Str("sub", "hid").
			Msg("init")
	default:
		app.display = console.New()
		app.log.Debug().
			Str("component", "console").
			Str("sub", "display").
			Str("result", "success").
			Msg("init")

		go console.SetupHID(hidEvents)

		app.log.Debug().
			Str("component", "console").
			Str("sub", "hid").
			Msg("init")
	}

	app.ui = ui.NewUserInterface(&ui.Config{
		I18n:             app.i18n,
		HIDEvents:        hidEvents,
		Display:          app.display,
		LiveData:         &ui.LiveData{Gear: kinematics.NullGear},
		Log:              *opts.Logger,
		SettingsCallback: app.settingAction,
		Done:             app.done,
	})

	err = app.ui.Screen.RenderSplashScreen(Version)
	if err != nil {
		app.log.Error().
			Err(err).
			Str("component", "ui").
			Str("sub", "screen").
			Str("result", "failure").
			Msg("render splash screen")

		return nil, fmt.Errorf("render splash screen: %w", err)
	}

	// initialise synthesizer
	app.synth, err = synthesizer.New(&synthesizer.SynthOpts{
		Config:     app.config.GetSynthesizer(),
		Logger:     *opts.Logger,
		Kinematics: &app.kinematics,
	})
	if err != nil {
		app.log.Error().
			Err(err).
			Str("component", "synth").
			Str("result", "failure").
			Msg("init")

		_ = app.ui.Screen.RenderErrorScreen("Synth init")

		return nil, err
	}

	// initialise GT telemetry client
	gtClientLogger := opts.Logger.With().Str("component", "gt client").Logger()

	app.gtClient, err = gttelemetry.New(gttelemetry.Options{
		Source:    app.config.GetTelemetrySource(),
		Logger:    &gtClientLogger,
		LogLevel:  app.config.GetAppLogLevel(),
		VehicleDB: opts.VehicleDB,
	})
	if err != nil {
		app.log.Error().
			Err(err).
			Str("component", "gt client").
			Str("result", "failure").
			Msg("init")

		_ = app.ui.Screen.RenderErrorScreen("GT client init")

		return nil, err
	}

	app.odometer = odometer.New(*opts.Logger)

	app.fuelRange = fuelrange.New(*opts.Logger)

	app.circuit, err = circuit.New(*app.gtClient.CircuitDB, *opts.Logger)
	if err != nil {
		// TODO: fatal error?
		app.log.Error().
			Err(err).
			Str("package", "circuit").
			Str("result", "failure").
			Msg("init")
	}

	discordBotConfig := pitradio.DiscordOptions{
		Token:          app.config.GetDiscordToken(),
		ChannelID:      app.config.GetDiscordChannelID(),
		VoiceChannelID: app.config.GetDiscordVoiceChannelID(),
		GuildID:        app.config.GetDiscordGuildID(),
		Cache:          &app.cache,
		Logger:         *opts.Logger,
	}

	app.pitRadio, err = pitradio.NewDiscordBot(discordBotConfig)
	if err != nil {
		app.log.Error().
			Err(err).
			Str("component", "discord").
			Str("result", "failure").
			Msg("init")
	}

	app.resetPitRadioState()

	app.log.Debug().
		Str("component", "app").
		Str("result", "success").
		Msg("init")

	return app, nil
}

// Run starts the main application loop and associated goroutines.
func (a *App) Run() {
	a.startBackgroundTasks()

	a.startAudioOutput()

	a.mainLoop()
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

	err = a.pitRadio.Disconnect()
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "discord").
			Str("result", "failure").
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

// startBackgroundTasks launches all necessary background goroutines for the application.
func (a *App) startBackgroundTasks() {
	go a.ui.HIDEventHandler()

	go a.pitRadio.MessageDispatcher(a.log)

	go a.newLapHandler()

	go func() {
		// retry pit radio connection for up to 5 minutes while network comes up at boot time
		for count := 0; count < 60; count++ {
			err := a.pitRadio.Connect()
			if err == nil {
				break
			}

			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("result", "failure").
				Msg("init")

			time.Sleep(5 * time.Second)
		}

		err := a.pitRadio.Send(pitradio.Message{
			Text:   a.i18n.GetString(translations.RadioOnline),
			Lang:   a.i18n.GetCurrentLanguage(),
			Accent: a.config.GetAppAccent(),
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("result", "failure").
				Msg("send message")
		}

		a.log.Debug().
			Str("component", "discord").
			Str("result", "success").
			Msg("init")
	}()

	go func() {
		for {
			err, recoverable := a.gtClient.Run()
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

	if a.webEnabled {
		a.webUI = webui.New(a.log, a.telemetryChartFeed)
		go a.webUI.Start()
	}
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

	a.log.Debug().Str("component", "app").Str("result", "success").Msg("main loop started")

	for {
		select {
		case <-a.done:
			return
		case <-tickerHaptics.C:
			a.handleGameStateChange()

			stateChanged := a.updateState()

			if a.gtClient.Telemetry.IsInMainMenu() {
				continue
			}

			if stateChanged {
				a.handleVehicleChange()
				a.generateForceHaptics()
			}

			a.checkRaceComplete()
			a.checkForNewLap()

		case <-tickerGeneral.C:
			if a.gtClient.Telemetry.IsOnCircuit() {
				a.updateFuelRange()
			}

			a.updateCircuit()
			a.sendTelemetryChartData()

		case <-tickerEngineHaptics.C:
			if a.gtClient.Telemetry.IsOnCircuit() {
				a.generateEngineHaptic()
			}

		case <-tickerDisplay.C:
			a.ui.UpdateDisplay(ui.LiveData{
				Gear:            a.kinematics.Current.TransmissionGear,
				TelemetryActive: a.state.telemetryActive,
			})

		case <-tickerPitRadio.C:
			if a.gtClient.Telemetry.IsOnCircuit() {
				a.sendPitRadioMessage()
			}
		}
	}
}

func (a *App) newLapHandler() bool {
	a.log.Debug().
		Str("handler", "new lap events").
		Msg("Start")

	for {
		select {
		case <-a.lapStartEvents:
			lap := a.state.current.lapNumber
			coordinate := a.gtClient.Telemetry.PositionalMapCoordinates()
			odometerReading := a.odometer.Add(coordinate)

			didUpdate := a.circuit.UpdateCircuit(odometerReading, lap, coordinate, models.CoordinateTypeStartLine)
			if didUpdate {
				a.odometer.Reset()
				a.fuelRange.Reset()
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

	a.log.Info().Msg("App state reset")
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
	if a.state.current.gameState == a.state.last.gameState {
		return
	}

	switch {
	case a.state.current.gameState == gameStateMainMenu:
		a.disableHaptics("main menu")
		a.resetPitRadioState()
		a.vehicle = vehicleRecord{}
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.circuit.Reset()
		a.resetAppState()

		a.log.Info().Msg("Entered main menu")

	case a.state.current.gameState == gameStateRaceMenu:
		a.disableHaptics("race menu")
		a.resetPitRadioState()
		a.odometer.Reset()
		a.fuelRange.ResetEstimate()
		a.circuit.ResetLapProgress()

		a.log.Info().Msg("Entered race menu")

	case a.state.current.gameState == gameStateOnCircuit:
		a.log.Info().Msg("Vehicle on circuit")

	case a.liveFlagHasChanged():
		a.resetPitRadioState()
		a.vehicle = vehicleRecord{}
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.fuelRange.SetLive(a.state.current.isLive)
		a.circuit.Reset()
		a.resetAppState()

		a.log.Info().Bool("is_live", a.state.current.isLive).Msg("Live flag change")

	case a.timeOfDayHasReset():
		a.disableHaptics("time of day reset")
		a.resetPitRadioState()
		a.odometer.Reset()
		a.fuelRange.Reset()
		a.circuit.ResetLapProgress()

		a.log.Info().Msg("Time of day reset")

	// Assune vehicle pit stop
	case a.gtClient.Telemetry.Flags().Loading:
		a.fuelRange.ResetEstimate()

		a.log.Debug().Msg("Loading flag")
	}

	if a.sessionIsComplete() {
		a.done <- true
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
	var vehicleType vehicleTypeName

	switch a.gtClient.Telemetry.VehicleType() {
	case string(vehicleTypeStreet):
		vehicleType = vehicleTypeStreet
	case string(vehicleTypeTuned):
		vehicleType = vehicleTypeTuned
	case string(vehicleTypeRace):
		vehicleType = vehicleTypeRace
	default:
		vehicleType = vehicleTypeStreet
	}

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

	peakNaturalPulseRate := float64(revLimit) * engine.firingFrequency

	// Heavy pulse rate adjustment for high rev limit engines
	peakPulseRate := peakNaturalPulseRate * engine.haptics.PulseScale
	if peakPulseRate > maxPulseRate {
		engine.haptics.PulseScale = (maxPulseRate / peakPulseRate) * engine.haptics.PulseScale
		peakPulseRate *= engine.haptics.PulseScale
	}

	a.vehicle = vehicleRecord{
		ID:          a.gtClient.Telemetry.VehicleID(),
		vehicleType: vehicleType,
		engine:      engine,
		revLimit:    revLimit,
	}

	// Set default rev limit if not available
	if a.vehicle.revLimit == 0 {
		a.vehicle.revLimit = 8000
	}

	switch vehicleType {
	case vehicleTypeRace, vehicleTypeTuned:
		a.transmissionGainMin = a.config.GetTransmissionGain() + a.config.GetTransmissionGainMinRace()
	case vehicleTypeStreet:
		fallthrough
	default:
		a.transmissionGainMin = a.config.GetTransmissionGain() + a.config.GetTransmissionGainMinStreet()
	}

	a.state.last.transmissionGear = a.state.current.transmissionGear

	a.odometer.Reset()
	a.fuelRange.Reset()

	a.log.Debug().
		Str("engine_layout", a.vehicle.engine.layout).
		Str("resolved_engine", a.vehicle.engine.dbEntry).
		Float32("crank_plane_angle", crankPlaneAngle).
		Float32("cylinder_bank_angle", bankAngle).
		Uint16("rev_limit", a.vehicle.revLimit).
		Float64("peak_natural_pulse_rate", peakNaturalPulseRate).
		Float64("pulse_scale", a.vehicle.engine.haptics.PulseScale).
		Float64("peak_pulse_rate", peakPulseRate).
		Float64("pulse_overlap", a.vehicle.engine.pulseOverlap).
		Msg("engine characteristics")

	a.log.Info().
		Uint32("ID", a.vehicle.ID).
		Str("manufacturer", a.gtClient.Telemetry.VehicleManufacturer()).
		Str("model", a.gtClient.Telemetry.VehicleModel()).
		Str("type", string(vehicleType)).
		Str("engine", a.vehicle.engine.dbEntry).
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
