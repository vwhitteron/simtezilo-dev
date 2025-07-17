package pirateaudio

import (
	"fmt"
	"image"
	"log"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

const volumeFontSize = 20

var screens = []string{
	"volume",
	// "jerkProfile",
	"jerkExp",
	"jerkMax",
	// "snapProfile",
	"snapExp",
	"snapMax",
	"minHz",
	"maxHz",
	"chassis",
	"gear",
	"mixAlgo",
}
var currentScreen = 0

func SetupPirateAudioButtons(lcdDevice hardware.LCD, synth *synth.Synthesizer, config *config.Config, lastActive *time.Time, log zerolog.Logger) func() {
	return func() {
		OnButtonAPressed(func() {
			displayString := ""

			switch screens[currentScreen] {
			case "volume":
				synth.IncreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "A").
					Str("action", "increase master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerkProfile":
				profile := config.NextJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "next profile").
					Str("type", "jerk").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "jerkExp":
				profile := config.IncreaseJerkExponent()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "jerk exp").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "jerkMax":
				jerkMax := config.IncreaseJerkMax()

				displayString = fmt.Sprintf("JMax %d", jerkMax)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "jerk max").
					Str("value", fmt.Sprintf("%d", jerkMax)).
					Msg("button press")
			case "snapProfile":
				profile := config.NextSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "next profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snapExp":
				profile := config.IncreaseSnapExponent()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "snap exp").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snapMax":
				snapMax := config.IncreaseSnapMax()

				displayString = fmt.Sprintf("SMax %d", snapMax)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "snap max").
					Str("value", fmt.Sprintf("%d", snapMax)).
					Msg("button press")
			case "minHz":
				minHz := config.IncreaseMinHz()

				displayString = fmt.Sprintf("FMin %d", minHz)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "min frequency").
					Str("value", fmt.Sprintf("%d", minHz)).
					Msg("button press")
			case "maxHz":
				maxHz := config.IncreaseMaxHz()

				displayString = fmt.Sprintf("FMax %d", maxHz)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "max frequency").
					Str("value", fmt.Sprintf("%d", maxHz)).
					Msg("button press")
			case "chassis":
				volume, _ := synth.IncreaseChannelVolume("chassis")

				displayString = fmt.Sprintf("Chassis %d", volume)

				log.Debug().
					Str("button", "A").
					Str("action", "increase chassis haptics volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "gear":
				volume, _ := synth.IncreaseChannelVolume("gearchange")

				displayString = fmt.Sprintf("Gear %d", volume)

				log.Debug().
					Str("button", "A").
					Str("action", "increase race gear volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "mixAlgo":
				algo := synth.Mixer.NextAlgorithm()

				displayString = fmt.Sprintf("Mix %s", algo)

				log.Debug().
					Str("button", "A").
					Str("action", "next mix algorithm").
					Str("algorithm", algo).
					Msg("button press")
			default:
				log.Debug().
					Str("button", "A").
					Str("action", "none").
					Str("mode", screens[currentScreen]).
					Msg("button press")

				return
			}

			*lastActive = time.Now()
			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		OnButtonBPressed(func() {
			displayString := ""

			switch screens[currentScreen] {
			case "volume":
				synth.DecreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerkProfile":
				profile := config.PreviousJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "B").
					Str("action", "previous profile").
					Str("type", "jerk").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "jerkExp":
				value := config.DecreaseJerkExponent()

				displayString = fmt.Sprintf("Jerk %d", value)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "jerk exp").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "jerkMax":
				value := config.DecreaseJerkMax()

				displayString = fmt.Sprintf("JMax %d", value)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "jerk max").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "snapProfile":
				profile := config.PreviousSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "B").
					Str("action", "previous profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snapExp":
				value := config.DecreaseSnapExponent()

				displayString = fmt.Sprintf("Snap %d", value)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "snap exp").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "snapMax":
				snapMax := config.DecreaseSnapMax()

				displayString = fmt.Sprintf("SMax %d", snapMax)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "snap max").
					Str("value", fmt.Sprintf("%d", snapMax)).
					Msg("button press")
			case "minHz":
				minHz := config.DecreaseMinHz()

				displayString = fmt.Sprintf("FMin %d", minHz)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "min frequency").
					Str("value", fmt.Sprintf("%d", minHz)).
					Msg("button press")
			case "maxHz":
				maxHz := config.DecreaseMaxHz()

				displayString = fmt.Sprintf("FMax %d", maxHz)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "max frequency").
					Str("value", fmt.Sprintf("%d", maxHz)).
					Msg("button press")
			case "chassis":
				volume, _ := synth.DecreaseChannelVolume("chassis")

				displayString = fmt.Sprintf("Chassis %d", volume)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease chassis haptics volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "gear":
				volume, _ := synth.DecreaseChannelVolume("gearchange")

				displayString = fmt.Sprintf("Gear %d", volume)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease race gear volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "mixAlgo":
				algo := synth.Mixer.PreviousAlgorithm()

				displayString = fmt.Sprintf("Mix %s", algo)

				log.Debug().
					Str("button", "B").
					Str("action", "previous mix algorithm").
					Str("algorithm", algo).
					Msg("button press")
			default:
				log.Debug().
					Str("button", "B").
					Str("action", "none").
					Str("mode", screens[currentScreen]).
					Msg("button press")

				return
			}

			*lastActive = time.Now()
			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)

		})

		OnButtonXPressed(func() {
			if currentScreen >= len(screens)-1 {
				currentScreen = 0
			} else {
				currentScreen++
			}

			displayString := ""

			switch screens[currentScreen] {
			case "volume":
				displayString = fmt.Sprintf("%0.2f dB", synth.GetMasterGain())
			case "jerkProfile":
				displayString = fmt.Sprintf("Jerk %d", config.GetJerkProfile())
			case "jerkExp":
				displayString = fmt.Sprintf("Jerk %d", int(config.GetJerkExponent()*1000.0))
			case "jerkMax":
				displayString = fmt.Sprintf("JMax %d", config.GetJerkMax())
			case "snapProfile":
				displayString = fmt.Sprintf("Snap %d", config.GetSnapProfile())
			case "snapExp":
				displayString = fmt.Sprintf("Snap %d", int(config.GetSnapExponent()*1000.0))
			case "snapMax":
				displayString = fmt.Sprintf("SMax %d", config.GetSnapMax())
			case "minHz":
				displayString = fmt.Sprintf("FMin %d", int(config.GetMinHz()))
			case "maxHz":
				displayString = fmt.Sprintf("FMax %d", int(config.GetMaxHz()))
			case "chassis":
				volume, _ := synth.GetChannelVolume("chassis")
				displayString = fmt.Sprintf("Chassis %d", volume)
			case "gear":
				volume, _ := synth.GetChannelVolume("gearchange")
				displayString = fmt.Sprintf("Gear %d", volume)
			case "mixAlgo":
				displayString = fmt.Sprintf("Mix %s", synth.Mixer.GetAlgorithm())
			}

			log.Debug().
				Str("button", "X").
				Str("action", "next mode").
				Str("mode", screens[currentScreen]).
				Msg("button press")

			*lastActive = time.Now()
			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		OnButtonYPressed(func() {
			log.Debug().
				Str("button", "Y").
				Str("action", "none").
				Msg("button press")

		})

		log.Debug().Str("component", "pirate audio buttons").Str("result", "success").Msg("button setup complete")
	}
}

func init() {
	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}
}

func OnButtonAPressed(fn func()) {
	onButtonPressed(5, fn)
}

func OnButtonBPressed(fn func()) {
	onButtonPressed(6, fn)
}

func OnButtonXPressed(fn func()) {
	onButtonPressed(16, fn)
}

func OnButtonYPressed(fn func()) {
	onButtonPressed(24, fn)
}

func onButtonPressed(n int, fn func()) {
	go func() {
		p := gpioreg.ByName(fmt.Sprintf("GPIO%d", n))
		if err := p.In(gpio.PullUp, gpio.FallingEdge); err != nil {
			log.Fatal(err)
		}

		for {
			p.WaitForEdge(-1)
			fn()
			time.Sleep(250 * time.Millisecond)
		}
	}()
}
