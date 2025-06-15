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

func (c *App) Run() {
	go c.buttonsFn()

	go c.gt.Run()

	go StartWebChartServer(c)

	chassisStreamer := synth.NewBumpStream(c.synth)
	speaker.Init(
		beep.SampleRate(c.synth.GetSampleRate()),
		c.synth.GetSampleRate()/15,
	)
	go speaker.Play(chassisStreamer)

	ticker120fps := time.NewTicker((1000 / 120) * time.Millisecond)
	ticker60fps := time.NewTicker((1000 / 60) * time.Millisecond)
	ticker15fps := time.NewTicker((1000 / 15) * time.Millisecond)

	c.log.Debug().Str("component", "app").Str("result", "success").Msg("main loop started")

	for {
		select {
		case <-c.done:
			return
		case <-ticker120fps.C:
			c.physicsEvents()
		case <-ticker60fps.C:
			c.checkSessionComplete()
			c.sendWebTelemetry()
		case <-ticker15fps.C:
			c.updateDisplay()
		}
	}
}

func (c *App) Close() {
	err := c.synth.Close()
	if err != nil {
		c.log.Error().
			Err(err).
			Str("component", "audio output device").
			Str("result", "failure").
			Msg("close")
	}
	c.lcdDevice.Close()
}

func (c *App) physicsEvents() {
	startTime := time.Now()

	seq := c.gt.Telemetry.SequenceID()

	// Do nothing until the sequence ID advances
	seqDelta := seq - c.seq
	if seq == 0 || seqDelta == 0 {
		return
	}

	currentGear := c.gt.Telemetry.CurrentGear()
	currentVehicleID := c.gt.Telemetry.VehicleID()

	if c.vehicleID != currentVehicleID {
		c.resetState(seq, currentGear)
		c.silenceHaptics()

		go c.updateVehicle(currentVehicleID, currentGear)
		c.log.Debug().Uint32("ID", currentVehicleID).Msg("vehicle ID changed")

		return
	}

	if c.gt.Telemetry.Flags().GamePaused {
		c.resetState(seq, currentGear)
		c.silenceHaptics()

		return
	}

	// The loading flag typically means the session has restarted
	if c.sessionHasReset(seq) {
		c.resetState(seq, currentGear)
		c.silenceHaptics()

		return
	}

	// Initialise the gear if it hasn't been set yet
	if c.lastGear == NullGear {
		c.seq = seq
		c.lastGear = currentGear

		c.resetState(seq, currentGear)
		c.silenceHaptics()

		c.log.Debug().Msg("initialising gear")

		return
	}

	currentTimeOfDay := c.gt.Telemetry.TimeOfDay()
	timeOfDayDelta := currentTimeOfDay - c.timeOfDay
	c.timeOfDay = currentTimeOfDay
	if timeOfDayDelta < 0 {
		c.seq = seq

		c.resetState(seq, currentGear)
		c.silenceHaptics()

		c.log.Debug().
			Uint32("sequence_id", seq).
			Str("time_of_day_delta", timeOfDayDelta.String()).
			Msg("time of day reset")

		return
	}

	if !c.gt.Telemetry.Flags().Live && !c.replayMode {
		c.resetState(seq, currentGear)
		c.silenceHaptics()

		c.log.Debug().Msg("not live")

		return
	}

	c.physics.Current.SequenceID = seq

	// speaker.Resume()
	c.synth.FadeIn(3 * time.Second)

	c.processHaptics(seqDelta)

	c.seq = seq
	c.physics.Current.ComputeTime = time.Since(startTime)
	c.physics.Last = c.physics.Current

	if c.physics.Current.ComputeTime.Microseconds() > 16000 {
		c.log.Warn().Float64("ms", float64(c.physics.Current.ComputeTime.Milliseconds())).Msg("slow compute")
	}

}

func (c *App) processHaptics(seqDelta uint32) {
	// no haptics if sequence ID has not advanced
	if seqDelta < 1 {
		return
	}

	windowMilliseconds := (float64(seqDelta) / frameRate)

	c.physics.Update(windowMilliseconds, c.gt)

	// no haptics if telemetry packets dropped/missed
	if seqDelta > 1 {
		c.log.Debug().Uint32("delta", seqDelta).Msg("missed packets")
	}

	c.generateBump()
}

