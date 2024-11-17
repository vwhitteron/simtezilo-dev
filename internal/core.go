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
	telemetry_client "github.com/vwhitteron/gt-telemetry"
	"github.com/vwhitteron/simtezilo-dev/internal/config"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware/nulldevice"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware/waveshare"
	"github.com/vwhitteron/simtezilo-dev/internal/physics"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/vector"
	"github.com/vwhitteron/simtezilo-dev/internal/signal"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
)

type displayContent struct {
	gear int
}

type appInfo struct {
	BuildTime string
	Version   string
}

type Core struct {
	appInfo          appInfo
	assetDir         string
	buttonsFn        func()
	chartDataChannel chan map[string]float32
	config           *config.Config
	lcdDevice        hardware.LCD
	displayContent   displayContent
	done             chan bool
	gt               *telemetry_client.GTClient
	physics          physics.PhysicsTracker
	lastGear         int
	log              zerolog.Logger
	replayMode       bool
	seq              uint32
	synth            *synth.Synthesizer
	timeOfDay        time.Duration
	lastActive       time.Time
	vehicleID        uint32
	webEnabled       bool
	webSocketClients int
}

type CoreOptions struct {
	Done       chan bool
	Gain       float64
	BuildTime  string
	Version    string
	WebEnabled bool
}

func NewCore(opts CoreOptions) (*Core, error) {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	config := config.NewConfig("simtezilo.conf", log)

	switch config.App.LogLevel {
	case "trace":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "panic":
		zerolog.SetGlobalLevel(zerolog.PanicLevel)
	case "off":
		zerolog.SetGlobalLevel(zerolog.Disabled)
	default:
		config.App.LogLevel = "warn"
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
		log.Warn().Str("log_level", config.App.LogLevel).Msg("unknown log level, setting level to warn")
	}

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

		buttonsFn = pirateaudio.SetupPirateAudioButtons(lcdDevice, synthesizer, log)
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

		buttonsFn = waveshare.SetupWaveshareButtons(lcdDevice, synthesizer, log)
	default:
		lcdDevice = nulldevice.NewNullDeviceDisplay()
		log.Debug().
			Str("component", "null display").
			Str("result", "success").
			Msg("init")

		buttonsFn = nulldevice.SetupNullDeviceButtons(synthesizer, opts.Done, log)
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

	log.Info().
		Str("component", "core").
		Str("result", "success").
		Msg("init")

	lcdDevice.ShowTextOverlay("splash", appInfo.Version, 7)

	return &Core{
		appInfo:          appInfo,
		assetDir:         config.App.AssetDir,
		buttonsFn:        buttonsFn,
		chartDataChannel: make(chan map[string]float32, 600),
		config:           config,
		done:             opts.Done,
		gt:               gt,
		lastGear:         NullGear,
		lcdDevice:        lcdDevice,
		log:              log,
		physics:          physics,
		replayMode:       config.App.ReplayMode,
		seq:              uint32(0),
		synth:            synthesizer,
		lastActive:       time.Time{},
		vehicleID:        0,
		webEnabled:       opts.WebEnabled,
		webSocketClients: 0,
	}, nil

}

func (c *Core) Run() {
	go c.buttonsFn()

	go c.gt.Run()

	go StartWebChartServer(c)

	chassisStreamer := synth.NewBumpStream(c.synth)
	speaker.Init(
		beep.SampleRate(c.synth.GetSampleRate()),
		c.synth.GetSampleRate()/15,
	)
	go speaker.Play(chassisStreamer)

	ticker60fps := time.NewTicker((1000 / 120) * time.Millisecond)
	ticker15fps := time.NewTicker((1000 / 15) * time.Millisecond)

	c.log.Info().Str("component", "core").Str("result", "success").Msg("main loop started")

	for {
		select {
		case <-c.done:
			return
		case <-ticker60fps.C:
			go c.sendWebTelemetry()
			c.physicsEvents()
			c.checkSessionComplete()
		case <-ticker15fps.C:
			c.updateDisplay()
		}
	}
}

func (c *Core) Close() {
	c.lcdDevice.Close()
}

