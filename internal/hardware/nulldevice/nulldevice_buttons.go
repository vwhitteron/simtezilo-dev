package nulldevice

import (
	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
)

func SetupNullDeviceButtons(synthMixer *synth.Mixer, done chan bool, log zerolog.Logger) func() {

	return func() {
		keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			switch key.Code {
			case keys.CtrlC, keys.Escape:
				done <- true

				return true, nil // Return true to stop listener
			case keys.Up:
				synthMixer.MasterIncrease(0.5)
				log.Info().
					Str("button", "up arrow").
					Str("action", "increase master gain").
					Float64("master_gain", synthMixer.Master).
					Msg("button press")
			case keys.Down:
				synthMixer.MasterDecrease(0.5)
				log.Info().
					Str("button", "down arrow").
					Str("action", "decrease master gain").
					Float64("master_gain", synthMixer.Master).
					Msg("button press")

			}

			return false, nil // Return false to continue listening
		})
	}
}