func (c *App) generateBump() {
	startTime := time.Now()

	snap := signal.LargestMagnitude(c.physics.Current.Velocity.Snap, (c.physics.Current.Attitude.Snap * 100))

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, c.config.GetSnapExponent()))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, c.config.GetSnapScale())
	pulseFrequencyHz := c.config.GetFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < c.config.GetMinHz() {
		pulseFrequencyHz = c.config.GetMinHz()
	} else if pulseFrequencyHz > c.config.GetMaxHz() {
		pulseFrequencyHz = c.config.GetMaxHz()
	}

	pulseWidth := math.Round(float64(c.config.Synthesizer.SampleRateHz) / (2 * pulseFrequencyHz))

	sig := signal.LargestMagnitude(c.physics.Current.Velocity.Jerk, (c.physics.Current.Attitude.Jerk * 100))
	pulseAmplitude := signal.Exponent(sig, c.config.GetJerkExponent())
	pulseAmplitude = signal.Scale(pulseAmplitude, c.config.GetJerkScale())

	p1 := pulseAmplitude
	pulseAmplitude, wasLimited := signal.LimitMax(pulseAmplitude, c.config.Synthesizer.PulseMaxAmplitude)
	if wasLimited {
		c.log.Debug().Float64("pulse", p1).Msg("limiter")
	}

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	bufferLen := c.synth.GetBufferLength()
	pulseBuffer := make([]float64, bufferLen)
	for i := range int(pulseWidth * 2) {
		phase := waveSamplePeriod * (float64(i) - waveOffset)
		pulseBuffer[i] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
	}

	// no haptics when vehicle comes to a controlled stop
	// TODO: check angular velocity, etc to enable for uncontrolled stops
	// if vector.Magnitude(c.physics.Current.Velocity.Vector) >= 0.28 {
	lastMag := vector.Magnitude(c.physics.Last.Velocity.Vector)
	currentMag := vector.Magnitude(c.physics.Current.Velocity.Vector)
	if signal.LargestMagnitude(lastMag, currentMag) >= 0.28 {
		c.synth.WriteBuffer("chassis", pulseBuffer)
	}

	if c.physics.Current.TransmissionGear != NullGear {
		if c.physics.Current.TransmissionGear != c.physics.Last.TransmissionGear {
			volumeMaxPercent, _ := c.synth.GetChannelVolume("gearchange")
			volumeMax := float64(volumeMaxPercent) / 100.0

			gforceSaturation := 1.6 // TODO: create config option
			volumeCurve := 0.26     // TODO: create config option

			gForce := float64(0)
			// Only increase gear change feedback if the vehicle is in motion
			if c.gt.Telemetry.GroundSpeedKPH() > 0.5 {
				gForce = signal.Abs(float64(c.physics.Current.AccelerationLongitude) / gravityConstant)
			}

			gearChangeVolume := (math.Pow((gForce/gforceSaturation), volumeCurve) * volumeMax)
			gearChangeVolume, _ = signal.LimitWindow(gearChangeVolume, c.gearVolumeMin, volumeMax)

			volumePercent := int(gearChangeVolume*100.0 + c.gearVolumeMin)

			c.synth.PlayEffectWithVolume("gearchange", volumePercent)
			c.log.Debug().
				Int("sequence_id", int(c.seq)).
				Int("volume_pc", volumePercent).
				Float64("gforce", gForce).
				Int("gear", c.physics.Current.TransmissionGear).
				Msg("gear change")
		}
	} else {
		c.log.Debug().
			Int("sequence_id", int(c.seq)).
			Msg("no gear")
	}

	c.physics.Last.SynthOutputAmplitude = c.physics.Current.SynthOutputAmplitude
	c.physics.Current.SynthOutputAmplitude = pulseAmplitude

	c.physics.Last.SynthOutputFrequency = c.physics.Current.SynthOutputFrequency
	c.physics.Current.SynthOutputFrequency = int(pulseFrequencyHz)

	if pulseAmplitude > 1.0 || pulseAmplitude < -1.0 {
		c.log.Debug().
			Float64("jerk", c.physics.Current.Velocity.Jerk).
			Float64("snap", c.physics.Current.Velocity.Snap).
			Str("process_time", time.Since(startTime).String()).
			Uint32("sequence_id", c.physics.Current.SequenceID).
			Msg("Bump inputs")
		c.log.Debug().
			Float64("amplitude", pulseAmplitude).
			Float64("samplePeriod", waveSamplePeriod).
			Float64("pulseWidth", pulseWidth).
			Msg("Bump outputs")
	}
}

func (c *App) sessionHasReset(seq uint32) bool {
	if c.gt.Telemetry.Flags().Loading {
		c.log.Debug().
			Uint32("sequence_id", seq).
			Msg("loading flag detected")

		return true
	}

	return false
}

func (c *App) resetState(seq uint32, currentGear int) {
	c.timeOfDay = c.gt.Telemetry.TimeOfDay()
	c.seq = seq
	c.lastGear = currentGear

	c.synth.Silence()

	c.physics = physics.NewPhysicsTracker()

	c.synth.ClearBuffer()
}

func (c *App) silenceHaptics() {
	// speaker.Suspend()
	c.synth.Silence()
	c.synth.ClearBuffer()
}

