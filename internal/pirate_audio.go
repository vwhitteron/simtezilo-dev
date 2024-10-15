package internal

import (
	"fmt"
	"image"

	"github.com/rubiojr/go-pirateaudio/buttons"
)

func (c *Core) setupPirateAudioButtons() {
	if c.pirateAudioEnabled == false {
		c.log.Debug().Str("component", "pirate audio buttons").Str("result", "skipped").Msg("init")
		return
	}

	buttons.OnButtonAPressed(func() {
		c.mixerGain.MasterIncrease(0.25)

		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, fmt.Sprintf("%0.0f dB", c.mixerGain.Master), volumeFontSize)

		go func() {
			c.audioDevice.Play("gearChange", c.mixerGain.Master)
		}()

		c.log.Info().
			Str("button", "A").
			Str("action", "increase master gain").
			Float64("master_gain", c.mixerGain.Master).
			Msg("button press")
	})

	buttons.OnButtonBPressed(func() {
		c.mixerGain.MasterDecrease(0.25)

		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		c.display.ShowTextCentered(canvas, fmt.Sprintf("%0.0f dB", c.mixerGain.Master), volumeFontSize)

		go func() {
			c.audioDevice.Play("gearChange", c.mixerGain.Master)
		}()

		c.log.Info().
			Str("button", "B").
			Str("action", "decrease master gain").
			Float64("master_gain", c.mixerGain.Master).
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
