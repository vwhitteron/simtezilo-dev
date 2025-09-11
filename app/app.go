package app

import (
	"fmt"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/console"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/spotpear"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

// vehicleRecord holds static vehicle data loaded from the GT vehicle database
type vehicleRecord struct {
	ID          uint32
	vehicleType string
	engine      engineCharacteristics
	revLimit    uint16
}

// raceState holds transient race data for haptic generation and pit radio notifications
type raceState struct {
	// Telemetry session information
	sequenceNumber uint32
	sequenceDelta  uint32
	timeOfDay      time.Duration

	// Vehicle information
	currentGear int
	vehicleID   uint32

	// Race timing information
	currentLapNumber int16
	lastLapTime      time.Duration
}

// appState holds the overall application state
type appState struct {
	hapticsEnabled  bool // TODO: move state to haptics?
	telemetryActive bool
	current         raceState
	last            raceState
	engine          engineState
}

// pitRadioState tracks Discord/pit radio communication state
// Handled separately from the main race state to prevent interference due to differences
// in refresh rates
type pitRadioState struct {
	// Last values sent to prevent duplicate messages
	lastNotifiedLapNumber int16
	lastNotifiedLapTime   time.Duration
	lastRaceProgress      int8
	lastNotifiedPosition  int16

	// Current position tracking with debouncing
	currentPosition        int16
	positionNotifyDebounce time.Time

	// Fuel monitoring state
	lastFuelPercent           float32
	fuelUsedPerLap            float32
	averageFuelUsagePerLap    float32
	sampledLaps               int
	lastNotifiedFuelWarning   int16
	fuelNotifyPrewarnComplete bool
	fuelUsageHistory          []float32

	// Lap distance estimation for fuel calculations
	lastLapNumber        int16
	estimatedLapDistance float64
	lastPosition         telemetry_client.Vector
	lapDistance          float64
	distanceTraveled     float64
	isTrackingDistance   bool
}

type App struct {
	log    zerolog.Logger
	config *config.Config
	done   chan bool

	ui *ui.UserInterface

	i18n    *i18n.Language
	display hardware.Display

	gtClient   *telemetry_client.GTClient
	pitRadio   pitradio.PitRadioService
	kinematics kinematics.KinematicsTracker
	synth      *synth.Synthesizer

	transmissionGainMin float64

	state         appState
	pitRadioState *pitRadioState
	vehicle       vehicleRecord

	telemetryChartFeed chan map[string]float32
	webEnabled         bool
	webUI              *webui.WebUI
	webSequenceId      uint32
}

type AppOptions struct {
	VehicleDB  string
	Done       chan bool
	Logger     *zerolog.Logger
	WebEnabled bool
}

func NewApp(opts AppOptions) (*App, error) {
	a := &App{
		log:  opts.Logger.With().Str("component", "app").Logger(),
		done: opts.Done,
		state: appState{
			current: raceState{
				currentGear: kinematics.NullGear,
			},
			last: raceState{
				currentGear: kinematics.NullGear,
			},
		},
		kinematics:         kinematics.NewKinematicsTracker(),
		telemetryChartFeed: make(chan map[string]float32, 600),
		webEnabled:         opts.WebEnabled,
	}

	// load config from file
	a.config = config.NewConfig("simtezilo.conf", a.log)

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

	// load language translations
	a.i18n = i18n.NewLanguage(
		a.config.GetAppLanguage(),
		a.log,
	)
	a.log.Debug().Str("language", a.i18n.Code).Str("result", "success").Msg("init language")

	hidEvents := make(chan ui.HIDInputEvent, 10)

	// initialise display and button hardware
	switch a.config.GetHardwareModel() {
	case "pirateaudio":
		hardware.Init()
		orientation := a.config.GetDisplayOrientation()

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

			return nil, err
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
	case "spotpear":
		hardware.Init()

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

			return nil, err
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
	case "waveshare":
		hardware.Init()
		orientation := a.config.GetDisplayOrientation()

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

			return nil, err
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
	default:
		a.display = console.NewDisplay()
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

	a.ui = ui.NewUserInterface(&ui.Config{
		I18n:             a.i18n,
		HIDEvents:        hidEvents,
		Display:          a.display,
		LiveData:         &ui.LiveData{Gear: kinematics.NullGear},
		Log:              a.log.With().Str("component", "ui").Logger(),
		SettingsCallback: a.settingAction,
		Done:             a.done,
	})

	err = a.ui.Screen.RenderSplashScreen(Version)
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "ui").
			Str("sub", "screen").
			Str("result", "failure").
			Msg("render splash screen")

		return nil, fmt.Errorf("render splash screen: %w", err)
	}

	// initialise synthesizer
	a.synth, err = synth.NewSynthesizer(&synth.SynthOpts{
		Config:     a.config.GetSynthesizer(),
		Logger:     a.log,
		Kinematics: &a.kinematics,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "synth").
			Str("result", "failure").
			Msg("init")

		_ = a.ui.Screen.RenderErrorScreen("Synth init")

		return nil, err
	}

	// initialise GT telemetry client
	gtClientLogger := a.log.With().Str("component", "gt client").Logger()
	a.gtClient, err = telemetry_client.NewGTClient(telemetry_client.GTClientOpts{
		Source:    a.config.GetTelemetrySource(),
		Logger:    &gtClientLogger,
		LogLevel:  a.config.GetAppLogLevel(),
		VehicleDB: opts.VehicleDB,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "gt client").
			Str("result", "failure").
			Msg("init")

		_ = a.ui.Screen.RenderErrorScreen("GT client init")

		return nil, err
	}

	a.pitRadio, err = pitradio.NewDiscordBot(a.config.GetDiscordToken(), a.config.GetDiscordChannelID())
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "discord").
			Str("result", "failure").
			Msg("init")
	}

	a.log.Debug().
		Str("component", "app").
		Str("result", "success").
		Msg("init")

	return a, nil
}

