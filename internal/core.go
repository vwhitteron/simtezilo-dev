package internal

import (
	"fmt"
	"image"
	"net/http"
	"os"
	"time"

	json "github.com/goccy/go-json"
	"github.com/gopxl/beep/speaker"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/rubiojr/go-pirateaudio/buttons"
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

type displayContent struct {
	gear int
}

type physics struct {
	acceleration   float64
	jerk           float64
	velocityVector telemetry_client.Vector
}

type physicsTracker struct {
	last    physics
	current physics
}

type Core struct {
	assetDir           string
	audio              *AudioOut
	chartDataChannel   chan map[string]float32
	display            Display
	displayContent     displayContent
	done               chan bool
	gearShiftGain      float64
	gt                 *telemetry_client.GTClient
	physics            physicsTracker
	jerk               physics
	lastAcceleration   float64
	lastGear           int
	log                zerolog.Logger
	masterGain         float64
	pirateAudioEnabled bool
	replayMode         bool
	seq                uint32
	streamerGain       float64
	timeOfDay          time.Duration
	timeSinceLive      int
	vehicleID          uint32
	webEnabled         bool
	webSocketClients   int
}

type CoreOptions struct {
	AssetDir           string
	Gain               float64
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
			// display, err = NewSpotpearGameDisplay(SpotpearGameDisplayOpts{
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

	streamerGain := volumeToGain(opts.Gain)

	return &Core{
		assetDir:           opts.AssetDir,
		audio:              audio,
		chartDataChannel:   make(chan map[string]float32, 600),
		display:            display,
		done:               make(chan bool),
		gearShiftGain:      0,
		gt:                 gt,
		log:                log,
		masterGain:         opts.Gain,
		physics:            physicsTracker{},
		lastAcceleration:   0,
		lastGear:           1024,
		pirateAudioEnabled: opts.PirateAudioEnabled,
		replayMode:         opts.ReplayMode,
		seq:                uint32(0),
		streamerGain:       streamerGain,
		timeSinceLive:      1000,
		vehicleID:          0,
		webEnabled:         opts.WebEnabled,
		webSocketClients:   0,
	}, nil

}

func (c *Core) Run() {

	go c.setupButtons()

	go c.gt.Run()

	go StartWebChartServer(c)

	chassisStreamer := NewBumpStream(
		&c.physics,
		&c.streamerGain,
	)
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
		case <-ticker15fps.C:
			c.updateDisplay()
		}
	}
}

func (c *Core) Close() {
	c.display.Close()
}

func (c *Core) setupButtons() {
	if c.pirateAudioEnabled == false {
		c.log.Debug().Str("component", "pirate audio buttons").Str("result", "skipped").Msg("init")
		return
	}

	buttons.OnButtonAPressed(func() {
		c.masterGain += 1
		c.streamerGain = volumeToGain(c.masterGain)

		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, fmt.Sprintf("%0.0f db", c.masterGain), volumeFontSize)

		go func() {
			c.audio.Play("gearChange", c.masterGain)
		}()

		c.log.Info().
			Str("button", "A").
			Str("action", "increase master gain").
			Float64("master_gain", c.masterGain).
			Msg("button press")
	})

	buttons.OnButtonBPressed(func() {
		c.masterGain -= 1
		c.streamerGain = volumeToGain(c.masterGain)

		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, fmt.Sprintf("%0.0f dB", c.masterGain), volumeFontSize)

		go func() {
			c.audio.Play("gearChange", c.masterGain)
		}()

		c.log.Info().
			Str("button", "B").
			Str("action", "decrease master gain").
			Float64("master_gain", c.masterGain).
			Msg("button press")
	})

	sprites := []string{"splash", "error"}
	index := 0
	buttons.OnButtonXPressed(func() {
		index += 1
		if index >= len(sprites) {
			index = len(sprites) - 1
		}

		if index == 0 {
			c.display.PowerOn()
		}

		c.display.Show(sprites[index])

		c.log.Info().
			Str("button", "X").
			Str("action", "show next sprite").
			Str("sprite", sprites[index]).
			Msg("button press")
	})

	buttons.OnButtonYPressed(func() {
		index -= 1
		if index < -1 {
			index = -1
		}

		if index == -1 {
			c.display.PowerOff()
			return
		}

		c.display.Show(sprites[index])

		c.log.Info().
			Str("button", "X").
			Str("action", "show previous sprite").
			Str("sprite", sprites[index]).
			Msg("button press")
	})

	c.log.Info().Str("component", "pirate audio buttons").Str("result", "success").Msg("button setup complete")
}

func (c *Core) chassisHaptics(seqDelta uint32, currentVelocityVector telemetry_client.Vector) {
	velocityDelta := vectorDelta(currentVelocityVector, c.physics.last.velocityVector)
	windowMilliseconds := (float64(seqDelta) / frameRate)

	acceleration := vectorMagnitude(velocityDelta) / windowMilliseconds
	jerk := (acceleration - c.lastAcceleration) / windowMilliseconds

	c.physics.last.jerk = c.physics.current.jerk
	c.physics.current.jerk = jerk

	c.physics.last.velocityVector = currentVelocityVector
	c.lastAcceleration = acceleration
}

func (c *Core) physicsEvents() {
	go c.sendWebTelemetry()

	seq := c.gt.Telemetry.SequenceID()
	currentGear := c.gt.Telemetry.CurrentGear()
	currentVehicleID := c.gt.Telemetry.VehicleID()
	velocityVector := c.gt.Telemetry.VelocityVector()

	// Do nothing until the sequence ID advances
	seqDelta := seq - c.seq
	if seq == 0 || seqDelta == 0 {
		return
	}

	// Initialise the gear if it hasn't been set yet
	if c.lastGear == 1024 {
		c.seq = seq
		c.lastGear = currentGear
		return
	}

	if c.vehicleID != currentVehicleID {
		go c.updateVehicle(currentVehicleID, currentGear)
		return
	}

	// The loading flag typically means the session has restarted
	if c.gt.Telemetry.Flags().Loading == true {
		c.timeOfDay = c.gt.Telemetry.TimeOfDay()
		c.seq = seq

		c.log.Info().
			Uint32("sequence_id", seq).
			Msg("loading flag detected")

		time.Sleep(1000 * time.Millisecond)

		return
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
		return
	}

	c.chassisHaptics(seqDelta, velocityVector)

	c.gearChange(currentGear)

	c.seq = seq
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

	gain := c.gearShiftGain + c.masterGain
	if currentGear != NeutralGear {
		c.audio.Play("gearchange", gain)
	}

	c.log.Info().
		Int("sequence_id", int(c.seq)).
		Int("gear", currentGear).
		Float64("audio_gain", gain).
		Msg("gear change")
}

func (c *Core) checkSessionComplete() {
	if c.gt.Finished {
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
			"jerk":      float32(c.physics.last.jerk),
		}
	}()
}