func (c *App) updateDisplay() {
	if (c.gt.Telemetry.Flags().Live == false && c.replayMode == false) || c.gt.Telemetry.Flags().GamePaused == true {
		if time.Since(*c.lastActive) > 20*time.Second {
			c.lcdDevice.PowerOff()

			return
		}

		if time.Since(*c.lastActive) > 5*time.Second {
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			c.lcdDevice.ShowTextCentered(canvas, "Waiting...", 16)
		}

		return
	}

	currentGear := c.physics.Current.TransmissionGear

	if c.displayContent.gear == currentGear || currentGear == NullGear {
		return
	}

	c.lcdDevice.PowerOn()
	*c.lastActive = time.Now()

	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	c.lcdDevice.ShowTextCentered(canvas, gearName(currentGear), gearFontSize)

	c.displayContent.gear = currentGear
}

func (c *App) updateVehicle(currentVehicleID uint32, currentGear int) {
	vehicleType := c.gt.Telemetry.VehicleType()
	c.vehicleID = currentVehicleID

	c.log.Debug().
		Uint32("ID", currentVehicleID).
		Str("manufacturer", c.gt.Telemetry.VehicleManufacturer()).
		Str("model", c.gt.Telemetry.VehicleModel()).
		Str("type", vehicleType).
		Msg("vehicle updated")

	fmt.Printf("Vehicle: %s %s [Type: %s ID: %-4d]\r\n",
		c.gt.Telemetry.VehicleManufacturer(),
		c.gt.Telemetry.VehicleModel(),
		vehicleType,
		currentVehicleID,
	)

	switch vehicleType {
	case "race":
		c.gearVolumeMin = float64(c.config.Synthesizer.GearVolumeMinRace) / 100
	default:
		c.gearVolumeMin = float64(c.config.Synthesizer.GearVolumeMinStreet) / 100
	}

	c.lastGear = currentGear
}

func (c *App) checkSessionComplete() {
	if c.gt.Finished {
		c.resetState(0, NullGear)
		c.log.Debug().Msg("session finished")
		c.done <- true
	}
}

func (c *App) handleWebSocketConnection(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.log.Error().Str("component", "websocket").Msgf("error upgrading connection: %s", err)
		return
	}
	defer ws.Close()

	c.webSocketClients++
	defer func() {
		c.webSocketClients--
		c.log.Debug().Str("component", "websocket").Int("clients", c.webSocketClients).Msg("connection closed")
	}()
	c.log.Debug().Str("component", "websocket").Int("clients", c.webSocketClients).Msg("connection established")

	sid := 0
	for { // TODO: find alternative solution to satisfy staticcheck
		select {
		case data := <-c.chartDataChannel:
			if sid != 0 {
				diff := int(data["seq"]) - sid
				if diff == 0 {
					continue
				}
			}

			sid = int(data["seq"])

			encodedData, err := json.Marshal(data)
			if err != nil {
				c.log.Error().Err(err).Msg("failed to encode data")
				continue
			}
			err = ws.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				c.log.Error().Err(err).Str("component", "websocket").Msg("failed to send data")
				continue
			}
		}
	}
}

func (c *App) sendWebTelemetry() {
	if !c.webEnabled {
		return
	}

	if c.webSocketClients <= 0 {
		return
	}

	if c.gt.Telemetry.Flags().GamePaused {
		return
	}

	if c.gt.Finished {
		return
	}

	if c.physics.Current.SequenceID == c.webSequenceId {
		return
	}

	c.webSequenceId = c.physics.Current.SequenceID

	go func() {
		c.chartDataChannel <- map[string]float32{
			"seq":                  float32(c.seq),
			"timeOfDay":            float32(c.gt.Telemetry.TimeOfDay().Milliseconds()),
			"throttle":             c.gt.Telemetry.ThrottlePercent(),
			"brake":                c.gt.Telemetry.BrakePercent(),
			"rpm":                  c.gt.Telemetry.EngineRPM(),
			"speed":                c.gt.Telemetry.GroundSpeedKPH(),
			"gear":                 float32(c.physics.Current.TransmissionGear),
			"acceleration":         float32(c.physics.Current.AccelerationLongitude),
			"velocityX":            c.physics.Current.Velocity.Vector.X,
			"velocityY":            c.physics.Current.Velocity.Vector.Y,
			"velocityZ":            c.physics.Current.Velocity.Vector.Z,
			"gforce3D":             float32(c.physics.Current.Velocity.Acceleration3D) / gravityConstant,
			"gforceLong":           float32(c.physics.Current.AccelerationLongitude) / gravityConstant,
			"jerk":                 float32(c.physics.Current.Velocity.Jerk),
			"snap":                 float32(c.physics.Current.Velocity.Snap),
			"attitudeJerk":         float32(c.physics.Current.Attitude.Jerk * 50),
			"synthOutputAmplitude": float32(c.physics.Current.SynthOutputAmplitude),
			"synthOutputFrequency": float32(c.physics.Current.SynthOutputFrequency),
			"computeTime":          float32(c.physics.Last.ComputeTime.Microseconds()),
		}
	}()
}
