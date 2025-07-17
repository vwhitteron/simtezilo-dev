package internal

import (
	"fmt"
	"image"
	"math"
	"net/http"
	"os"
	"time"

	json "github.com/goccy/go-json"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/config"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware/nulldevice"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/internal/physics"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/vector"
	"github.com/vwhitteron/simtezilo-dev/internal/signal"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

type displayContent struct {
	gear int
}

type appInfo struct {
	BuildTime string
	Version   string
}

type App struct {
	appInfo          appInfo
	assetDir         string
	buttonsFn        func()
	chartDataChannel chan map[string]float32
	config           *config.Config
	lcdDevice        hardware.LCD
	displayContent   displayContent
	done             chan bool
	gearVolumeMin    float64
	gt               *telemetry_client.GTClient
	lastGear         int
	log              zerolog.Logger
	physics          physics.PhysicsTracker
	replayMode       bool
	seq              uint32
	synth            *synth.Synthesizer
	timeOfDay        time.Duration
	lastActive       *time.Time
	vehicleID        uint32
	webEnabled       bool
	webSocketClients int
	webSequenceId    uint32
}

type AppOptions struct {
	BuildTime  string
	Done       chan bool
	LogLevel   string
	Version    string
	WebEnabled bool
}

func NewApp(opts AppOptions) (*App, error) {
	var logLevel zerolog.Level

	switch opts.LogLevel {
	case "trace":
		logLevel = zerolog.TraceLevel
	case "debug":
		logLevel = zerolog.DebugLevel
	case "info":
		logLevel = zerolog.InfoLevel
	case "warn":
		logLevel = zerolog.WarnLevel
	case "error":
		logLevel = zerolog.ErrorLevel
	case "fatal":
		logLevel = zerolog.FatalLevel
	case "panic":
		logLevel = zerolog.PanicLevel
	case "off":
		logLevel = zerolog.Disabled
	case "":
		logLevel = zerolog.WarnLevel
	default:
		logLevel = zerolog.WarnLevel
		fmt.Printf("invalid log level parameter %q, setting level to warn", opts.LogLevel)
	}

	log := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel)

	config := config.NewConfig("simtezilo.conf", log)

	if opts.LogLevel == "" {
		switch config.App.LogLevel {
		case "trace":
			logLevel = zerolog.TraceLevel
		case "debug":
			logLevel = zerolog.DebugLevel
		case "info":
			logLevel = zerolog.InfoLevel
		case "warn":
			logLevel = zerolog.WarnLevel
		case "error":
			logLevel = zerolog.ErrorLevel
		case "fatal":
			logLevel = zerolog.FatalLevel
		case "panic":
			logLevel = zerolog.PanicLevel
		case "off":
			logLevel = zerolog.Disabled
		default:
			logLevel = zerolog.WarnLevel
			log.Error().Str("configured", config.App.LogLevel).Str("fallback", "warn").Msg("invalid log level")
		}
	}

	log = log.Level(logLevel)

	log.Info().Str("Level", logLevel.String()).Msg("log level")

	appInfo := appInfo{
		BuildTime: opts.BuildTime,
		Version:   opts.Version,
	}

	physics := physics.NewPhysicsTracker()

	synthesizer, err := synth.NewSynth(synth.SynthOpts{
		AssetDir: config.App.AssetDir,
		Config:   config.Synthesizer,
		Logger:   log.With().Str("component", "synth").Logger(),
		Physics:  &physics,
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
			AssetDir:    config.App.AssetDir,
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
			AssetDir:    config.App.AssetDir,
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

		buttonsFn = waveshare.SetupWaveshareButtons(lcdDevice, synthesizer, config, log)
	default:
		lcdDevice = nulldevice.NewNullDeviceDisplay()
		log.Debug().
			Str("component", "null display").
			Str("result", "success").
			Msg("init")

		buttonsFn = nulldevice.SetupNullDeviceButtons(synthesizer, config, opts.Done, log)
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

	lcdDevice.ShowTextOverlay("splash", appInfo.Version, 7)

	// if !isSetupComplete(log) {
	// 	runSetupWizard(lcdDevice)
	// }

	return &App{
		appInfo:          appInfo,
		assetDir:         config.App.AssetDir,
		buttonsFn:        buttonsFn,
		chartDataChannel: make(chan map[string]float32, 600),
		config:           config,
		done:             opts.Done,
		gearVolumeMin:    0,
		gt:               gt,
		lastGear:         NullGear,
		lcdDevice:        lcdDevice,
		log:              log,
		physics:          physics,
		replayMode:       config.App.ReplayMode,
		seq:              uint32(0),
		synth:            synthesizer,
		lastActive:       &lastActive,
		vehicleID:        0,
		webEnabled:       opts.WebEnabled,
		webSocketClients: 0,
	}, nil

}

func (a *App) Run() {
	go a.buttonsFn()

	go a.gt.Run()

	go StartWebChartServer(a)

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
			a.physicsEvents()
		case <-ticker60fps.C:
			a.checkSessionComplete()
			a.sendWebTelemetry()
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

func (a *App) physicsEvents() {
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

	a.physics.Current.SequenceID = seq

	// speaker.Resume()
	a.synth.FadeIn(3 * time.Second)

	a.processHaptics(seqDelta)

	a.seq = seq
	a.physics.Current.ComputeTime = time.Since(startTime)
	a.physics.Last = a.physics.Current

	if a.physics.Current.ComputeTime.Microseconds() > 16000 {
		a.log.Warn().Float64("ms", float64(a.physics.Current.ComputeTime.Milliseconds())).Msg("slow compute")
	}

}

func (a *App) processHaptics(seqDelta uint32) {
	// no haptics if sequence ID has not advanced
	if seqDelta < 1 {
		return
	}

	windowMilliseconds := (float64(seqDelta) / frameRate)

	a.physics.Update(windowMilliseconds, a.gt)

	// no haptics if telemetry packets dropped/missed
	if seqDelta > 1 {
		a.log.Debug().Uint32("delta", seqDelta).Msg("missed packets")
	}

	a.generateBump()
}

func (a *App) generateBump() {
	startTime := time.Now()

	snap := signal.LargestMagnitude(a.physics.Current.Velocity.Snap, (a.physics.Current.Attitude.Snap * 100))

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, a.config.GetSnapExponent()))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, a.config.GetSnapScale())
	pulseFrequencyHz := a.config.GetFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < a.config.GetMinHz() {
		pulseFrequencyHz = a.config.GetMinHz()
	} else if pulseFrequencyHz > a.config.GetMaxHz() {
		pulseFrequencyHz = a.config.GetMaxHz()
	}

	pulseWidth := math.Round(float64(a.config.Synthesizer.SampleRateHz) / (2 * pulseFrequencyHz))

	sig := signal.LargestMagnitude(a.physics.Current.Velocity.Jerk, (a.physics.Current.Attitude.Jerk * 100))
	pulseAmplitude := signal.Exponent(sig, a.config.GetJerkExponent())
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
	// if vector.Magnitude(c.physics.Current.Velocity.Vector) >= 0.28 {
	lastMag := vector.Magnitude(a.physics.Last.Velocity.Vector)
	currentMag := vector.Magnitude(a.physics.Current.Velocity.Vector)
	if signal.LargestMagnitude(lastMag, currentMag) >= 0.28 {
		a.synth.WriteBuffer("chassis", pulseBuffer)
	}

	if a.physics.Current.TransmissionGear != NullGear {
		if a.physics.Current.TransmissionGear != a.physics.Last.TransmissionGear {
			volumeMaxPercent, _ := a.synth.GetChannelVolume("gearchange")
			volumeMax := float64(volumeMaxPercent) / 100.0

			gforceSaturation := 1.6 // TODO: create config option
			volumeCurve := 0.26     // TODO: create config option

			gForce := float64(0)
			// Only increase gear change feedback if the vehicle is in motion
			if a.gt.Telemetry.GroundSpeedKPH() > 0.5 {
				gForce = signal.Abs(float64(a.physics.Current.AccelerationLongitude) / gravityConstant)
			}

			gearChangeVolume := (math.Pow((gForce/gforceSaturation), volumeCurve) * volumeMax)
			gearChangeVolume, _ = signal.LimitWindow(gearChangeVolume, a.gearVolumeMin, volumeMax)

			volumePercent := int(gearChangeVolume*100.0 + a.gearVolumeMin)

			a.synth.PlayEffectWithVolume("gearchange", volumePercent)
			a.log.Debug().
				Int("sequence_id", int(a.seq)).
				Int("volume_pc", volumePercent).
				Float64("gforce", gForce).
				Int("gear", a.physics.Current.TransmissionGear).
				Msg("gear change")
		}
	} else {
		a.log.Debug().
			Int("sequence_id", int(a.seq)).
			Msg("no gear")
	}

	a.physics.Last.SynthOutputAmplitude = a.physics.Current.SynthOutputAmplitude
	a.physics.Current.SynthOutputAmplitude = pulseAmplitude

	a.physics.Last.SynthOutputFrequency = a.physics.Current.SynthOutputFrequency
	a.physics.Current.SynthOutputFrequency = int(pulseFrequencyHz)

	if pulseAmplitude > 1.0 || pulseAmplitude < -1.0 {
		a.log.Debug().
			Float64("jerk", a.physics.Current.Velocity.Jerk).
			Float64("snap", a.physics.Current.Velocity.Snap).
			Str("process_time", time.Since(startTime).String()).
			Uint32("sequence_id", a.physics.Current.SequenceID).
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

	a.physics = physics.NewPhysicsTracker()

	a.synth.ClearBuffer()
}

func (a *App) silenceHaptics() {
	// speaker.Suspend()
	a.synth.Silence()
	a.synth.ClearBuffer()
}

func (a *App) updateDisplay() {
	if (a.gt.Telemetry.Flags().Live == false && a.replayMode == false) || a.gt.Telemetry.Flags().GamePaused == true {
		if time.Since(*a.lastActive) > 20*time.Second {
			a.lcdDevice.PowerOff()

			return
		}

		if time.Since(*a.lastActive) > 5*time.Second {
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			a.lcdDevice.ShowTextCentered(canvas, "Waiting...", 16)
		}

		return
	}

	currentGear := a.physics.Current.TransmissionGear

	if a.displayContent.gear == currentGear || currentGear == NullGear {
		return
	}

	a.lcdDevice.PowerOn()
	*a.lastActive = time.Now()

	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	a.lcdDevice.ShowTextCentered(canvas, gearName(currentGear), gearFontSize)

	a.displayContent.gear = currentGear
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
		a.gearVolumeMin = float64(a.config.Synthesizer.GearVolumeMinRace) / 100
	default:
		a.gearVolumeMin = float64(a.config.Synthesizer.GearVolumeMinStreet) / 100
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

func (a *App) handleWebSocketConnection(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		a.log.Error().Str("component", "websocket").Msgf("error upgrading connection: %s", err)
		return
	}
	defer ws.Close()

	a.webSocketClients++
	defer func() {
		a.webSocketClients--
		a.log.Debug().Str("component", "websocket").Int("clients", a.webSocketClients).Msg("connection closed")
	}()
	a.log.Debug().Str("component", "websocket").Int("clients", a.webSocketClients).Msg("connection established")

	sid := 0
	for { // TODO: find alternative solution to satisfy staticcheck
		select {
		case data := <-a.chartDataChannel:
			if sid != 0 {
				diff := int(data["seq"]) - sid
				if diff == 0 {
					continue
				}
			}

			sid = int(data["seq"])

			encodedData, err := json.Marshal(data)
			if err != nil {
				a.log.Error().Err(err).Msg("failed to encode data")
				continue
			}
			err = ws.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				a.log.Error().Err(err).Str("component", "websocket").Msg("failed to send data")
				continue
			}
		}
	}
}

