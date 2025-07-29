package app

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/terminal"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

type appState struct {
	seq            uint32
	timeOfDay      time.Duration
	lastActive     time.Time
	startTime      time.Time
	currentGear    int
	lastGear       int
	hapticsEnabled bool
	vehicleID      uint32
}

type App struct {
	log    zerolog.Logger
	config *config.Config
	done   chan bool

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
	LogLevel   string
	WebEnabled bool
}

func NewApp(opts AppOptions) (*App, error) {
	a := &App{
		done: opts.Done,
		state: appState{
			lastActive: time.Now(),
			lastGear:   NullGear,
			startTime:  time.Now(),
		},
		hidEvents:          make(chan ui.HIDInputEvent, 10),
		menuSystem:         ui.NewMenuSystem(),
		kinematics:         kinematics.NewKinematicsTracker(),
		telemetryChartFeed: make(chan map[string]float32, 600),
		webEnabled:         opts.WebEnabled,
	}

	// setup logger based on cli arg or default warn level
	argLogLevel, err := zerolog.ParseLevel(opts.LogLevel)
	if err != nil {
		log.Printf("invalid log level parameter %q, setting level to warn", opts.LogLevel)
		argLogLevel = zerolog.WarnLevel
	}
	a.log = zerolog.New(os.Stderr).With().Timestamp().Logger().Level(argLogLevel)
	a.log.Debug().Str("level", argLogLevel.String()).Str("source", "cli arg").Msg("log level update")

	// load config from file
	a.config = config.NewConfig("simtezilo.conf", a.log)

	// update logger log level if configured and not overridden by cli arg
	if opts.LogLevel == "" {
		configLogLevel, err := zerolog.ParseLevel(a.config.App.LogLevel)
		if err != nil {
			a.log.Error().Str("configured", a.config.App.LogLevel).Str("fallback", argLogLevel.String()).Msg("invalid log level")
		}

		a.log.Debug().Str("level", configLogLevel.String()).Str("source", "config").Msg("log level update")
		a.log.Level(configLogLevel)
	}

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

		a.display.lcdDevice, err = pirateaudio.NewDisplay(
			pirateaudio.PirateAudioLCDOpts{
				Orientation: a.config.Hardware.DisplayOrientation,
			},
		)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", "pirate audio").
				Str("sub", "lcd").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}
		a.log.Debug().
			Str("component", "pirate audio").
			Str("sub", "lcd").
			Str("result", "success").
			Msg("init")

		pirateaudio.SetupHID(a.hidEvents)
		a.log.Debug().
			Str("component", "pirate audio").
			Str("sub", "hid").
			Msg("init")
	case "waveshare":
		hardware.Init()

		a.display.lcdDevice, err = waveshare.NewDisplay(waveshare.LCDOpts{
			Orientation: a.config.Hardware.DisplayOrientation,
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "waveshare 14972").
				Str("sub", "lcd").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}
		a.log.Debug().
			Str("component", "waveshare 14972").
			Str("sub", "lcd").
			Str("result", "success").
			Msg("init")

		waveshare.SetupHID(a.hidEvents)
		log.Debug().
			Str("component", "waveshare 14972").
			Str("sub", "hid").
			Msg("init")
	default:
		a.display.lcdDevice = terminal.NewNullDeviceDisplay()
		a.log.Debug().
			Str("component", "null display").
			Str("sub", "lcd").
			Str("result", "success").
			Msg("init")

		go terminal.SetupNullDeviceButtons(a.hidEvents)
		log.Debug().
			Str("component", "terminal").
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
		a.display.lcdDevice.PowerOn()
		a.display.lcdDevice.Show("error")

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
			a.checkSessionComplete()
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

	a.drawStartupDisplay("Goodbye")
	time.Sleep(1 * time.Second)
	a.display.lcdDevice.Close()
}

