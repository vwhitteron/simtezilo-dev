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

func SetupPirateAudioButtons(lcdDevice hardware.LCD, synth *synth.Synthesizer, config *config.Config, log zerolog.Logger) func() {
	return func() {
		// buttons.OnButtonAPressed(func() {
		// 	synth.IncreaseMasterGain()

		// 	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		// 	lcdDevice.PowerOn()
		// 	masterGain := synth.GetMasterGain()
		// 	lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("%0.2f dB", masterGain), volumeFontSize)

		// 	log.Info().
		// 		Str("button", "A").
		// 		Str("action", "increase master gain").
		// 		Float64("master_gain", masterGain).
		// 		Msg("button press")
		// })

		// buttons.OnButtonBPressed(func() {
		// 	synth.DecreaseMasterGain()

		// 	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		// 	lcdDevice.PowerOn()
		// 	masterGain := synth.GetMasterGain()
		// 	lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("%0.2f dB", masterGain), volumeFontSize)

		// 	log.Info().
		// 		Str("button", "B").
		// 		Str("action", "decrease master gain").
		// 		Float64("master_gain", masterGain).
		// 		Msg("button press")
		// })

		buttons.OnButtonAPressed(func() {
			profile := config.NextJerkProfile()

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.PowerOn()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("Jerk %d", profile), volumeFontSize)

			log.Info().
				Str("button", "A").
				Str("action", "next profile").
				Str("type", "jerk").
				Str("profile", fmt.Sprintf("%d", profile)).
				Msg("button press")
		})

		buttons.OnButtonBPressed(func() {
			profile := config.PreviousJerkProfile()

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.PowerOn()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("Jerk %d", profile), volumeFontSize)

			log.Info().
				Str("button", "B").
				Str("action", "previous profile").
				Str("type", "jerk").
				Str("profile", fmt.Sprintf("%d", profile)).
				Msg("button press")
		})

		buttons.OnButtonXPressed(func() {
			profile := config.NextSnapProfile()

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.PowerOn()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("Snap %d", profile), volumeFontSize)

			log.Info().
				Str("button", "X").
				Str("action", "next profile").
				Str("type", "snap").
				Str("profile", fmt.Sprintf("%d", profile)).
				Msg("button press")
		})

		buttons.OnButtonYPressed(func() {
			profile := config.PreviousSnapProfile()

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.PowerOn()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("Snap %d", profile), volumeFontSize)

			log.Info().
				Str("button", "Y").
				Str("action", "previous profile").
				Str("type", "snap").
				Str("profile", fmt.Sprintf("%d", profile)).
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

		// 	log.Info().
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

		// 	log.Info().
		// 		Str("button", "X").
		// 		Str("action", "show previous sprite").
		// 		Str("sprite", sprites[index]).
		// 		Msg("button press")
		// })

		log.Info().Str("component", "pirate audio buttons").Str("result", "success").Msg("button setup complete")
	}
}