func (a *App) sendWebTelemetry() {
	if !a.webEnabled {
		return
	}

	if a.webSocketClients <= 0 {
		return
	}

	if a.gt.Telemetry.Flags().GamePaused {
		return
	}

	if a.gt.Finished {
		return
	}

	if a.physics.Current.SequenceID == a.webSequenceId {
		return
	}

	a.webSequenceId = a.physics.Current.SequenceID

	go func() {
		a.chartDataChannel <- map[string]float32{
			"seq":                  float32(a.seq),
			"timeOfDay":            float32(a.gt.Telemetry.TimeOfDay().Milliseconds()),
			"throttle":             a.gt.Telemetry.ThrottlePercent(),
			"brake":                a.gt.Telemetry.BrakePercent(),
			"rpm":                  a.gt.Telemetry.EngineRPM(),
			"speed":                a.gt.Telemetry.GroundSpeedKPH(),
			"gear":                 float32(a.physics.Current.TransmissionGear),
			"acceleration":         float32(a.physics.Current.AccelerationLongitude),
			"velocityX":            a.physics.Current.Velocity.Vector.X,
			"velocityY":            a.physics.Current.Velocity.Vector.Y,
			"velocityZ":            a.physics.Current.Velocity.Vector.Z,
			"gforce3D":             float32(a.physics.Current.Velocity.Acceleration3D) / gravityConstant,
			"gforceLong":           float32(a.physics.Current.AccelerationLongitude) / gravityConstant,
			"jerk":                 float32(a.physics.Current.Velocity.Jerk),
			"snap":                 float32(a.physics.Current.Velocity.Snap),
			"attitudeJerk":         float32(a.physics.Current.Attitude.Jerk * 50),
			"synthOutputAmplitude": float32(a.physics.Current.SynthOutputAmplitude),
			"synthOutputFrequency": float32(a.physics.Current.SynthOutputFrequency),
			"computeTime":          float32(a.physics.Last.ComputeTime.Microseconds()),
		}
	}()
}