func (a *App) hidEventHandler() {
	ready := false
	for key := range a.hidEvents {
		// discard hid events in the first 2 seconds after app start
		if !ready {
			if time.Since((a.state.startTime)) < 2*time.Second {
				continue
			}

			ready = true
		}

		menuPage := a.menuSystem.GetCurrentMenuPage()
		value := ""

		switch key {
		case ui.HIDInputUp:
			menuPage = a.menuSystem.GetCurrentMenuPage()
			value = a.alterSetting(menuPage, "increase")

			log.Debug().
				Str("key", "up").
				Str("action", "increase").
				Str("type", menuPage).
				Str("value", value).
				Msg("HID event")
		case ui.HIDInputDown:
			menuPage = a.menuSystem.GetCurrentMenuPage()
			value = a.alterSetting(menuPage, "decrease")

			log.Debug().
				Str("key", "down").
				Str("action", "decrease").
				Str("type", menuPage).
				Str("value", value).
				Msg("HID event")
		case ui.HIDInputLeft:
			if a.display.lcdDevice.IsPoweredOn() {
				menuPage = a.menuSystem.PreviousMenuPage()
			}
			value = a.alterSetting(menuPage, "get")

			log.Debug().
				Str("key", "left").
				Str("action", "previous").
				Str("type", "menuPage").
				Str("value", menuPage).
				Msg("HID event")
		case ui.HIDInputRight:
			if a.display.lcdDevice.IsPoweredOn() {
				menuPage = a.menuSystem.NextMenuPage()
			}
			value = a.alterSetting(menuPage, "get")

			log.Debug().
				Str("key", "left").
				Str("action", "previous").
				Str("type", "menuPage").
				Str("value", menuPage).
				Msg("HID event")
		case ui.HIDInputEscape:
			a.log.Debug().Str("key", "escape").Msg("HID event")
			a.done <- true
		case ui.HIDInputPower:
			backlightState := a.display.lcdDevice.PowerToggle()
			a.drawSettingsDisplay("", backlightState)

			log.Debug().
				Str("key", "power").
				Str("action", "toggle").
				Str("type", "backlight").
				Bool("value", backlightState).
				Msg("HID event")

			continue
		default:
			a.log.Debug().Msgf("HID Input: Unknown (%d)", key)

			continue
		}

		displayContent := value

		if menuPage != "vol" {
			displayContent = menuPage + " " + displayContent
		}

		a.drawSettingsDisplay(displayContent, true)
	}
}

func (a *App) alterSetting(name string, action string) string {
	switch name {
	case "vol":
		value := float64(0)

		switch action {
		case "increase":
			value = a.synth.IncreaseMasterGain()
		case "decrease":
			value = a.synth.DecreaseMasterGain()
		default:
			value = a.synth.GetMasterGain()
		}

		return strconv.FormatFloat(value, 'f', 2, 64) + " dB"
	case "vCurve":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseJerkCurve()
		case "decrease":
			value = a.config.DecreaseJerkCurve()
		default:
			value = int(a.config.GetJerkCurve() * 1000)
		}

		return strconv.Itoa(value)
	case "vMax":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseJerkMax()
		case "decrease":
			value = a.config.DecreaseJerkMax()
		default:
			value = a.config.GetJerkMax()
		}

		return strconv.Itoa(value)
	case "fCurve":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseSnapCurve()
		case "decrease":
			value = a.config.DecreaseSnapCurve()
		default:
			value = int(a.config.GetSnapCurve() * 1000)
		}

		return strconv.Itoa(value)
	case "fMax":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseSnapMax()
		case "decrease":
			value = a.config.DecreaseSnapMax()
		default:
			value = a.config.GetSnapMax()
		}

		return strconv.Itoa(value)
	case "maxHz":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseMaxHz()
		case "decrease":
			value = a.config.DecreaseMaxHz()
		default:
			value = int(a.config.GetMaxHz())
		}

		return strconv.Itoa(value)
	case "minHz":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseMinHz()
		case "decrease":
			value = a.config.DecreaseMinHz()
		default:
			value = int(a.config.GetMinHz())
		}

		return strconv.Itoa(value)
	case "gCurve":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseGearShiftCurve()
		case "decrease":
			value = a.config.DecreaseGearShiftCurve()
		default:
			value = int(a.config.GetGearShiftCurve() * 1000)
		}

		return strconv.Itoa(value)
	case "gMax":
		value := float64(0)

		switch action {
		case "increase":
			value = a.config.IncreaseGearShiftGforceMax()
		case "decrease":
			value = a.config.DecreaseGearShiftGforceMax()
		default:
			value = a.config.GetGearShiftGforceMax()
		}

		return strconv.FormatFloat(value, 'f', 1, 64)
	case "cVol":
		value := 0

		switch action {
		case "increase":
			value, _ = a.synth.IncreaseChannelVolume("chassis")
		case "decrease":
			value, _ = a.synth.DecreaseChannelVolume("chassis")
		default:
			value, _ = a.synth.GetChannelVolume("chassis")
		}

		return strconv.Itoa(value)
	case "gVol":
		value := 0

		switch action {
		case "increase":
			value, _ = a.synth.IncreaseChannelVolume("gearchange")
		case "decrease":
			value, _ = a.synth.DecreaseChannelVolume("gearchange")
		default:
			value, _ = a.synth.GetChannelVolume("gearchange")
		}

		return strconv.Itoa(value)
	case "mix":
		switch action {
		case "increase":
			return a.synth.Mixer.NextAlgorithm()
		case "decrease":
			return a.synth.Mixer.PreviousAlgorithm()
		default:
			return a.synth.Mixer.GetAlgorithm()
		}
	default:
		return "err"
	}
}

