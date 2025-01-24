package pirateaudio

import (
	"fmt"
	"image"

	"github.com/rs/zerolog"
	"github.com/rubiojr/go-pirateaudio/buttons"
	"github.com/vwhitteron/simtezilo-dev/internal/config"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
)

const volumeFontSize = 20

var modes = []string{
	"volume",
	"jerk",
	"snap",
	"chassis",
	"gear",
}
var mode = 0

func SetupPirateAudioButtons(lcdDevice hardware.LCD, synth *synth.Synthesizer, config *config.Config, log zerolog.Logger) func() {
	return func() {
		buttons.OnButtonAPressed(func() {
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
			case "snap":
				profile := config.NextSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "next profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
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

		buttons.OnButtonBPressed(func() {
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
			case "snap":
				profile := config.PreviousSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "B").
					Str("action", "previous profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
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

		buttons.OnButtonXPressed(func() {
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
			case "snap":
				displayString = fmt.Sprintf("Snap %d", config.GetSnapProfile())
			case "chassis":
				volume, _ := synth.GetChannelVolume("chassis")
				displayString = fmt.Sprintf("Chassis %d", volume)
			case "gear":
				volume, _ := synth.GetChannelVolume("gearchange")
				displayString = fmt.Sprintf("Gear %d", volume)
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

		buttons.OnButtonYPressed(func() {
			log.Debug().
				Str("button", "Y").
				Str("action", "none").
				Msg("button press")

		})

		// sprites := []string{"splash", "error"}
		// index := 0
		// buttons.OnButtonXPressed(func() {
		// 	index += 1
		// 	if index >= len(sprites) {
		// 		index = len(sprites) - 1
		// 	}

		// 	if index == 0 {
		// 		lcdDevice.PowerOn()
		// 	}

		// 	lcdDevice.Show(sprites[index])

		// 	log.Debug().
		// 		Str("button", "X").
		// 		Str("action", "show next sprite").
		// 		Str("sprite", sprites[index]).
		// 		Msg("button press")
		// })

		// buttons.OnButtonYPressed(func() {
		// 	index -= 1
		// 	if index < -1 {
		// 		index = -1
		// 	}

		// 	if index == -1 {
		// 		lcdDevice.PowerOff()
		// 		return
		// 	}

		// 	lcdDevice.Show(sprites[index])

		// 	log.Debug().
		// 		Str("button", "X").
		// 		Str("action", "show previous sprite").
		// 		Str("sprite", sprites[index]).
		// 		Msg("button press")
		// })

		log.Debug().Str("component", "pirate audio buttons").Str("result", "success").Msg("button setup complete")
	}
}