func (a *App) Run() {
	go a.ui.HIDEventHandler()

	go func() {
		err := a.pitRadio.Connect()
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("result", "failure").
				Msg("init")

			return
		}

		a.log.Info().
			Str("component", "discord").
			Str("result", "success").
			Msg("init")

		a.pitRadio.Send("Radio check")
	}()

	go func() {
		for {
			err, recoverable := a.gtClient.Run()
			if err != nil {
				if recoverable {
					a.log.Error().
						Err(err).
						Str("component", "gt client").
						Str("result", "failure").
						Msg("run")

					a.ui.Screen.RenderSplashScreen("GT client error")

					continue
				} else {
					a.ui.Screen.RenderErrorScreen("GT client error")

					a.log.Fatal().
						Err(err).
						Str("component", "gt client").
						Str("result", "failure").
						Msg("run")
				}
			}
		}
	}()

	if a.webEnabled {
		a.webUI = webui.NewWebUI(a.log, a.telemetryChartFeed)
		go a.webUI.Start()
	}

	outputSampleRate := beep.SampleRate(a.config.GetOutputSampleRateHz())
	hapticStreamer := synth.NewHapticStream(a.synth, outputSampleRate)
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
			a.hapticEvents()
		case <-tickerGeneral.C:
			a.sessionIsComplete()
			a.sendTelemetryChartData()
		case <-tickerEngineHaptics.C:
			a.generateEngineHaptic()
		case <-tickerDisplay.C:
			a.ui.UpdateDisplay(ui.LiveData{
				Gear:            a.kinematics.Current.TransmissionGear,
				TelemetryActive: a.state.telemetryActive,
			})
		case <-tickerPitRadio.C:
			a.sendPitRadioMessage()
		}
	}
}

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

	a.pitRadio.Disconnect()

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

func (a *App) sessionIsComplete() bool {
	if a.gtClient.Finished {
		a.state.current.currentGear = kinematics.NullGear
		a.resetState(hardReset)
		a.log.Debug().Msg("session finished")
		a.done <- true

		return true
	}

	return false
}

func (a *App) sessionHasReset() bool {
	if a.gtClient.Telemetry.Flags().Loading {
		a.log.Debug().
			Uint32("sequence_id", a.state.current.sequenceNumber).
			Msg("loading flag detected")

		return true
	}

	return false
}

func (a *App) resetState(resetType int) {
	a.state.last = a.state.current

	switch resetType {
	case hardReset:
		a.resetPitRadioState(true)
	default:
		a.resetPitRadioState(false)
	}

	a.synth.Silence()

	a.kinematics = kinematics.NewKinematicsTracker()

	a.synth.ClearBuffers()
}

func (a *App) updateVehicle() {
	vehicleType := a.gtClient.Telemetry.VehicleType()
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
		Str("type", vehicleType).
		Str("engine", a.vehicle.engine.dbEntry).
		Msg("vehicle update")

	switch vehicleType {
	case "race":
		a.transmissionGainMin = a.config.GetTransmissionGain() + a.config.GetTransmissionGainMinRace()
	default:
		a.transmissionGainMin = a.config.GetTransmissionGain() + a.config.GetTransmissionGainMinStreet()
	}

	a.state.last.vehicleID = a.state.current.vehicleID
	a.state.last.currentGear = a.state.current.currentGear
}

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