func (a *App) hapticEvents() {
	startTime := time.Now()

	seq := a.gtClient.Telemetry.SequenceID()

	// Do nothing until the sequence ID advances
	seqDelta := seq - a.state.seq
	if seq == 0 || seqDelta == 0 {
		return
	}

	currentGear := a.gtClient.Telemetry.CurrentGear()
	currentVehicleID := a.gtClient.Telemetry.VehicleID()

	if a.state.vehicleID != currentVehicleID {
		a.resetState(seq, currentGear)
		a.disableHaptics("vehicle changed")

		go a.updateVehicle(currentVehicleID, currentGear)
		a.log.Debug().Uint32("ID", currentVehicleID).Msg("vehicle ID changed")

		return
	}

	if !a.shouldGenerateHaptics() {
		if a.state.hapticsEnabled {
			a.resetState(seq, currentGear)
			a.disableHaptics("not live")
		}

		return
	}

	// The loading flag typically means the session has restarted
	if a.sessionHasReset(seq) {
		a.resetState(seq, currentGear)
		a.disableHaptics("session reset")

		return
	}

	// Initialise the gear if it hasn't been set yet
	if a.state.lastGear == NullGear {
		a.state.seq = seq
		a.state.lastGear = currentGear

		a.resetState(seq, currentGear)
		a.disableHaptics("initialising gear")

		return
	}

	currentTimeOfDay := a.gtClient.Telemetry.TimeOfDay()
	timeOfDayDelta := currentTimeOfDay - a.state.timeOfDay
	a.state.timeOfDay = currentTimeOfDay
	if timeOfDayDelta < 0 {
		a.state.seq = seq

		a.resetState(seq, currentGear)
		a.disableHaptics("time of day reset")

		a.log.Debug().
			Uint32("sequence_id", seq).
			Str("time_of_day_delta", timeOfDayDelta.String()).
			Msg("time of day reset")

		return
	}

	if !a.state.hapticsEnabled {
		a.enableHaptics()
	}

	a.kinematics.Current.SequenceID = seq

	a.processHaptics(seqDelta)

	a.state.seq = seq
	a.kinematics.Current.ComputeTime = time.Since(startTime)
	a.kinematics.Last = a.kinematics.Current

	if a.kinematics.Current.ComputeTime.Microseconds() > 16000 {
		a.log.Warn().Float64("ms", float64(a.kinematics.Current.ComputeTime.Milliseconds())).Msg("slow compute")
	}

}

func (a *App) processHaptics(seqDelta uint32) {
	// no haptics if sequence ID has not advanced
	if seqDelta < 1 {
		return
	}

	windowMilliseconds := (float64(seqDelta) / frameRate)

	a.kinematics.Update(windowMilliseconds, a.gtClient)

	// no haptics if telemetry packets dropped/missed
	if seqDelta > 1 {
		a.log.Debug().Uint32("delta", seqDelta).Msg("missed packets")
	}

	if a.hasGearChanged() {
		a.playGearChangeHaptic()
	}

	a.generateBump()
}

