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
	"github.com/vwhitteron/gt-pi/internal/audio"
	"github.com/vwhitteron/gt-pi/internal/hardware"
	"github.com/vwhitteron/gt-pi/internal/hardware/nulldevice"
	"github.com/vwhitteron/gt-pi/internal/hardware/pirateaudio"
	"github.com/vwhitteron/gt-pi/internal/hardware/waveshare"
	"github.com/vwhitteron/gt-pi/internal/physics"
	"github.com/vwhitteron/gt-pi/internal/physics/symmetryaxis"
	"github.com/vwhitteron/gt-pi/internal/physics/vector"
	"github.com/vwhitteron/gt-pi/internal/signal"
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

type displayContent struct {
	gear int
}

type Core struct {
	assetDir         string
	audioDevice      *audio.OutputDevice
	audioBuffer      audio.AudioBuffer
	buttonsFn        func()
	chartDataChannel chan map[string]float32
	lcdDevice        hardware.LCD
	displayContent   displayContent
	done             chan bool
	gt               *telemetry_client.GTClient
	physics          physics.PhysicsTracker
	lastGear         int
	log              zerolog.Logger
	audioMixer       audio.Mixer
	replayMode       bool
	seq              uint32
	timeOfDay        time.Duration
	lastLive         time.Time
	vehicleID        uint32
	webEnabled       bool
	webSocketClients int
}

