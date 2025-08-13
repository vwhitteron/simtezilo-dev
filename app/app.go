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
	"github.com/vwhitteron/simtezilo-dev/app/synth"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

type vehicleRecord struct {
	vehicleID    uint32
	engineLayout string
}

type stateRecord struct {
	seq       uint32
	seqDelta  uint32
	timeOfDay time.Duration
	gear      int
	vehicle   vehicleRecord
}

type appState struct {
	hapticsEnabled      bool // TODO: move state to haptics?
	telemetryActive     bool
	current             stateRecord
	last                stateRecord
	lastKnownRPM        float64   // Cache last known RPM for fallback
	lastRPMTime         time.Time // Timestamp of last known RPM
	enginePulsePolarity bool      // Alternating polarity for engine pulses
}

type App struct {
	log    zerolog.Logger
	config *config.Config
	done   chan bool

	ui *ui.UserInterface

	i18n    *i18n.Language
	display hardware.Display

	gtClient   *telemetry_client.GTClient
	kinematics kinematics.KinematicsTracker
	synth      *synth.Synthesizer

	transmissionGainMin float64

	state appState

	telemetryChartFeed chan map[string]float32
	webEnabled         bool
	webUI              *webui.WebUI
	webSequenceId      uint32
}

type AppOptions struct {
	Done       chan bool
	Logger     *zerolog.Logger
	WebEnabled bool
}

func NewApp(opts AppOptions) (*App, error) {
	a := &App{
		log:  opts.Logger.With().Str("component", "app").Logger(),
		done: opts.Done,
		state: appState{
			current: stateRecord{
				gear: kinematics.NullGear,
			},
			last: stateRecord{
				gear: kinematics.NullGear,
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
	configLogLevel, err := zerolog.ParseLevel(a.config.App.LogLevel)
	if err != nil {
		a.log.Error().Str("config value", a.config.App.LogLevel).Msg("invalid log level")
	}
	if configLogLevel < a.log.GetLevel() || configLogLevel >= zerolog.NoLevel {
		a.log = a.log.Level(configLogLevel).With().Logger()

		a.log.Debug().Str("level", configLogLevel.String()).Str("source", "config").Msg("log level update")
	}

	// load language translations
	a.i18n = i18n.NewLanguage(
		&a.config.App.Language,
		a.log,
	)
	a.log.Debug().Str("language", a.i18n.Code).Str("result", "success").Msg("init language")

	hidEvents := make(chan ui.HIDInputEvent, 10)

	// initialise display and button hardware
	switch a.config.Hardware.Model {
	case "pirateaudio":
		hardware.Init()

		a.display, err = pirateaudio.NewDisplay(pirateaudio.DisplayOptions{
			Orientation: a.config.Hardware.DisplayOrientation,
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

		pirateaudio.SetupHID(hidEvents)
		a.log.Debug().
			Str("component", "pirate audio").
			Str("sub", "hid").
			Msg("init")
	case "spotpear":
		hardware.Init()

		a.display, err = spotpear.NewDisplay(spotpear.DisplayOptions{
			Orientation: a.config.Hardware.DisplayOrientation,
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

		a.display, err = waveshare.NewDisplay(waveshare.DisplayOptions{
			Orientation: a.config.Hardware.DisplayOrientation,
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

		waveshare.SetupHID(hidEvents)
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

	// initialise synthesizer
	a.synth, err = synth.NewSynth(synth.SynthOpts{
		Config:     a.config.Synthesizer,
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
	a.gtClient, err = telemetry_client.NewGTClient(telemetry_client.GTClientOpts{
		Source:   a.config.Telemetry.Source,
		Logger:   &a.log,
		LogLevel: a.config.App.LogLevel,
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

	a.log.Debug().
		Str("component", "app").
		Str("result", "success").
		Msg("init")

	return a, nil
}

func (a *App) Run() {
	go a.ui.HIDEventHandler()

	go a.gtClient.Run()

	if a.webEnabled {
		a.webUI = webui.NewWebUI(a.log, a.telemetryChartFeed)
		go a.webUI.Start()
	}

	chassisStreamer := synth.NewBumpStream(a.synth)
	err := speaker.Init(
		beep.SampleRate(a.synth.GetSampleRate()),
		a.synth.GetSampleRate()/15,
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

	go speaker.Play(chassisStreamer)

	ticker120fps := time.NewTicker((1000 / 120) * time.Millisecond)
	ticker60fps := time.NewTicker((1000 / 60) * time.Millisecond)
	ticker15fps := time.NewTicker((1000 / 15) * time.Millisecond)

	a.log.Debug().Str("component", "app").Str("result", "success").Msg("main loop started")

	for {
		select {
		case <-a.done:
			return
		case <-ticker120fps.C:
			a.hapticEvents()
		case <-ticker60fps.C:
			a.sessionIsComplete()
			a.sendTelemetryChartData()
		case <-ticker15fps.C:
			a.ui.UpdateDisplay(ui.LiveData{
				Gear:            a.kinematics.Current.TransmissionGear,
				TelemetryActive: a.state.telemetryActive,
			})
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
		a.state.current.gear = kinematics.NullGear
		a.resetState()
		a.log.Debug().Msg("session finished")
		a.done <- true

		return true
	}

	return false
}

func (a *App) sessionHasReset() bool {
	if a.gtClient.Telemetry.Flags().Loading {
		a.log.Debug().
			Uint32("sequence_id", a.state.current.seq).
			Msg("loading flag detected")

		return true
	}

	return false
}

func (a *App) resetState() {
	a.state.last = a.state.current

	a.synth.Silence()

	a.kinematics = kinematics.NewKinematicsTracker()

	a.synth.ClearBuffer()
}

func (a *App) updateVehicle() {
	vehicleType := a.gtClient.Telemetry.VehicleType()

	a.log.Debug().Uint32("ID", a.state.last.vehicle.vehicleID).Msg("vehicle ID changed")

	a.log.Info().
		Uint32("ID", a.state.last.vehicle.vehicleID).
		Str("manufacturer", a.gtClient.Telemetry.VehicleManufacturer()).
		Str("model", a.gtClient.Telemetry.VehicleModel()).
		Str("type", vehicleType).
		// TODO: Uncomment when gt-telemetry is updated
		Str("engine_layout", a.gtClient.Telemetry.VehicleEngineLayout()).
		Msg("vehicle update")

	switch vehicleType {
	case "race":
		a.transmissionGainMin = a.config.Synthesizer.TransmissionGain + a.config.Synthesizer.TransmissionGainMinRace
	default:
		a.transmissionGainMin = a.config.Synthesizer.TransmissionGain + a.config.Synthesizer.TransmissionGainMinStreet
	}

	a.state.last.vehicle.vehicleID = a.state.current.vehicle.vehicleID
	a.state.last.vehicle.engineLayout = a.state.current.vehicle.engineLayout
	a.state.last.gear = a.state.current.gear
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
