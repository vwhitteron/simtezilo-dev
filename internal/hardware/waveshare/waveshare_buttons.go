package waveshare

import (
	"fmt"
	"image"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/hardware"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
)

const volumeFontSize = 24

func SetupWaveshareButtons(lcdDevice hardware.LCD, synth *synth.Synthesizer, log zerolog.Logger) func() {
	return func() {
		hardware.OnButtonUpPressed(func() {
			// synthMixer.MasterIncrease(1)
			synth.IncreaseMasterGain()

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			masterGain := synth.GetMasterGain()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("%0.0f db", masterGain), volumeFontSize)

			log.Debug().
				Str("button", "Up").
				Str("action", "increase master gain").
				Float64("master_gain", masterGain).
				Msg("button press")
		})

		hardware.OnButtonDownPressed(func() {
			synth.DecreaseMasterGain()

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			masterGain := synth.GetMasterGain()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("%0.0f db", masterGain), volumeFontSize)

			log.Debug().
				Str("button", "Down").
				Str("action", "decrease master gain").
				Float64("master_gain", masterGain).
				Msg("button press")
		})

		sprites := []string{"splash", "error"}
		index := 0
		hardware.OnButtonLeftPressed(func() {
			index += 1
			if index >= len(sprites) {
				index = len(sprites) - 1
			}

			if index == 0 {
				lcdDevice.PowerOn()
				log.Debug().
					Str("button", "Left").
					Str("action", "backlight on").
					Str("sprite", sprites[index]).
					Msg("button press")

				return
			}

			lcdDevice.Show(sprites[index])

			log.Debug().
				Str("button", "Left").
				Str("action", "show next sprite").
				Str("sprite", sprites[index]).
				Msg("button press")
		})

		hardware.OnButtonRightPressed(func() {
			index -= 1
			if index < -1 {
				index = -1
			}

			if index == -1 {
				lcdDevice.PowerOff()
				log.Debug().
					Str("button", "Right").
					Str("action", "backlight off").
					Msg("button press")

				return
			}

			lcdDevice.Show(sprites[index])

			log.Debug().
				Str("button", "Right").
				Str("action", "show previous sprite").
				Str("sprite", sprites[index]).
				Msg("button press")
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
			lcdDevice.PowerOff()

			log.Debug().
				Str("button", "Three").
				Str("action", "Exit").
				Msg("button press")

			// c.done <- true
		})

		log.Debug().Str("component", "waveshare 14972 buttons").Str("result", "success").Msg("button setup complete")
	}
}