type CoreOptions struct {
	AssetDir    string
	Done        chan bool
	Gain        float64
	LogLevel    string
	Orientation int
	Hardware    string
	ReplayMode  bool
	Source      string
	WebEnabled  bool
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

	audioMixer := audio.NewAudioMixer(opts.Gain, log)
	audioBuffer := audio.NewAudioBuffer(133, 20)
	audioDevice, err := audio.NewAudioOutputDevice(audio.AudioOutDeviceOpts{
		AssetDir: opts.AssetDir,
		Logger:   log.With().Str("component", "audio").Logger(),
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "audio output device").
			Str("result", "failure").
			Msg("init")

		// lcd.Show("error")

		return nil, err
	}

	var lcdDevice hardware.LCD
	buttonsFn := func() {}

	switch opts.Hardware {
	case "pirateaudio":
		var err error
		lcdDevice, err = pirateaudio.NewPirateAudioLCD(pirateaudio.PirateAudioLCDOpts{
			Orientation: opts.Orientation,
			AssetDir:    opts.AssetDir,
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

		buttonsFn = pirateaudio.SetupPirateAudioButtons(lcdDevice, audioDevice, audioMixer, log)
	case "waveshare":
		var err error
		lcdDevice, err = waveshare.NewWaveshare14972Display(waveshare.Waveshare14972LCDOpts{
			Orientation: opts.Orientation,
			AssetDir:    opts.AssetDir,
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

		buttonsFn = waveshare.SetupWaveshareButtons(lcdDevice, audioDevice, audioMixer, log)
	default:
		lcdDevice = nulldevice.NewNullDisplay()
		log.Debug().
			Str("component", "null display").
			Str("result", "success").
			Msg("init")
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

		lcdDevice.Show("error")

		return nil, err
	}

	log.Info().
		Str("component", "core").
		Str("result", "success").
		Msg("init")

	lcdDevice.Show("splash")

	return &Core{
		assetDir:         opts.AssetDir,
		audioBuffer:      audioBuffer,
		audioDevice:      audioDevice,
		audioMixer:       audioMixer,
		buttonsFn:        buttonsFn,
		chartDataChannel: make(chan map[string]float32, 600),
		done:             opts.Done,
		gt:               gt,
		lastGear:         1024,
		lcdDevice:        lcdDevice,
		log:              log,
		physics:          physics.PhysicsTracker{},
		replayMode:       opts.ReplayMode,
		seq:              uint32(0),
		lastLive:         time.Time{},
		vehicleID:        0,
		webEnabled:       opts.WebEnabled,
		webSocketClients: 0,
	}, nil

}

func (c *Core) Run() {
	go c.buttonsFn()

	go c.gt.Run()

	go StartWebChartServer(c)

	chassisStreamer := audio.NewBumpStream(
		&c.audioBuffer,
		&c.physics,
		&c.audioMixer.Streamer,
	)
	// speaker.Init(44100, 44100/15)
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
	if c.lastGear == 1024 {
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

	c.audioMixer.FadeInHaptics()

	c.physics.Current.SequenceID = seq

	windowMilliseconds := (float64(seqDelta) / frameRate)

	c.chassisHaptics(windowMilliseconds, seqDelta)

	c.gearChange(currentGear)

	c.seq = seq
	c.physics.Last = c.physics.Current
}

func (c *Core) chassisHaptics(windowMilliseconds float64, seqDelta uint32) {
	c.updatePhysics(windowMilliseconds)

	// no haptics if sequence ID has not advanced
	if seqDelta < 1 {
		return
	}

	// no haptics if telemetry packets dropped/missed
	if seqDelta > 1 {
		c.log.Debug().Uint32("delta", seqDelta).Msg("missed packets, skipping haptics")

		return
	}

	// no haptics if vehicle speed velocity lower than 30cm per second
	if vector.Magnitude(c.physics.Current.VelocityVector) < 0.28 {
		return
	}

	c.generateBump(seqDelta)
}

func (c *Core) updatePhysics(windowMilliseconds float64) {
	c.physics.Current.VelocityVector = c.gt.Telemetry.VelocityVector()
	c.physics.Current.AttitudeVector = c.gt.Telemetry.RotationVector()

	// chassis attitude
	c.physics.Current.AttitudeDelta = symmetryaxis.Delta(c.physics.Current.AttitudeVector, c.physics.Last.AttitudeVector)
	c.physics.Current.AttitudeAcceleration = symmetryaxis.Magnitude(c.physics.Current.AttitudeDelta) / windowMilliseconds

	c.physics.Last.AttitudeJerk = c.physics.Current.AttitudeJerk
	c.physics.Current.AttitudeJerk = (c.physics.Current.AttitudeAcceleration - c.physics.Last.AttitudeAcceleration) / windowMilliseconds
	if c.physics.Current.AttitudeJerk > 10 {
		c.physics.Current.AttitudeJerk = 10
	} else if c.physics.Current.AttitudeJerk < -10 {
		c.physics.Current.AttitudeJerk = -10
	}

	c.physics.Last.AttitudeSnap = c.physics.Current.AttitudeSnap
	c.physics.Current.AttitudeSnap = (c.physics.Current.AttitudeJerk - c.physics.Last.AttitudeJerk) / windowMilliseconds

	// chassis position
	c.physics.Current.VelocityDelta = vector.Delta(c.physics.Current.VelocityVector, c.physics.Last.VelocityVector)
	// FIXME: not sure if the xy biasing helps at all
	// biaseddVelocityDelta := vector.Scale(c.physics.Current.VelocityDelta, 0.25, 0.25, 1)
	// c.physics.Current.Acceleration = vector.Magnitude(biaseddVelocityDelta) / windowMilliseconds
	c.physics.Current.Acceleration = vector.Magnitude(c.physics.Current.VelocityDelta) / windowMilliseconds

	c.physics.Last.Jerk = c.physics.Current.Jerk
	c.physics.Current.Jerk = (c.physics.Current.Acceleration - c.physics.Last.Acceleration) / windowMilliseconds

	c.physics.Last.Snap = c.physics.Current.Snap
	c.physics.Current.Snap = (c.physics.Current.Jerk - c.physics.Last.Jerk) / windowMilliseconds

	c.physics.Last.Crackle = c.physics.Current.Crackle
	c.physics.Current.Crackle = (c.physics.Current.Snap - c.physics.Last.Snap) / windowMilliseconds

	c.physics.Last.SequenceID = c.physics.Current.SequenceID
}

func (c *Core) generateBump(seqDelta uint32) {
	bufferLen := c.audioBuffer.GetLength()

	if seqDelta == 0 {
		return
		// FIXME: probably no longer needed as the buffer is now shifted by the audio streamer
		// } else if seqDelta < uint32(c.audioBuffer.Slots) {
		// 	c.audioBuffer.ShiftBufferSlots(int(seqDelta))
		// } else {
		// 	c.log.Warn().Uint32("delta", seqDelta).Msg("sequence ID delta greater than 1")
		// 	for i := 0; i < bufferLen; i++ {
		// 		c.audioBuffer.Buffer[i] = 0
		// 	}
	}

	startTime := time.Now()

	// exponent 0.5, scale 1/47.5 (1/57.0) - small bumps slightly too loud
	// exponent 0.4, scale 1/29.75 (1/36.0) - best balance of small, med and large bumps
	// log10, scale 0.08 - small bumps too loud
	// log2, scale 0.025 - small bumps too loud
	// sig := (c.physics.Current.jerk - (c.physics.Current.attitudeJerk * 10))
	sig := signal.LargestMagnitude(c.physics.Current.Jerk, (c.physics.Current.AttitudeJerk * 50))
	pulse := signal.Exponent(sig, 0.56)
	pulse = signal.Scale(pulse, 1/36.0)
	p1 := pulse
	pulse = signal.Limit(pulse, 1.0)

	if pulse < p1 {
		c.log.Debug().Float64("pulse", p1).Msg("limiter")
	}

	snap := signal.LargestMagnitude(c.physics.Current.Snap, (c.physics.Current.AttitudeSnap * 100))

	pulseWidthMax := 143.0
	pulseWidth := pulseWidthMax

	pulseReduction := signal.Abs(signal.Scale(snap, 1/1000.0))
	// pulseReduction := signal.Abs(snap)
	pulseWidth -= pulseReduction

	// 50Hz max
	if pulseWidth < 80 {
		pulseWidth = 80
	}

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	pulseBuffer := make([]float64, bufferLen)
	for i := 0; i < int(pulseWidth*2); i++ {
		pulseBuffer[i] = (pulse*math.Sin(waveSamplePeriod*(float64(i)-waveOffset)) + pulse) / 2
	}

	c.audioBuffer.Write(pulseBuffer)

	c.physics.Last.AudioOutValue = c.physics.Current.AudioOutValue
	// FIXME: temporarily report output frequency in telemetry dashboard
	// freq := 1 / ((2 * pulseWidth) / 8000)
	// c.physics.Current.AudioOutValue = freq
	c.physics.Current.AudioOutValue = pulse

	if pulse > 1.0 {
		c.log.Debug().
			Float64("jerk", c.physics.Current.Jerk).
			Float64("snap", c.physics.Current.Snap).
			Str("process_time", time.Since(startTime).String()).
			Uint32("sequence_id", c.physics.Current.SequenceID).
			Msg("Bump inputs")
		c.log.Debug().
			Float64("amplitude", pulse).
			Float64("pulseReduce", pulseReduction).
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

	c.audioMixer.SetFader(-30)
	c.audioMixer.SetFadeInTime(frameRate * 2)

	c.physics.Last = physics.Physics{}
	c.physics.Current = physics.Physics{}

	c.audioBuffer.ClearBuffer()

	return
}

func (c *Core) silenceHaptics() {
	c.audioBuffer.ClearBuffer()

	c.audioMixer.SetFader(-30)
	c.audioMixer.SetFadeInTime(frameRate * 2)

	return
}

func (c *Core) updateDisplay() {
	currentGear := c.lastGear

	if c.displayContent.gear == currentGear {
		return
	}

	if c.gt.Telemetry.Flags().Live == false && c.replayMode == false {
		if time.Since(c.lastLive) > 10*time.Second {
			c.lcdDevice.PowerOff()

			return
		}

		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.lcdDevice.ShowTextCentered(canvas, "Waiting...", 16)

		return
	}

	c.lcdDevice.PowerOn()
	c.lastLive = time.Now()

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

	switch vehicleType {
	case "race":
		c.audioMixer.SetGearChangeGain(0)
	case "street":
		c.audioMixer.SetGearChangeGain(-2)
	default:
		c.audioMixer.SetGearChangeGain(-2)
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

	gain := c.audioMixer.GetGearChangeGain()
	if currentGear != NeutralGear {
		c.audioDevice.Play("gearchange", gain)
	}

	c.log.Info().
		Int("sequence_id", int(c.seq)).
		Int("gear", currentGear).
		Float64("audio_gain", gain).
		Msg("gear change")
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
			"velocityX":    c.physics.Current.VelocityVector.X,
			"velocityY":    c.physics.Current.VelocityVector.Y,
			"velocityZ":    c.physics.Current.VelocityVector.Z,
			"gforce":       float32(c.physics.Current.Acceleration) / gravityConstant,
			"jerk":         float32(c.physics.Current.Jerk),
			"snap":         float32(c.physics.Current.Snap),
			"attitudeJerk": float32(c.physics.Current.AttitudeJerk),
			"output":       float32(c.physics.Current.AudioOutValue),
		}
	}()
}