func (a *App) generateBump() {
	startTime := time.Now()

	snap := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Snap, (a.kinematics.Current.SixDOFRotation.Snap * 100))

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, a.config.GetSnapCurve()))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, a.config.GetSnapScale())
	pulseFrequencyHz := a.config.GetFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < a.config.GetMinHz() {
		pulseFrequencyHz = a.config.GetMinHz()
	} else if pulseFrequencyHz > a.config.GetMaxHz() {
		pulseFrequencyHz = a.config.GetMaxHz()
	}

	pulseWidth := math.Round(float64(a.config.Synthesizer.SampleRateHz) / (2 * pulseFrequencyHz))

	sig := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Jerk, (a.kinematics.Current.SixDOFRotation.Jerk * 100))
	pulseAmplitude := signal.Exponent(sig, a.config.GetJerkCurve())
	pulseAmplitude = signal.Scale(pulseAmplitude, a.config.GetJerkScale())

	p1 := pulseAmplitude
	pulseAmplitude, wasLimited := signal.LimitMax(pulseAmplitude, a.config.Synthesizer.PulseMaxAmplitude)
	if wasLimited {
		a.log.Debug().Float64("pulse", p1).Msg("limiter")
	}

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	bufferLen := a.synth.GetBufferLength()
	pulseBuffer := make([]float64, bufferLen)
	for i := range int(pulseWidth * 2) {
		phase := waveSamplePeriod * (float64(i) - waveOffset)
		pulseBuffer[i] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
	}

	// no haptics when vehicle comes to a controlled stop
	// TODO: check angular velocity, etc to enable for uncontrolled stops
	// if vector.Magnitude(c.kinematics.Current.Velocity.Vector) >= 0.28 {
	lastMag := vector.Magnitude(a.kinematics.Last.SixDOFTranslationCalc.Velocity)
	currentMag := vector.Magnitude(a.kinematics.Current.SixDOFTranslationCalc.Velocity)
	if signal.LargestMagnitude(lastMag, currentMag) >= 0.28 {
		a.synth.WriteBuffer("chassis", pulseBuffer)
	}

	a.kinematics.Last.SynthOutputAmplitude = a.kinematics.Current.SynthOutputAmplitude
	a.kinematics.Current.SynthOutputAmplitude = pulseAmplitude

	a.kinematics.Last.SynthOutputFrequency = a.kinematics.Current.SynthOutputFrequency
	a.kinematics.Current.SynthOutputFrequency = int(pulseFrequencyHz)

	if pulseAmplitude > 1.0 || pulseAmplitude < -1.0 {
		a.log.Debug().
			Float64("jerk", a.kinematics.Current.SixDOFTranslationCalc.Jerk).
			Float64("snap", a.kinematics.Current.SixDOFTranslationCalc.Snap).
			Str("process_time", time.Since(startTime).String()).
			Uint32("sequence_id", a.kinematics.Current.SequenceID).
			Msg("Bump inputs")
		a.log.Debug().
			Float64("amplitude", pulseAmplitude).
			Float64("samplePeriod", waveSamplePeriod).
			Float64("pulseWidth", pulseWidth).
			Msg("Bump outputs")
	}
}

func (a *App) sessionHasReset(seq uint32) bool {
	if a.gtClient.Telemetry.Flags().Loading {
		a.log.Debug().
			Uint32("sequence_id", seq).
			Msg("loading flag detected")

		return true
	}

	return false
}

func (a *App) resetState(seq uint32, currentGear int) {
	a.state.timeOfDay = a.gtClient.Telemetry.TimeOfDay()
	a.state.seq = seq
	a.state.lastGear = currentGear

	a.synth.Silence()

	a.kinematics = kinematics.NewKinematicsTracker()

	a.synth.ClearBuffer()
}

func (a *App) disableHaptics(reason string) {
	// speaker.Suspend()
	a.synth.Silence()
	a.synth.ClearBuffer()
	a.state.hapticsEnabled = false

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Str("reason", reason).Msg("haptics state change")
}

func (a *App) enableHaptics() {
	// speaker.Resume()
	a.synth.FadeIn(3 * time.Second)
	a.state.hapticsEnabled = true

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Msg("haptics state change")
}

func (a *App) shouldGenerateHaptics() bool {
	if a.gtClient.Telemetry.Flags().GamePaused {
		return false
	}

	if !a.gtClient.Telemetry.Flags().Live && !a.config.App.ReplayMode {
		return false
	}

	return true
}

func (a *App) updateVehicle(currentVehicleID uint32, currentGear int) {
	vehicleType := a.gtClient.Telemetry.VehicleType()
	a.state.vehicleID = currentVehicleID

	a.log.Debug().
		Uint32("ID", currentVehicleID).
		Str("manufacturer", a.gtClient.Telemetry.VehicleManufacturer()).
		Str("model", a.gtClient.Telemetry.VehicleModel()).
		Str("type", vehicleType).
		Msg("vehicle updated")

	fmt.Printf("Vehicle: %s %s [Type: %s ID: %-4d]\r\n",
		a.gtClient.Telemetry.VehicleManufacturer(),
		a.gtClient.Telemetry.VehicleModel(),
		vehicleType,
		currentVehicleID,
	)

	switch vehicleType {
	case "race":
		a.gearVolumeMin = float64(a.config.Synthesizer.GearShiftVolumeMinRace) / 100
	default:
		a.gearVolumeMin = float64(a.config.Synthesizer.GearShiftVolumeMinStreet) / 100
	}

	a.state.lastGear = currentGear
}

