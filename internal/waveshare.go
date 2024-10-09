package internal

import (
	"fmt"
	"image"

	"github.com/vwhitteron/gt-pi/internal/buttons"
)

func (c *Core) setupWaveshareButtons() {
	if c.pirateAudioEnabled == false {
		c.log.Debug().Str("component", "waveshare 14972 buttons").Str("result", "skipped").Msg("init")
		return
	}

	buttons.OnButtonUpPressed(func() {
		c.mixerGain.master += 1
		c.mixerGain.fader = c.mixerGain.master
		c.mixerGain.streamer = volumeToGain(c.mixerGain.fader)

		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, fmt.Sprintf("%0.0f db", c.mixerGain.master), volumeFontSize)

		go func() {
			c.audioDevice.Play("gearChange", c.mixerGain.master)
		}()

		c.log.Info().
			Str("button", "Up").
			Str("action", "increase master gain").
			Float64("master_gain", c.mixerGain.master).
			Msg("button press")
	})

	buttons.OnButtonDownPressed(func() {
		c.mixerGain.master -= 1
		c.mixerGain.fader = c.mixerGain.master
		c.mixerGain.streamer = volumeToGain(c.mixerGain.fader)

		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, fmt.Sprintf("%0.0f dB", c.mixerGain.master), volumeFontSize)

		go func() {
			c.audioDevice.Play("gearChange", c.mixerGain.master)
		}()

		c.log.Info().
			Str("button", "Down").
			Str("action", "decrease master gain").
			Float64("master_gain", c.mixerGain.master).
			Msg("button press")
	})

	sprites := []string{"splash", "error"}
	index := 0
	buttons.OnButtonLeftPressed(func() {
		index += 1
		if index >= len(sprites) {
			index = len(sprites) - 1
		}

		if index == 0 {
			c.display.PowerOn()
			c.log.Info().
				Str("button", "Left").
				Str("action", "backlight on").
				Str("sprite", sprites[index]).
				Msg("button press")

			return
		}

		c.display.Show(sprites[index])

		c.log.Info().
			Str("button", "Left").
			Str("action", "show next sprite").
			Str("sprite", sprites[index]).
			Msg("button press")
	})

	buttons.OnButtonRightPressed(func() {
		index -= 1
		if index < -1 {
			index = -1
		}

		if index == -1 {
			c.display.PowerOff()
			c.log.Info().
				Str("button", "Right").
				Str("action", "backlight off").
				Msg("button press")

			return
		}

		c.display.Show(sprites[index])

		c.log.Info().
			Str("button", "Right").
			Str("action", "show previous sprite").
			Str("sprite", sprites[index]).
			Msg("button press")
	})

	buttons.OnButtonCenterPressed(func() {
		c.log.Info().
			Str("button", "Center").
			Str("action", "None").
			Msg("button press")
	})

	buttons.OnButtonOnePressed(func() {
		c.log.Info().
			Str("button", "One").
			Str("action", "None").
			Msg("button press")
	})

	buttons.OnButtonTwoPressed(func() {
		orientation := c.display.GetOrientation() + 90
		if orientation >= 360 {
			orientation = 0
		}

		c.display.SetOrientation(orientation)
		c.log.Info().
			Str("button", "Two").
			Str("action", "Rotate screen").
			Str("orientation", fmt.Sprintf("%d", orientation)).
			Msg("button press")
	})

	buttons.OnButtonThreePressed(func() {
		c.display.PowerOff()

		c.log.Info().
			Str("button", "Three").
			Str("action", "Exit").
			Msg("button press")

		c.done <- true
	})

	c.log.Info().Str("component", "waveshare 14972 buttons").Str("result", "success").Msg("button setup complete")
}
