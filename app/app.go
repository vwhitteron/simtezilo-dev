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
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/spotpear"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/terminal"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

type stateRecord struct {
	seq       uint32
	seqDelta  uint32
	timeOfDay time.Duration
	vehicleID uint32
	gear      int
}

type appState struct {
	lastActive     time.Time
	startTime      time.Time
	hapticsEnabled bool
	current        stateRecord
	last           stateRecord
}

type App struct {
	log    zerolog.Logger
	config *config.Config
	done   chan bool

	i18n       *i18n.Language
	display    display
	hidEvents  chan ui.HIDInputEvent
	menuSystem *ui.MenuSystem

	gtClient   *telemetry_client.GTClient
	kinematics kinematics.KinematicsTracker
	synth      *synth.Synthesizer

	gearVolumeMin float64

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
			startTime:  time.Now(),
			lastActive: time.Now(),
			current: stateRecord{
				gear: NullGear,
			},
			last: stateRecord{
				gear: NullGear,
			},
		},
		hidEvents:          make(chan ui.HIDInputEvent, 10),
		menuSystem:         ui.NewMenuSystem(),
		kinematics:         kinematics.NewKinematicsTracker(),
		telemetryChartFeed: make(chan map[string]float32, 600),
		webEnabled:         opts.WebEnabled,
	}

	// load config from file
	a.config = config.NewConfig("simtezilo.conf", a.log)

	// update to configured log level when greater than current
	configLogLevel, err := zerolog.ParseLevel(a.config.App.LogLevel)
	if err != nil {
		a.log.Error().Str("config value", a.config.App.LogLevel).Msg("invalid log level")
	}
	if configLogLevel < a.log.GetLevel() {
		zerolog.SetGlobalLevel(configLogLevel)

		a.log.Debug().Str("level", configLogLevel.String()).Str("source", "config").Msg("log level update")
	}

	// load language translations
	a.i18n = i18n.NewLanguage(
		a.config.App.Language,
		log.With().Str("component", "i18n").Logger(),
	)
	a.log.Debug().Str("language", a.i18n.Code).Str("result", "success").Msg("init language")

	// initialise synthesizer
	a.synth, err = synth.NewSynth(synth.SynthOpts{
		Config:     a.config.Synthesizer,
		Logger:     log.With().Str("component", "synth").Logger(),
		Kinematics: &a.kinematics,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "synth").
			Str("result", "failure").
			Msg("init")

		return nil, err
	}

	// initialise display and button hardware
	switch a.config.Hardware.Model {
	case "pirateaudio":
		hardware.Init()

		a.display.device, err = pirateaudio.NewDisplay(pirateaudio.DisplayOptions{
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

		pirateaudio.SetupHID(a.hidEvents)
		a.log.Debug().
			Str("component", "pirate audio").
			Str("sub", "hid").
			Msg("init")
	case "spotpear":
		hardware.Init()

		a.display.device, err = spotpear.NewDisplay(spotpear.DisplayOptions{
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

		spotpear.SetupHID(a.hidEvents)
		log.Debug().
			Str("component", "spotpear game 1.3").
			Str("sub", "hid").
			Msg("init")
	case "waveshare":
		hardware.Init()

		a.display.device, err = waveshare.NewDisplay(waveshare.DisplayOptions{
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

		waveshare.SetupHID(a.hidEvents)
		log.Debug().
			Str("component", "waveshare 14972").
			Str("sub", "hid").
			Msg("init")
	default:
		a.display.device = terminal.NewHeadlessDisplay()
		a.log.Debug().
			Str("component", "headless").
			Str("sub", "display").
			Str("result", "success").
			Msg("init")

		go terminal.SetupNullDeviceButtons(a.hidEvents)
		a.log.Debug().
			Str("component", "headless").
			Str("sub", "hid").
			Msg("init")
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
		a.display.device.PowerOn()
		a.display.device.Show("error")

		return nil, err
	}

	a.drawStartupDisplay(Version)

	a.log.Debug().
		Str("component", "app").
		Str("result", "success").
		Msg("init")

	return a, nil
}

func (a *App) Run() {
	go a.hidEventHandler()

	go a.gtClient.Run()

	if a.webEnabled {
		a.webUI = webui.NewWebUI(a.log, a.telemetryChartFeed)
		go a.webUI.Start()
	}

	chassisStreamer := synth.NewBumpStream(a.synth)
	speaker.Init(
		beep.SampleRate(a.synth.GetSampleRate()),
		a.synth.GetSampleRate()/15,
	)
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
			a.updateDisplay()
		}
	}
}

func (a *App) Close() {
	a.log.Info().Msg("shutting down app")

	err := a.synth.Close()
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "audio output device").
			Str("result", "failure").
			Msg("close")
	}

	a.drawStartupDisplay(a.i18n.GetString("ui.quit"))
	time.Sleep(1 * time.Second)
	a.display.device.Close()
}

func (a *App) sessionIsComplete() bool {
	if a.gtClient.Finished {
		a.state.current.gear = NullGear
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

	a.log.Debug().Uint32("ID", a.state.last.vehicleID).Msg("vehicle ID changed")

	a.log.Debug().
		Uint32("ID", a.state.last.vehicleID).
		Str("manufacturer", a.gtClient.Telemetry.VehicleManufacturer()).
		Str("model", a.gtClient.Telemetry.VehicleModel()).
		Str("type", vehicleType).
		Msg("vehicle updated")

	fmt.Printf("Vehicle: %s %s [Type: %s ID: %-4d]\r\n",
		a.gtClient.Telemetry.VehicleManufacturer(),
		a.gtClient.Telemetry.VehicleModel(),
		vehicleType,
		a.state.last.vehicleID,
	)

	switch vehicleType {
	case "race":
		a.gearVolumeMin = float64(a.config.Synthesizer.GearShiftVolumeMinRace) / 100
	default:
		a.gearVolumeMin = float64(a.config.Synthesizer.GearShiftVolumeMinStreet) / 100
	}

	a.state.last.vehicleID = a.state.current.vehicleID
	a.state.last.gear = a.state.current.gear
}

func (a *App) gearHasChanged() bool {
	// ignore gear change events from initial unset state
	if a.kinematics.Current.TransmissionGear == NullGear ||
		a.kinematics.Last.TransmissionGear == NullGear {
		return false
	}

	if a.kinematics.Current.TransmissionGear == a.kinematics.Last.TransmissionGear {
		return false
	}

	return true
}
