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
	"github.com/vwhitteron/gt-pi/internal/signal"
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

type displayContent struct {
	gear int
}

type physics struct {
	sequenceID uint32

	attitudeAcceleration float64
	attitudeJerk         float64
	attitudeSnap         float64
	attitudeDelta        telemetry_client.SymmetryAxes
	attitudeVector       telemetry_client.SymmetryAxes

	acceleration   float64
	jerk           float64
	snap           float64
	crackle        float64
	velocityDelta  telemetry_client.Vector
	velocityVector telemetry_client.Vector

	audioOutValue float64
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

type audioBuffer struct {
	slotSize int
	slots    int
	buffer   []float64
}

type Core struct {
	assetDir           string
	audioDevice        *AudioOutDevice
	audioBuffer        audioBuffer
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

	audioDevice, err := NewAudioOutputDevice(AudioOutDeviceOpts{
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

	return &Core{
		assetDir:           opts.AssetDir,
		audioDevice:        audioDevice,
		audioBuffer:        newAudioBuffer(133, 6),
		chartDataChannel:   make(chan map[string]float32, 600),
		display:            display,
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

func newAudioBuffer(slotSize int, slots int) audioBuffer {
	return audioBuffer{
		slotSize: slotSize,
		slots:    slots,
		buffer:   zeroedBuffer(slotSize * slots),
	}
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
	c.physics.current.attitudeVector = c.gt.Telemetry.RotationVector()

	// chassis attitude
	c.physics.current.attitudeDelta = symmetryAxisDelta(c.physics.current.attitudeVector, c.physics.last.attitudeVector)
	c.physics.current.attitudeAcceleration = symmetryAxisMagnitude(c.physics.current.attitudeDelta) / windowMilliseconds

	c.physics.last.attitudeJerk = c.physics.current.attitudeJerk
	c.physics.current.attitudeJerk = (c.physics.current.attitudeAcceleration - c.physics.last.attitudeAcceleration) / windowMilliseconds
	if c.physics.current.attitudeJerk > 10 {
		c.physics.current.attitudeJerk = 10
	} else if c.physics.current.attitudeJerk < -10 {
		c.physics.current.attitudeJerk = -10
	}

	c.physics.last.attitudeSnap = c.physics.current.attitudeSnap
	c.physics.current.attitudeSnap = (c.physics.current.attitudeJerk - c.physics.last.attitudeJerk) / windowMilliseconds

	// chassis position
	c.physics.current.velocityDelta = vectorDelta(c.physics.current.velocityVector, c.physics.last.velocityVector)
	c.physics.current.acceleration = vectorMagnitudeBiased(c.physics.current.velocityDelta, 0.25, 0.25, 1) / windowMilliseconds

	c.physics.last.jerk = c.physics.current.jerk
	c.physics.current.jerk = (c.physics.current.acceleration - c.physics.last.acceleration) / windowMilliseconds

	c.physics.last.snap = c.physics.current.snap
	c.physics.current.snap = (c.physics.current.jerk - c.physics.last.jerk) / windowMilliseconds

	c.physics.last.crackle = c.physics.current.crackle
	c.physics.current.crackle = (c.physics.current.snap - c.physics.last.snap) / windowMilliseconds

	c.physics.last.sequenceID = c.physics.current.sequenceID

	if seqDelta <= 1 || vectorMagnitude(c.physics.current.velocityVector) > 0.28 {
		c.generateBump(seqDelta)
	}
}

func (c *Core) generateBump(seqDelta uint32) {
	if !c.hapticsOutput.ChassisEnabled {
		for i := 0; i < c.audioBuffer.slotSize; i++ {
			c.audioBuffer.buffer[i] = 0
		}

		return
	}

	bufferLen := c.audioBuffer.slotSize * c.audioBuffer.slots

	if seqDelta == 0 {
		return
	} else if seqDelta < uint32(c.audioBuffer.slots) {
		c.shiftBuffer2(int(seqDelta))
	} else {
		c.log.Warn().Uint32("delta", seqDelta).Msg("sequence ID delta greater than 1")
		for i := 0; i < bufferLen; i++ {
			c.audioBuffer.buffer[i] = 0
		}
	}

	// startTime := time.Now()

	// exponent 0.5, scale 1/47.5 (1/57.0) - small bumps slightly too loud
	// exponent 0.4, scale 1/29.75 (1/36.0) - best balance of small, med and large bumps
	// log10, scale 0.08 - small bumps too loud
	// log2, scale 0.025 - small bumps too loud
	// sig := (c.physics.current.jerk - (c.physics.current.attitudeJerk * 10))
	sig := signal.LargestMagnitude(c.physics.current.jerk, (c.physics.current.attitudeJerk * 50))
	thisAmplitude := signal.Exponent(sig, 0.56)
	thisAmplitude = signal.Scale(thisAmplitude, 1/80.0)
	thisAmplitude = signal.Limit(thisAmplitude, 0.8)

	snap := signal.LargestMagnitude(c.physics.current.snap, (c.physics.current.attitudeSnap * 100))
	snap = signal.Scale(snap, 1/1000.0)

	periodReduction := snap

	maxPulseLen := 133.0
	pulseLen := maxPulseLen

	if periodReduction > 0 {
		pulseLen -= periodReduction
	} else {
		pulseLen += periodReduction
	}

	if pulseLen < 100 {
		pulseLen = 100
	}

	waveOffset := pulseLen / 2
	waveSamplePeriod := math.Pi / pulseLen

	peak := 0.0
	for i := 0; i < bufferLen-1; i++ {
		sineValue := thisAmplitude*math.Sin(waveSamplePeriod*(float64(i)-waveOffset)) + thisAmplitude

		c.audioBuffer.buffer[i] = sineValue

		if sineValue > 0 && sineValue > peak {
			peak = sineValue
		} else if sineValue < 0 && sineValue < peak {
			peak = sineValue
		}
	}

	c.physics.last.audioOutValue = c.physics.current.audioOutValue
	c.physics.current.audioOutValue = peak

	// duration := time.Since(startTime)
	// fmt.Printf("INPUT:  jerk: %0.05f, snap: %0.05f, time: %v seq: %d\n", c.physics.current.jerk, c.physics.current.snap, duration, c.physics.current.sequenceID)
	// fmt.Printf("OUTPUT: peak: %0.05f, amplitude: %0.05f, reduce: %0.05f, samplePeriod: %0.05f, pulseLen: %0.05f, maxPulseLen: %0.05f\n\n", peak, thisAmplitude, periodReduction, waveSamplePeriod, pulseLen, maxPulseLen)
}

func (c *Core) shiftBuffer(slots int) {
	offset := slots * c.audioBuffer.slotSize

	for i := 0; i < offset-1; i++ {
		c.audioBuffer.buffer[i+offset] = c.audioBuffer.buffer[i]
	}
}

func (c *Core) shiftBuffer2(slots int) {
	bufferMax := (c.audioBuffer.slotSize * c.audioBuffer.slots) - 1
	offset := slots * c.audioBuffer.slotSize

	for i := bufferMax - offset; i >= 0; i-- {
		c.audioBuffer.buffer[i+offset] = c.audioBuffer.buffer[i]
	}
}

func (c *Core) shiftBuffer3(slots int) {
	bufferMax := (c.audioBuffer.slotSize * c.audioBuffer.slots) - 1
	offset := slots * c.audioBuffer.slotSize

	for i := 0; i <= bufferMax; i++ {
		if i < bufferMax-offset {
			c.audioBuffer.buffer[i] = c.audioBuffer.buffer[i+offset]
		} else {
			c.audioBuffer.buffer[i] = 0
		}
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
	c.audioBuffer.buffer = zeroedBuffer(c.audioBuffer.slotSize * c.audioBuffer.slots)

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

	if c.timeSinceLive > 3600 {
		c.display.PowerOff()
	}

	if c.gt.Telemetry.Flags().Live == false && c.replayMode == false {
		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, "Waiting...", 16)

		c.timeSinceLive += 1

		return
	}

	c.display.PowerOn()
	c.timeSinceLive = 0

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
			c.audioDevice.Play("gearchange", gain)
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
			"timeOfDay":    float32(c.gt.Telemetry.TimeOfDay().Milliseconds()),
			"throttle":     c.gt.Telemetry.ThrottlePercent(),
			"brake":        c.gt.Telemetry.BrakePercent(),
			"rpm":          c.gt.Telemetry.EngineRPM(),
			"speed":        c.gt.Telemetry.GroundSpeedKPH(),
			"velocityX":    c.physics.current.velocityVector.X,
			"velocityY":    c.physics.current.velocityVector.Y,
			"velocityZ":    c.physics.current.velocityVector.Z,
			"gforce":       float32(c.physics.current.acceleration) / gravityConstant,
			"jerk":         float32(c.physics.current.jerk),
			"snap":         float32(c.physics.current.snap),
			"attitudeJerk": float32(c.physics.current.attitudeJerk),
			"output":       float32(c.physics.current.audioOutValue),
		}
	}()
}