func (a *App) checkSessionComplete() {
	if a.gtClient.Finished {
		a.resetState(0, NullGear)
		a.log.Debug().Msg("session finished")
		a.done <- true
	}
}

func (a *App) hasGearChanged() bool {
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

func (a *App) playGearChangeHaptic() {
	volumePercent := a.determineGearChangeVolume()

	a.synth.PlayEffectWithVolume("gearchange", volumePercent)
	a.log.Debug().
		Int("sequence_id", int(a.state.seq)).
		Int("volume_pc", volumePercent).
		Int("gear", a.kinematics.Current.TransmissionGear).
		Msg("gear change")
}

func (a *App) determineGearChangeVolume() int {
	volumeMaxPercent, _ := a.synth.GetChannelVolume("gearchange")

	if volumeMaxPercent >= 100 || a.config.DynamicGearShiftFeedbackEnabled() {
		return 100
	}

	gForce := a.kinematics.GetSurgeGforce()
	gforceMax := a.config.GetGearShiftGforceMax()
	volumeCurve := a.config.GetGearShiftCurve()

	volumeMax := float64(volumeMaxPercent) / 100.0
	gearChangeVolume := (math.Pow((gForce/gforceMax), volumeCurve) * volumeMax)
	gearChangeVolume, _ = signal.LimitWindow(gearChangeVolume, a.gearVolumeMin, volumeMax)

	return int(gearChangeVolume * 100.0)
}

func (a *App) sendTelemetryChartData() {
	if a.webUI == nil {
		return
	}

	if !a.webUI.HasActiveClients() {
		return
	}

	if a.gtClient.Telemetry.Flags().GamePaused {
		return
	}

	if a.gtClient.Finished {
		return
	}

	if a.kinematics.Current.SequenceID == a.webSequenceId {
		return
	}

	a.webSequenceId = a.kinematics.Current.SequenceID

	go func() {
		a.telemetryChartFeed <- map[string]float32{
			"computeTime":                 float32(a.kinematics.Last.ComputeTime.Microseconds()),
			"seq":                         float32(a.state.seq),
			"timeOfDay":                   float32(a.gtClient.Telemetry.TimeOfDay().Milliseconds()),
			"throttleInput":               a.gtClient.Telemetry.ThrottleInputPercent(),
			"throttleOutput":              a.gtClient.Telemetry.ThrottleOutputPercent(),
			"brakeInput":                  a.gtClient.Telemetry.BrakeInputPercent(),
			"brakeOutput":                 a.gtClient.Telemetry.BrakeOutputPercent(),
			"rpm":                         a.gtClient.Telemetry.EngineRPM(),
			"speed":                       a.gtClient.Telemetry.GroundSpeedKPH(),
			"gear":                        float32(a.kinematics.Current.TransmissionGear),
			"surgeGforce":                 float32(a.kinematics.Current.SixDOFTranslation.Acceleration) / kinematics.GravityConstant,
			"surgeGforceCalc":             float32(a.kinematics.Current.SurgeCalculated) / kinematics.GravityConstant,
			"SixDOFTranslationalJerk":     float32(a.kinematics.Current.SixDOFTranslation.Jerk),
			"SixDOFTranslationalSnap":     float32(a.kinematics.Current.SixDOFTranslation.Snap),
			"SixDOFTranslationalJerkCalc": float32(a.kinematics.Current.SixDOFTranslationCalc.Jerk),
			"SixDOFTranslationalSnapCalc": float32(a.kinematics.Current.SixDOFTranslationCalc.Snap),
			"SixDOFRotationalJerk":        float32(a.kinematics.Current.SixDOFRotation.Jerk), // * 50),
			"SixDOFRotationalSnap":        float32(a.kinematics.Current.SixDOFRotation.Snap),
			"synthOutputAmplitude":        float32(signal.Abs(float64(a.kinematics.Current.SynthOutputAmplitude))),
			"synthOutputFrequency":        float32(a.kinematics.Current.SynthOutputFrequency),
		}
	}()
}
