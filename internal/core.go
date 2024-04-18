package internal

import (
	"image"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/go-pirateaudio/buttons"
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

type Core struct {
	audio              *AudioOut
	display            Display
	gear               int
	gearShiftGain      float64
	gt                 *telemetry_client.GTClient
	log                zerolog.Logger
	masterGain         float64
	pirateAudioEnabled bool
	replayMode         bool
	vehicleID          uint32
}

type CoreOptions struct {
	AssetDir           string
	PirateAudioEnabled bool
	Gain               float64
	LogLevel           string
	Orientation        int
	ReplayMode         bool
	Source             string
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
		log.Debug().Msg("initialising pirate audio display")
		var err error
		display, err = NewPirateAudioDisplay(DisplayOpts{
			Orientation: opts.Orientation,
			AssetDir:    opts.AssetDir,
		})
		if err != nil {
			log.Error().Msg("error initialising Pirate Audio display")

			return nil, err
		}
	} else {
		log.Debug().Msg("pirate audio features disabled, initialising null display")
		display = NewNullDisplay()
	}

	audio, err := NewAudioOutputDevice(AudioOutOpts{
		AssetDir: opts.AssetDir,
		Logger:   log.With().Str("component", "audio").Logger(),
	})
	if err != nil {
		log.Error().Msg("error initialising audio output device")
		display.Show("error")

		return nil, err
	}

	gt, err := telemetry_client.NewGTClient(telemetry_client.GTClientOpts{
		Source:   opts.Source,
		Logger:   &log,
		LogLevel: opts.LogLevel,
	})
	if err != nil {
		log.Error().Msg("error initialising GT client")
		display.Show("error")

		return nil, err
	}

	log.Info().Msg("initialised core")

	display.Show("splash")

	return &Core{
		audio:              audio,
		display:            display,
		gear:               1024,
		gearShiftGain:      0,
		gt:                 gt,
		log:                log,
		masterGain:         opts.Gain,
		pirateAudioEnabled: opts.PirateAudioEnabled,
		replayMode:         opts.ReplayMode,
		vehicleID:          0,
	}, nil

}

func (c *Core) Run() {
	c.log.Info().Msg("starting main loop")

	go c.setupButtons()

	go c.gt.Run()

	ticker := time.NewTicker(16 * time.Millisecond)
	done := make(chan bool)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			c.updateDisplay()
		}
	}
}

func (c *Core) setupButtons() {
	if c.pirateAudioEnabled == false {
		c.log.Debug().Msg("pirate audio features disabled, skipping button setup")
		return
	}

	c.log.Info().Msg("setting up buttons")

	buttons.OnButtonAPressed(func() {
		c.masterGain += 1

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

		go func() {
			c.audio.Play("gearChange", c.masterGain)
		}()

		c.log.Info().
			Str("button", "B").
			Str("action", "decrease master gain").
			Float64("master_gain", c.masterGain).
			Msg("button press")
	})

	sprites := []string{"splash", "error", "gearN", "gearR", "gear1", "gear2", "gear3", "gear4", "gear5", "gear6", "gear7", "gear8"}
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
}

func (c *Core) updateDisplay() {
	currentGear := c.gt.Telemetry.CurrentGear()
	currentVehicleID := c.gt.Telemetry.VehicleID()

	if c.gear == 1024 {
		c.gear = currentGear
		return
	}

	if c.vehicleID != currentVehicleID {
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
			c.gearShiftGain = -3
		case "race":
			c.gearShiftGain = 0
		default:
			c.gearShiftGain = -3
		}
		c.gear = currentGear
	}

	if currentGear != c.gear {
		c.gear = currentGear

		if c.gt.Telemetry.Flags().Loading == true {
			return
		}

		if c.replayMode == false && c.gt.Telemetry.Flags().Live == false {
			return
		}

		gain := c.gearShiftGain + c.masterGain
		c.log.Info().
			Int("gear", currentGear).
			Float64("audio_gain", gain).
			Msg("gear change")

		go func() {
			if currentGear != 15 { // Neutral gear
				c.audio.Play("gearChange", gain)
			}
		}()

		gearName := ""
		switch currentGear {
		case 15:
			gearName = "N"
		case 0:
			gearName = "R"
		default:
			gearName = strconv.Itoa(currentGear)
		}
		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, gearName)
	}
}

func (c *Core) Close() {
	c.display.Close()
}
