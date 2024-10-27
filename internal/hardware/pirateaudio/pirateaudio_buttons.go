package pirateaudio

import (
	"fmt"
	"image"

	"github.com/rs/zerolog"
	"github.com/rubiojr/go-pirateaudio/buttons"
	"github.com/vwhitteron/racesig-dev/internal/audio"
	"github.com/vwhitteron/racesig-dev/internal/hardware"
)

const volumeFontSize = 24

func SetupPirateAudioButtons(lcdDevice hardware.LCD, audioDevice *audio.OutputDevice, audioMixer *audio.Mixer, log zerolog.Logger) func() {
	return func() {
		buttons.OnButtonAPressed(func() {
			audioMixer.MasterIncrease(0.5)

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.PowerOn()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("%0.1f dB", audioMixer.Master), volumeFontSize)

			// go func() {
			// 	audioDevice.Play("gearChange", audioMixer.Master)
			// }()

			log.Info().
				Str("button", "A").
				Str("action", "increase master gain").
				Float64("master_gain", audioMixer.Master).
				Msg("button press")
		})

		buttons.OnButtonBPressed(func() {
			audioMixer.MasterDecrease(0.5)

			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.PowerOn()
			lcdDevice.ShowTextCentered(canvas, fmt.Sprintf("%0.1f dB", audioMixer.Master), volumeFontSize)

			// go func() {
			// 	audioDevice.Play("gearChange", audioMixer.Master)
			// }()

			log.Info().
				Str("button", "B").
				Str("action", "decrease master gain").
				Float64("master_gain", audioMixer.Master).
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
				lcdDevice.PowerOn()
			}

			lcdDevice.Show(sprites[index])

			log.Info().
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
				lcdDevice.PowerOff()
				return
			}

			lcdDevice.Show(sprites[index])

			log.Info().
				Str("button", "X").
				Str("action", "show previous sprite").
				Str("sprite", sprites[index]).
				Msg("button press")
		})

		log.Info().Str("component", "pirate audio buttons").Str("result", "success").Msg("button setup complete")
	}
}
