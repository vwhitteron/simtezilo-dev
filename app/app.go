package app

import (
	"fmt"
	"image"
	"math"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/terminal"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

type displayContent struct {
	gear  int
	state string
}

type App struct {
	buttonsFn          func()
	telemetryChartFeed chan map[string]float32
	config             *config.Config
	lcdDevice          hardware.LCD
	displayContent     displayContent
	done               chan bool
	gearVolumeMin      float64
	gt                 *telemetry_client.GTClient
	lastGear           int
	log                zerolog.Logger
	kinematics         kinematics.KinaticsTracker
	replayMode         bool
	seq                uint32
	synth              *synth.Synthesizer
	timeOfDay          time.Duration
	lastActive         *time.Time
	vehicleID          uint32
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
	logLevel, err := zerolog.ParseLevel(opts.LogLevel)
	if err != nil {
		fmt.Printf("invalid log level parameter %q, setting level to warn", opts.LogLevel)
		logLevel = zerolog.WarnLevel
	}

	log := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel)

	config := config.NewConfig("simtezilo.conf", log)

	if opts.LogLevel == "" {
		logLevel, err = zerolog.ParseLevel(config.App.LogLevel)
		if err != nil {
			log.Error().Str("configured", config.App.LogLevel).Str("fallback", "warn").Msg("invalid log level")
		}
	}

	log = log.Level(logLevel)

	log.Info().Str("Level", logLevel.String()).Msg("log level")

	kinematics := kinematics.NewKinematicsTracker()

	synthesizer, err := synth.NewSynth(synth.SynthOpts{
		Config:     config.Synthesizer,
		Logger:     log.With().Str("component", "synth").Logger(),
		Kinematics: &kinematics,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "synth").
			Str("result", "failure").
			Msg("init")

		return nil, err
	}

	lastActive := time.Now()

	var lcdDevice hardware.LCD
	var buttonsFn func()

	switch config.Hardware.Model {
	case "pirateaudio":
		var err error
		lcdDevice, err = pirateaudio.NewPirateAudioLCD(pirateaudio.PirateAudioLCDOpts{
			Orientation: config.Hardware.DisplayOrientation,
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("component", "pirate audio display").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}
		log.Debug().
			Str("component", "pirate audio display").
			Str("result", "success").
			Msg("init")

		lcdDevice.ShowTextCentered(image.NewRGBA(image.Rect(0, 0, 240, 240)), "Loading...", 16)

		// TODO: fix fast button presses can result in a crash when updatting lastActive pointer
		buttonsFn = pirateaudio.SetupPirateAudioButtons(lcdDevice, synthesizer, config, &lastActive, log)
	case "waveshare":
		var err error
		lcdDevice, err = waveshare.NewWaveshare14972Display(waveshare.Waveshare14972LCDOpts{
			Orientation: config.Hardware.DisplayOrientation,
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("component", "waveshare 14972 display").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}
		log.Debug().
			Str("component", "waveshare 14972 display").
			Str("result", "success").
			Msg("init")

		lcdDevice.Clear()

		buttonsFn = waveshare.SetupWaveshareButtons(lcdDevice, synthesizer, config, &lastActive, log)
	default:
		lcdDevice = terminal.NewNullDeviceDisplay()
		log.Debug().
			Str("component", "null display").
			Str("result", "success").
			Msg("init")

		buttonsFn = terminal.SetupNullDeviceButtons(synthesizer, config, opts.Done, log)
	}

	log.Debug().
		Str("component", "audio output device").
		Str("result", "success").
		Msg("init")

	gt, err := telemetry_client.NewGTClient(telemetry_client.GTClientOpts{
		Source:   config.Telemetry.Source,
		Logger:   &log,
		LogLevel: config.App.LogLevel,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gt client").
			Str("result", "failure").
			Msg("init")

		lcdDevice.Show("error")

		return nil, err
	}

	log.Debug().
		Str("component", "app").
		Str("result", "success").
		Msg("init")

	// if !isSetupComplete(log) {
	// 	runSetupWizard(lcdDevice)
	// }

	a := &App{
		buttonsFn:          buttonsFn,
		config:             config,
		done:               opts.Done,
		gearVolumeMin:      0,
		gt:                 gt,
		kinematics:         kinematics,
		lastGear:           NullGear,
		lcdDevice:          lcdDevice,
		log:                log,
		replayMode:         config.App.ReplayMode,
		seq:                uint32(0),
		synth:              synthesizer,
		lastActive:         &lastActive,
		telemetryChartFeed: make(chan map[string]float32, 600),
		vehicleID:          0,
		webEnabled:         opts.WebEnabled,
		webUI:              nil,
	}

	a.drawSplashDisplay(Version)

	return a, nil
}

func (a *App) Run() {
	go a.buttonsFn()

	go a.gt.Run()

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
	err := a.synth.Close()
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "audio output device").
			Str("result", "failure").
			Msg("close")
	}
	a.lcdDevice.Close()
}

func (a *App) hapticEvents() {
	startTime := time.Now()

	seq := a.gt.Telemetry.SequenceID()

	// Do nothing until the sequence ID advances
	seqDelta := seq - a.seq
	if seq == 0 || seqDelta == 0 {
		return
	}

	currentGear := a.gt.Telemetry.CurrentGear()
	currentVehicleID := a.gt.Telemetry.VehicleID()

	if a.vehicleID != currentVehicleID {
		a.resetState(seq, currentGear)
		a.silenceHaptics()

		go a.updateVehicle(currentVehicleID, currentGear)
		a.log.Debug().Uint32("ID", currentVehicleID).Msg("vehicle ID changed")

		return
	}

	if a.gt.Telemetry.Flags().GamePaused {
		a.resetState(seq, currentGear)
		a.silenceHaptics()

		return
	}

	// The loading flag typically means the session has restarted
	if a.sessionHasReset(seq) {
		a.resetState(seq, currentGear)
		a.silenceHaptics()

		return
	}

	// Initialise the gear if it hasn't been set yet
	if a.lastGear == NullGear {
		a.seq = seq
		a.lastGear = currentGear

		a.resetState(seq, currentGear)
		a.silenceHaptics()

		a.log.Debug().Msg("initialising gear")

		return
	}

	currentTimeOfDay := a.gt.Telemetry.TimeOfDay()
	timeOfDayDelta := currentTimeOfDay - a.timeOfDay
	a.timeOfDay = currentTimeOfDay
	if timeOfDayDelta < 0 {
		a.seq = seq

		a.resetState(seq, currentGear)
		a.silenceHaptics()

		a.log.Debug().
			Uint32("sequence_id", seq).
			Str("time_of_day_delta", timeOfDayDelta.String()).
			Msg("time of day reset")

		return
	}

	if !a.gt.Telemetry.Flags().Live && !a.replayMode {
		a.resetState(seq, currentGear)
		a.silenceHaptics()

		a.log.Debug().Msg("not live")

		return
	}

	a.kinematics.Current.SequenceID = seq

	// speaker.Resume()
	a.synth.FadeIn(3 * time.Second)

	a.processHaptics(seqDelta)

	a.seq = seq
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

	a.kinematics.Update(windowMilliseconds, a.gt)

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
	if a.gt.Telemetry.Flags().Loading {
		a.log.Debug().
			Uint32("sequence_id", seq).
			Msg("loading flag detected")

		return true
	}

	return false
}

func (a *App) resetState(seq uint32, currentGear int) {
	a.timeOfDay = a.gt.Telemetry.TimeOfDay()
	a.seq = seq
	a.lastGear = currentGear

	a.synth.Silence()

	a.kinematics = kinematics.NewKinematicsTracker()

	a.synth.ClearBuffer()
}

func (a *App) silenceHaptics() {
	// speaker.Suspend()
	a.synth.Silence()
	a.synth.ClearBuffer()
}

func (a *App) updateDisplay() {
	if a.simTelemetryIsActive() {
		a.drawActiveDisplay()
	} else if a.displayPowerOffTimeoutReached() {
		a.powerOffDisplay()
	} else if a.displayInactiveTimeoutReached() {
		a.drawInactiveDisplay()
	}
}

func (a *App) simTelemetryIsActive() bool {
	if a.gt.Telemetry.Flags().GamePaused {
		return false
	}

	if !a.gt.Telemetry.Flags().Live && !a.replayMode {
		return false
	}

	return true
}

func (a *App) displayPowerOffTimeoutReached() bool {
	return time.Since(*a.lastActive) > 20*time.Second
}

func (a *App) displayInactiveTimeoutReached() bool {
	return time.Since(*a.lastActive) > 5*time.Second
}

func (a *App) powerOffDisplay() {
	if a.displayContent.state == "off" {
		return
	}

	a.lcdDevice.PowerOff()

	a.displayContent = displayContent{
		gear:  NullGear,
		state: "off",
	}

	a.log.Debug().Str("screen", "power off").Msg("display update")

}

func (a *App) drawSplashDisplay(text string) {
	a.lcdDevice.ShowTextOverlay("splash", text, 7)

	a.displayContent = displayContent{
		gear:  NullGear,
		state: "splash",
	}

	a.log.Debug().Str("screen", "splash").Msg("display update")
}

func (a *App) drawInactiveDisplay() {
	if a.displayContent.state == "inactive" {
		return
	}

	a.drawSplashDisplay("waiting")

	a.log.Debug().Str("screen", "waiting").Msg("display update")
}

func (a *App) drawActiveDisplay() {
	currentGear := a.kinematics.Current.TransmissionGear

	if a.displayContent.gear == currentGear || currentGear == NullGear {
		return
	}

	a.lcdDevice.PowerOn()
	*a.lastActive = time.Now()

	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	a.lcdDevice.ShowTextCentered(canvas, gearName(currentGear), gearFontSize)

	a.log.Debug().Str("screen", "gear").Msg("display update")

	a.displayContent = displayContent{
		gear:  currentGear,
		state: "gear",
	}
}

func (a *App) updateVehicle(currentVehicleID uint32, currentGear int) {
	vehicleType := a.gt.Telemetry.VehicleType()
	a.vehicleID = currentVehicleID

	a.log.Debug().
		Uint32("ID", currentVehicleID).
		Str("manufacturer", a.gt.Telemetry.VehicleManufacturer()).
		Str("model", a.gt.Telemetry.VehicleModel()).
		Str("type", vehicleType).
		Msg("vehicle updated")

	fmt.Printf("Vehicle: %s %s [Type: %s ID: %-4d]\r\n",
		a.gt.Telemetry.VehicleManufacturer(),
		a.gt.Telemetry.VehicleModel(),
		vehicleType,
		currentVehicleID,
	)

	switch vehicleType {
	case "race":
		a.gearVolumeMin = float64(a.config.Synthesizer.GearShiftVolumeMinRace) / 100
	default:
		a.gearVolumeMin = float64(a.config.Synthesizer.GearShiftVolumeMinStreet) / 100
	}

	a.lastGear = currentGear
}

func (a *App) checkSessionComplete() {
	if a.gt.Finished {
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
		Int("sequence_id", int(a.seq)).
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

	if a.gt.Telemetry.Flags().GamePaused {
		return
	}

	if a.gt.Finished {
		return
	}

	if a.kinematics.Current.SequenceID == a.webSequenceId {
		return
	}

	a.webSequenceId = a.kinematics.Current.SequenceID

	go func() {
		a.telemetryChartFeed <- map[string]float32{
			"computeTime":                 float32(a.kinematics.Last.ComputeTime.Microseconds()),
			"seq":                         float32(a.seq),
			"timeOfDay":                   float32(a.gt.Telemetry.TimeOfDay().Milliseconds()),
			"throttleInput":               a.gt.Telemetry.ThrottleInputPercent(),
			"throttleOutput":              a.gt.Telemetry.ThrottleOutputPercent(),
			"brakeInput":                  a.gt.Telemetry.BrakeInputPercent(),
			"brakeOutput":                 a.gt.Telemetry.BrakeOutputPercent(),
			"rpm":                         a.gt.Telemetry.EngineRPM(),
			"speed":                       a.gt.Telemetry.GroundSpeedKPH(),
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
