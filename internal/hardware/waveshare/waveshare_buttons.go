package waveshare

import (
	"fmt"
	"image"

	"github.com/rs/zerolog"
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

func SetupWaveshareButtons(lcdDevice hardware.LCD, synth *synth.Synthesizer, config *config.Config, log zerolog.Logger) func() {
	return func() {
		hardware.OnButtonUpPressed(func() {
			displayString := ""

			switch modes[mode] {
			case "volume":
				synth.IncreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "up").
					Str("action", "increase master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerk":
				profile := config.NextJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "up").
					Str("action", "next profile").
					Str("type", "jerk").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snap":
				profile := config.NextSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "up").
					Str("action", "next profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "chassis":
				volume, _ := synth.IncreaseChannelVolume("chassis")

				displayString = fmt.Sprintf("Chassis %d", volume)

				log.Debug().
					Str("button", "up").
					Str("action", "increase chassis haptics volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "gear":
				volume, _ := synth.IncreaseChannelVolume("gearchange")

				displayString = fmt.Sprintf("Gear %d", volume)

				log.Debug().
					Str("button", "up").
					Str("action", "increase race gear volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			default:
				log.Debug().
					Str("button", "up").
					Str("action", "none").
					Str("mode", modes[mode]).
					Msg("button press")

				return
			}

			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonDownPressed(func() {
			displayString := ""

			switch modes[mode] {
			case "volume":
				synth.DecreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "down").
					Str("action", "decrease master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerk":
				profile := config.PreviousJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "down").
					Str("action", "previous profile").
					Str("type", "jerk").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snap":
				profile := config.PreviousSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "down").
					Str("action", "previous profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "chassis":
				volume, _ := synth.DecreaseChannelVolume("chassis")

				displayString = fmt.Sprintf("Chassis %d", volume)

				log.Debug().
					Str("button", "down").
					Str("action", "decrease chassis haptics volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "gear":
				volume, _ := synth.DecreaseChannelVolume("gearchange")

				displayString = fmt.Sprintf("Gear %d", volume)

				log.Debug().
					Str("button", "down").
					Str("action", "decrease race gear volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			default:
				log.Debug().
					Str("button", "down").
					Str("action", "none").
					Str("mode", modes[mode]).
					Msg("button press")

				return
			}

			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonLeftPressed(func() {
			if mode > 0 {
				mode--
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
				Str("button", "left").
				Str("action", "previous mode").
				Str("mode", modes[mode]).
				Msg("button press")

			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonRightPressed(func() {
			if mode < len(modes)-1 {
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
				Str("button", "right").
				Str("action", "next mode").
				Str("mode", modes[mode]).
				Msg("button press")

			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonCenterPressed(func() {
			log.Debug().
				Str("button", "Center").
				Str("action", "None").
				Msg("button press")
		})

		hardware.OnButtonOnePressed(func() {
			log.Debug().
				Str("button", "One").
				Str("action", "None").
				Msg("button press")
		})

		hardware.OnButtonTwoPressed(func() {
			orientation := lcdDevice.GetOrientation() + 90
			if orientation >= 360 {
				orientation = 0
			}

			lcdDevice.SetOrientation(orientation)
			log.Debug().
				Str("button", "Two").
				Str("action", "Rotate screen").
				Str("orientation", fmt.Sprintf("%d", orientation)).
				Msg("button press")
		})

		hardware.OnButtonThreePressed(func() {
			isPoweredOn := lcdDevice.PowerToggle()

			log.Debug().
				Str("button", "Three").
				Str("action", "Display Power").
				Str("state", fmt.Sprintf("%t", isPoweredOn)).
				Msg("button press")
		})

		log.Debug().Str("component", "waveshare 14972 buttons").Str("result", "success").Msg("button setup complete")
	}
}
