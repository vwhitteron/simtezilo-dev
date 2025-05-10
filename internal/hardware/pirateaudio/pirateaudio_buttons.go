package pirateaudio

import (
	"fmt"
	"image"
	"log"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/config"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

const volumeFontSize = 20

var modes = []string{
	"volume",
	"jerk",
	"jerkMax",
	"snap",
	"snapMax",
	"chassis",
	"gear",
	"mixAlgo",
}
var mode = 0

func SetupPirateAudioButtons(lcdDevice hardware.LCD, synth *synth.Synthesizer, config *config.Config, log zerolog.Logger) func() {
	return func() {
		OnButtonAPressed(func() {
			displayString := ""

			switch modes[mode] {
			case "volume":
				synth.IncreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "A").
					Str("action", "increase master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerk":
				profile := config.NextJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "next profile").
					Str("type", "jerk").
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
			case "snap":
				profile := config.NextSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "next profile").
					Str("type", "snap").
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
					Str("mode", modes[mode]).
					Msg("button press")

				return
			}

			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		OnButtonBPressed(func() {
			displayString := ""

			switch modes[mode] {
			case "volume":
				synth.DecreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerk":
				profile := config.PreviousJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "B").
					Str("action", "previous profile").
					Str("type", "jerk").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "jerkMax":
				jerkMax := config.DecreaseJerkMax()

				displayString = fmt.Sprintf("JMax %d", jerkMax)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "jerk max").
					Str("value", fmt.Sprintf("%d", jerkMax)).
					Msg("button press")
			case "snap":
				profile := config.PreviousSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "B").
					Str("action", "previous profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
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
					Str("mode", modes[mode]).
					Msg("button press")

				return
			}

			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)

		})

		OnButtonXPressed(func() {
			if mode >= len(modes)-1 {
				mode = 0
			} else {
				mode++
			}

			displayString := ""

			switch modes[mode] {
			case "volume":
				displayString = fmt.Sprintf("%0.2f dB", synth.GetMasterGain())
			case "jerk":
				displayString = fmt.Sprintf("Jerk %d", config.GetJerkProfile())
			case "jerkMax":
				displayString = fmt.Sprintf("JMax %d", config.GetJerkMax())
			case "snap":
				displayString = fmt.Sprintf("Snap %d", config.GetSnapProfile())
			case "snapMax":
				displayString = fmt.Sprintf("SMax %d", config.GetSnapMax())
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
				Str("mode", modes[mode]).
				Msg("button press")

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
			time.Sleep(200 * time.Millisecond)
		}
	}()
}