func (c *Core) physicsEvents() {
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

	if c.gt.Telemetry.Flags().GamePaused == true {
		c.resetState(seq, currentGear)
		c.silenceHaptics()

		c.log.Debug().Msg("game paused")

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

		c.log.Info().
			Uint32("sequence_id", seq).
			Str("time_of_day_delta", fmt.Sprintf("%s", timeOfDayDelta)).
			Msg("time of day reset")

		return
	}

	if c.gt.Telemetry.Flags().Live == false && c.replayMode == false {
		c.resetState(seq, currentGear)
		c.silenceHaptics()

		c.log.Debug().Msg("not live")

		return
	}

	c.physics.Current.SequenceID = seq

	c.synth.FadeIn(3 * time.Second)

	c.processHaptics(seqDelta)

	c.seq = seq
	c.physics.Last = c.physics.Current
}

func (c *Core) processHaptics(seqDelta uint32) {
	windowMilliseconds := (float64(seqDelta) / frameRate)

	c.physics.Update(windowMilliseconds, c.gt)

	// no haptics if sequence ID has not advanced
	if seqDelta < 1 {
		return
	}

	// no haptics if telemetry packets dropped/missed
	if seqDelta > 1 {
		// FIXME: no longer ignore missed packets?
		// c.log.Debug().Uint32("delta", seqDelta).Msg("missed packets, skipping haptics")
		c.log.Debug().Uint32("delta", seqDelta).Msg("missed packets")

		// return
	}

	c.generateBump()
}

