package internal

import (
	"fmt"
	"image"
	"math"
	"net/http"
	"os"
	"time"

	json "github.com/goccy/go-json"
	"github.com/gopxl/beep/speaker"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

type displayContent struct {
	gear int
}

type physics struct {
	sequenceID     uint32
	acceleration   float64
	jerk           float64
	snap           float64
	crackle        float64
	velocityDelta  telemetry_client.Vector
	velocityVector telemetry_client.Vector
}

type physicsTracker struct {
	last    physics
	current physics
}

type mixerGain struct {
	master          float64
	fader           float64
	streamer        float64
	gearChange      float64
	chassis         float64
	fadeInIncrement float64
}

type HapticsOutput struct {
	ChassisEnabled    bool
	GearChangeEnabled bool
}

type Core struct {
	assetDir           string
	audio              *AudioOut
	audioBuffer        []float64
	chartDataChannel   chan map[string]float32
	display            Display
	displayContent     displayContent
	done               chan bool
	gearShiftGain      float64
	gt                 *telemetry_client.GTClient
	hapticsOutput      HapticsOutput
	physics            physicsTracker
	lastAcceleration   float64
	lastGear           int
	log                zerolog.Logger
	mixerGain          mixerGain
	pirateAudioEnabled bool
	replayMode         bool
	seq                uint32
	timeOfDay          time.Duration
	timeSinceLive      int
	vehicleID          uint32
	webEnabled         bool
	webSocketClients   int
}

type CoreOptions struct {
	AssetDir           string
	Done               chan bool
	Gain               float64
	HapticsOutput      HapticsOutput
	LogLevel           string
	Orientation        int
	PirateAudioEnabled bool
	ReplayMode         bool
	Source             string
	WebEnabled         bool
}

func NewCore(opts CoreOptions) (*Core, error) {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()

	switch opts.LogLevel {
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
		opts.LogLevel = "warn"
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
		log.Warn().Str("log_level", opts.LogLevel).Msg("unknown log level, setting level to warn")
	}

	var display Display
	if opts.PirateAudioEnabled {
		var err error
		display, err = NewPirateAudioDisplay(PirateAudioDisplayOpts{
			// display, err = NewWaveshare14972Display(Waveshare14972DisplayOpts{
			Orientation: opts.Orientation,
			AssetDir:    opts.AssetDir,
		})
		if err != nil {
			log.Error().
				Err(err).
				// Str("component", "pirate audio display").
				Str("component", "waveshare 14972 display").
				Str("result", "failure").
				Msg("init")

			return nil, err
		}
		log.Debug().
			Str("component", "pirate audio display").
			// Str("component", "waveshare 14972 display").
			Str("result", "success").
			Msg("init")
	} else {
		display = NewNullDisplay()
		log.Debug().
			Str("component", "null display").
			Str("result", "success").
			Msg("init")
	}

	audio, err := NewAudioOutputDevice(AudioOutOpts{
		AssetDir: opts.AssetDir,
		Logger:   log.With().Str("component", "audio").Logger(),
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "audio output device").
			Str("result", "failure").
			Msg("init")

		display.Show("error")

		return nil, err
	}

	log.Debug().
		Str("component", "audio output device").
		Str("result", "success").
		Msg("init")

	gt, err := telemetry_client.NewGTClient(telemetry_client.GTClientOpts{
		Source:   opts.Source,
		Logger:   &log,
		LogLevel: opts.LogLevel,
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gt client").
			Str("result", "failure").
			Msg("init")

		display.Show("error")

		return nil, err
	}

	log.Info().
		Str("component", "core").
		Str("result", "success").
		Msg("init")

	display.Show("splash")

	mixerGain := mixerGain{
		master:          opts.Gain,
		fader:           opts.Gain,
		streamer:        volumeToGain(opts.Gain),
		gearChange:      volumeToGain(opts.Gain) + 3,
		chassis:         volumeToGain(opts.Gain),
		fadeInIncrement: 0,
	}

	audioBuffer := zeroedBuffer(266)

	return &Core{
		assetDir:         opts.AssetDir,
		audio:            audio,
		audioBuffer:      audioBuffer,
		chartDataChannel: make(chan map[string]float32, 600),
		display:          display,
		// done:               make(chan bool),
		done:               opts.Done,
		gearShiftGain:      0,
		gt:                 gt,
		hapticsOutput:      opts.HapticsOutput,
		log:                log,
		mixerGain:          mixerGain,
		physics:            physicsTracker{},
		lastAcceleration:   0,
		lastGear:           1024,
		pirateAudioEnabled: opts.PirateAudioEnabled,
		replayMode:         opts.ReplayMode,
		seq:                uint32(0),
		timeSinceLive:      1000,
		vehicleID:          0,
		webEnabled:         opts.WebEnabled,
		webSocketClients:   0,
	}, nil

}

func (c *Core) Run() {

	go c.setupPirateAudioButtons()
	// go c.setupWaveshareButtons()

	go c.gt.Run()

	go StartWebChartServer(c)

	chassisStreamer := NewBumpStream(
		&c.audioBuffer,
		&c.physics,
		&c.mixerGain.streamer,
		c.hapticsOutput.ChassisEnabled,
	)
	// speaker.Init(44100, 44100/15)
	go speaker.Play(chassisStreamer)

	ticker60fps := time.NewTicker((1000 / 60) * time.Millisecond)
	ticker15fps := time.NewTicker((1000 / 15) * time.Millisecond)

	c.log.Info().Str("component", "core").Str("result", "success").Msg("main loop started")

	for {
		select {
		case <-c.done:
			return
		case <-ticker60fps.C:
			c.physicsEvents()
			c.checkSessionComplete()
			c.updatePhysicsTracker()
		case <-ticker15fps.C:
			c.updateDisplay()
		}
	}
}

func (c *Core) Close() {
	c.display.Close()
}

func zeroedBuffer(length int) []float64 {
	buffer := make([]float64, length)
	for i := 0; i < length; i++ {
		buffer[i] = 0
	}

	return buffer
}

func (c *Core) physicsEvents() {
	go c.sendWebTelemetry()

	seq := c.gt.Telemetry.SequenceID()

	// Do nothing until the sequence ID advances
	seqDelta := seq - c.seq
	if seq == 0 || seqDelta == 0 {
		// c.log.Debug().Msg("waiting for sequence ID to advance")

		return
	}

	currentGear := c.gt.Telemetry.CurrentGear()
	currentVehicleID := c.gt.Telemetry.VehicleID()

	if c.vehicleID != currentVehicleID {
		c.resetState(seq, currentGear)
		c.silenceVolume()

		go c.updateVehicle(currentVehicleID, currentGear)
		c.log.Debug().Uint32("ID", currentVehicleID).Msg("vehicle ID changed")

		return
	}

	if c.gt.Telemetry.Flags().GamePaused == true {
		c.resetState(seq, currentGear)
		c.silenceVolume()

		c.log.Debug().Msg("game paused")

		return
	}

	// The loading flag typically means the session has restarted
	if c.sessionHasReset(seq) {
		c.resetState(seq, currentGear)
		c.silenceVolume()

		return
	}

	// Initialise the gear if it hasn't been set yet
	if c.lastGear == 1024 {
		c.seq = seq
		c.lastGear = currentGear

		c.log.Debug().Msg("initialising gear")

		return
	}

	if c.mixerGain.fader < c.mixerGain.master {
		c.log.Debug().Float64("gain", c.mixerGain.fader).Msg("ramping up haptics")
		if c.mixerGain.fadeInIncrement > 0 {
			c.mixerGain.fader += c.mixerGain.fadeInIncrement
		} else {
			c.mixerGain.fader -= c.mixerGain.fadeInIncrement
		}
		c.mixerGain.streamer = volumeToGain(c.mixerGain.fader)
	} else if c.mixerGain.fader != c.mixerGain.master {
		c.mixerGain.fader = c.mixerGain.master
		c.log.Debug().Float64("gain", c.mixerGain.fader).Msg("ramp up haptics complete")
	}

	currentTimeOfDay := c.gt.Telemetry.TimeOfDay()
	timeOfDayDelta := currentTimeOfDay - c.timeOfDay
	c.timeOfDay = currentTimeOfDay
	if timeOfDayDelta < 0 {
		c.seq = seq

		c.log.Info().
			Uint32("sequence_id", seq).
			Str("time_of_day_delta", fmt.Sprintf("%s", timeOfDayDelta)).
			Msg("time of day reset")

		return
	}

	if c.gt.Telemetry.Flags().Live == false && c.replayMode == false {
		c.log.Debug().Msg("not live")

		return
	}

	c.physics.current.sequenceID = seq

	windowMilliseconds := (float64(seqDelta) / frameRate)

	c.chassisHaptics(windowMilliseconds, seqDelta)

	c.gearChange(currentGear)

	c.seq = seq
}

func (c *Core) chassisHaptics(windowMilliseconds float64, seqDelta uint32) {
	c.physics.current.velocityVector = c.gt.Telemetry.VelocityVector()

	c.physics.current.velocityDelta = vectorDelta(c.physics.current.velocityVector, c.physics.last.velocityVector)
	c.physics.current.acceleration = vectorMagnitudeZBiased(c.physics.current.velocityDelta) / windowMilliseconds
	if c.gt.Telemetry.GroundSpeedKPH() < 100 {
		c.physics.current.acceleration = c.physics.current.acceleration * float64(c.gt.Telemetry.GroundSpeedKPH()/100)
	}

	c.physics.last.jerk = c.physics.current.jerk
	c.physics.current.jerk = (c.physics.current.acceleration - c.physics.last.acceleration) / windowMilliseconds

	c.physics.last.snap = c.physics.current.snap
	c.physics.current.snap = (c.physics.current.jerk - c.physics.last.jerk) / windowMilliseconds

	c.physics.last.crackle = c.physics.current.crackle
	c.physics.current.crackle = (c.physics.current.snap - c.physics.last.snap) / windowMilliseconds

	c.physics.last.sequenceID = c.physics.current.sequenceID

	c.generateBump(seqDelta)
}

func (c *Core) generateBump(seqDelta uint32) {
	bufferLen := len(c.audioBuffer)

	switch seqDelta {
	case 0:
		return
	case 1:
		c.shiftBuffer(bufferLen)
	default:
		c.log.Warn().Uint32("delta", seqDelta).Msg("sequence ID delta greater than 1")
		for i := 0; i < bufferLen-1; i++ {
			c.audioBuffer[i] = 0
		}
	}

	startTime := time.Now()

	sampleLen := bufferLen / 2

	// exponent 0.5, scale 1/47.5 (1/57.0)
	// exponent 0.4, scale 1/29.75 (1/36.0)
	// log10, scale 0.08
	// log2, scale 0.025
	thisAmplitude := functionExponent(c.physics.current.jerk, 0.4)
	thisAmplitude = functionScale(thisAmplitude, 1/36.0)

	// clamp large bump values
	if thisAmplitude > 1.2 {
		thisAmplitude = 1.2
	} else if thisAmplitude < -1.2 {
		thisAmplitude = -1.2
	}

	snap := functionExponent(c.physics.current.jerk, 0.5)
	snap = functionScale(snap, 1/47.5)
	impact := thisAmplitude * snap
	periodReduction := 0.0
	if impact < 6 {
		periodReduction = snap * 10
		if periodReduction > 30 {
			periodReduction = 30
		}
	}

	if !c.hapticsOutput.ChassisEnabled {
		for i := 0; i < sampleLen; i++ {
			c.audioBuffer[i] = 0
		}

		return
	}

	periodLen := float64(sampleLen) / 2

	if periodReduction > 0 {
		periodLen = periodLen - periodReduction
	} else if periodReduction < 0 {
		periodLen = periodLen + periodReduction
	}

	waveLen := periodLen * 2
	offset := periodLen / 2
	samplePeriod := math.Pi / periodLen

	peak := 0.0
	for i := range sampleLen {
		if float64(i) > waveLen {
			c.audioBuffer[i] = 0
		} else {
			sineValue := thisAmplitude*math.Sin(samplePeriod*(float64(i)-offset)) + thisAmplitude

			c.audioBuffer[i] = sineValue

			if sineValue > 0 && sineValue > peak {
				peak = sineValue
			} else if sineValue < 0 && sineValue < peak {
				peak = sineValue
			}
		}
	}

	if c.log.GetLevel() != zerolog.DebugLevel {
		return
	}

	if peak > 1 || peak < -1 {
		duration := time.Since(startTime)
		fmt.Printf("INPUT:  jerk: %0.05f, snap: %0.05f, impact: %0.05f, time: %v seq: %d\n", c.physics.current.jerk, c.physics.current.snap, impact, duration, c.physics.current.sequenceID)
		fmt.Printf("OUTPUT: peak: %0.05f, amplitude: %0.05f, reduce: %0.05f, samplePeriod: %0.05f, periodLen: %0.05f, waveLen: %0.05f\n\n", peak, thisAmplitude, periodReduction, samplePeriod, periodLen, waveLen)
	}
}

func (c *Core) shiftBuffer(length int) {
	offset := length / 2

	for i := 0; i < offset-1; i++ {
		c.audioBuffer[i+offset] = c.audioBuffer[i]
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
	c.mixerGain.fader = -30
	c.mixerGain.streamer = volumeToGain(c.mixerGain.fader)
	c.mixerGain.fadeInIncrement = (c.mixerGain.fader - c.mixerGain.master) / (frameRate * 2)
	c.physics.last = physics{}
	c.physics.current = physics{}
	c.audioBuffer = zeroedBuffer(int(len(c.audioBuffer)))

	return
}

func (c *Core) silenceVolume() {
	c.mixerGain.fader = -30
	c.mixerGain.streamer = volumeToGain(c.mixerGain.fader)
	c.mixerGain.fadeInIncrement = (c.mixerGain.fader - c.mixerGain.master) / (frameRate * 2)

	return
}

func (c *Core) updateDisplay() {
	currentGear := c.lastGear

	if c.displayContent.gear == currentGear {
		return
	}

	if c.gt.Telemetry.Flags().Live == false && c.replayMode == false {
		c.timeSinceLive += 1

		return
	}

	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	c.display.ShowTextCentered(canvas, gearName(currentGear), gearFontSize)

	c.displayContent.gear = currentGear
}

func (c *Core) updatePhysicsTracker() {
	c.physics.last = c.physics.current
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

	switch vehicleType {
	case "street":
		c.gearShiftGain = -2
	case "race":
		c.gearShiftGain = 0
	default:
		c.gearShiftGain = -2
	}
	c.lastGear = currentGear
}

func (c *Core) gearChange(currentGear int) {
	if c.lastGear == currentGear {
		return
	}

	c.lastGear = currentGear

	if c.gt.Telemetry.Flags().Loading == true {
		return
	}

	if c.replayMode == false && c.gt.Telemetry.Flags().Live == false {
		return
	}

	if c.hapticsOutput.GearChangeEnabled {
		gain := c.gearShiftGain + c.mixerGain.fader
		if currentGear != NeutralGear {
			c.audio.Play("gearchange", gain)
		}

		c.log.Info().
			Int("sequence_id", int(c.seq)).
			Int("gear", currentGear).
			Float64("audio_gain", gain).
			Msg("gear change")
	}
}

func (c *Core) checkSessionComplete() {
	if c.gt.Finished {
		c.resetState(0, 1024)
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
			"timeOfDay": float32(c.gt.Telemetry.TimeOfDay().Milliseconds()),
			"throttle":  c.gt.Telemetry.ThrottlePercent(),
			"brake":     c.gt.Telemetry.BrakePercent(),
			"rpm":       c.gt.Telemetry.EngineRPM(),
			"speed":     c.gt.Telemetry.GroundSpeedKPH(),
			"velocityX": c.physics.last.velocityVector.X,
			"velocityY": c.physics.last.velocityVector.Y,
			"velocityZ": c.physics.last.velocityVector.Z,
			"gforce":    float32(c.lastAcceleration) / gravityConstant,
			"snap":      float32(c.physics.last.jerk),
		}
	}()
}