func (c *Core) generateBump() {
	startTime := time.Now()

	pulseWidth := c.config.Synthesizer.PulseWidthMax

	snap := signal.LargestMagnitude(c.physics.Current.Velocity.Snap, (c.physics.Current.Attitude.Snap * 100))

	pulseWidthReduction := signal.Abs(signal.Scale(snap, 1/800.0))
	pulseWidth -= pulseWidthReduction

	if pulseWidth < c.config.Synthesizer.PulseWidthMin {
		pulseWidth = c.config.Synthesizer.PulseWidthMin
	}
	// exponent 0.5, scale 1/47.5 (1/57.0) - small bumps slightly too loud
	// exponent 0.4, scale 1/29.75 (1/36.0) - best balance of small, med and large bumps
	// log10, scale 0.08 - small bumps too loud
	// log2, scale 0.025 - small bumps too loud
	sig := signal.LargestMagnitude(c.physics.Current.Velocity.Jerk, (c.physics.Current.Attitude.Jerk * 50))
	pulseAmplitude := signal.Exponent(sig, c.config.Synthesizer.PulseExponent)
	pulseAmplitude = signal.Scale(pulseAmplitude, c.config.Synthesizer.PulseScaleAdjustment)

	pulseFrequency := int(math.Round(float64(c.config.Synthesizer.SampleRateHz) / (2 * pulseWidth)))

	p1 := pulseAmplitude
	pulseAmplitude, wasLimited := signal.Limit(pulseAmplitude, c.config.Synthesizer.PulseMaxAmplitude)
	if wasLimited {
		c.log.Info().Float64("pulse", p1).Int("frequency", pulseFrequency).Msg("limiter")
	}

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	bufferLen := c.synth.GetBufferLength()
	pulseBuffer := make([]float64, bufferLen)
	for i := 0; i < int(pulseWidth*2); i++ {
		phase := waveSamplePeriod * (float64(i) - waveOffset)
		pulseBuffer[i] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
	}

	// no haptics if vehicle speed velocity lower than 30cm per second
	if vector.Magnitude(c.physics.Current.Velocity.Vector) >= 0.28 {
		c.synth.WriteBuffer("chassis", pulseBuffer)
	}

	if c.physics.Current.TransmissionGear != NullGear {

		if c.physics.Current.TransmissionGear != c.physics.Last.TransmissionGear {
			c.synth.PlayEffect("gearchange")
			c.log.Info().
				Int("sequence_id", int(c.seq)).
				Int("gear", c.physics.Current.TransmissionGear).
				Msg("gear change")
		}

	} else {
		c.log.Info().
			Int("sequence_id", int(c.seq)).
			Msg("no gear")
	}

	c.physics.Last.SynthOutputAmplitude = c.physics.Current.SynthOutputAmplitude
	c.physics.Current.SynthOutputAmplitude = pulseAmplitude

	c.physics.Last.SynthOutputFrequency = c.physics.Current.SynthOutputFrequency
	c.physics.Current.SynthOutputFrequency = pulseFrequency

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

func (c *Core) sessionHasReset(seq uint32) bool {
	if c.gt.Telemetry.Flags().Loading == true {
		c.log.Info().
			Uint32("sequence_id", seq).
			Msg("loading flag detected")

		return true
	}

	return false
}

func (c *Core) resetState(seq uint32, currentGear int) {
	c.timeOfDay = c.gt.Telemetry.TimeOfDay()
	c.seq = seq
	c.lastGear = currentGear

	c.synth.Silence()

	c.physics = physics.NewPhysicsTracker()

	c.synth.ClearBuffer()

	return
}

func (c *Core) silenceHaptics() {
	c.synth.Silence()
	c.synth.ClearBuffer()

	return
}

func (c *Core) updateDisplay() {
	currentGear := c.physics.Current.TransmissionGear

	if c.displayContent.gear == currentGear || currentGear == NullGear {
		return
	}

	// if (c.gt.Telemetry.Flags().Live == false && c.replayMode == false) || c.gt.Telemetry.Flags().GamePaused == true {
	// 	if time.Since(c.lastActive) > 10*time.Second {
	// 		c.lcdDevice.PowerOff()

	// 		return
	// 	}

	// 	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	// 	c.lcdDevice.ShowTextCentered(canvas, "Waiting...", 16)

	// 	return
	// }

	c.lcdDevice.PowerOn()
	c.lastActive = time.Now()

	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	c.lcdDevice.ShowTextCentered(canvas, gearName(currentGear), gearFontSize)

	c.displayContent.gear = currentGear
}

func (c *Core) updateVehicle(currentVehicleID uint32, currentGear int) {
	vehicleType := c.gt.Telemetry.VehicleType()
	c.vehicleID = currentVehicleID

	c.log.Info().
		Uint32("ID", currentVehicleID).
		Str("manufacturer", c.gt.Telemetry.VehicleManufacturer()).
		Str("model", c.gt.Telemetry.VehicleModel()).
		Str("type", vehicleType).
		Msg("vehicle updated")

	var volume int
	switch vehicleType {
	case "race":
		volume = c.config.Synthesizer.GearRaceVolume
	default:
		volume = c.config.Synthesizer.GearStreetVolume
	}

	err := c.synth.SetChannelVolume("gearchange", volume)
	if err != nil {
		c.log.Error().Err(err).Str("channel", "gear").Msg("failed to set volume")
	}
	c.log.Info().
		Str("channel", "gear").
		Int("volume", volume).
		Msg("volume set")

	c.lastGear = currentGear
}

func (c *Core) checkSessionComplete() {
	if c.gt.Finished {
		c.resetState(0, NullGear)
		c.log.Info().Msg("session finished")
		c.done <- true
	}
}

func (c *Core) handleWebSocketConnection(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.log.Error().Msgf("Error upgrading to WebSocket: %s", err)
		return
	}
	defer ws.Close()

	c.webSocketClients++
	defer func() {
		c.webSocketClients--
		c.log.Info().Int("clients", c.webSocketClients).Msg("websocket connection closed")
	}()
	c.log.Info().Int("clients", c.webSocketClients).Msg("websocket connection established")

	for {
		select {
		case data := <-c.chartDataChannel:
			encodedData, err := json.Marshal(data)
			if err != nil {
				c.log.Error().Err(err).Msg("failed to encode data")
				continue
			}
			err = ws.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				c.log.Error().Err(err).Msg("failed to send data over WebSocket")
				continue
			}
		}
	}
}

func (c *Core) sendWebTelemetry() {
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

	go func() {
		c.chartDataChannel <- map[string]float32{
			"timeOfDay":            float32(c.gt.Telemetry.TimeOfDay().Milliseconds()),
			"throttle":             c.gt.Telemetry.ThrottlePercent(),
			"brake":                c.gt.Telemetry.BrakePercent(),
			"rpm":                  c.gt.Telemetry.EngineRPM(),
			"speed":                c.gt.Telemetry.GroundSpeedKPH(),
			"velocityX":            c.physics.Current.Velocity.Vector.X,
			"velocityY":            c.physics.Current.Velocity.Vector.Y,
			"velocityZ":            c.physics.Current.Velocity.Vector.Z,
			"gforce":               float32(c.physics.Current.Velocity.Acceleration) / gravityConstant,
			"jerk":                 float32(c.physics.Current.Velocity.Jerk),
			"snap":                 float32(c.physics.Current.Velocity.Snap),
			"attitudeJerk":         float32(c.physics.Current.Attitude.Jerk * 50),
			"synthOutputAmplitude": float32(c.physics.Current.SynthOutputAmplitude),
			"synthOutputFrequency": float32(c.physics.Current.SynthOutputFrequency),
		}
	}()
}
