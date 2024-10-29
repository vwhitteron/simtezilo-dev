package nulldevice

import (
	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
)

func SetupNullDeviceButtons(synth *synth.Synthesizer, done chan bool, log zerolog.Logger) func() {
	return func() {
		keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			switch key.Code {
			case keys.CtrlC, keys.Escape:
				done <- true

				return true, nil // Return true to stop listener
			case keys.Up:
				synth.IncreaseMasterGain(0.5)
				log.Info().
					Str("button", "up arrow").
					Str("action", "increase master gain").
					Float64("master_gain", synth.GetMasterGain()).
					Msg("button press")
			case keys.Down:
				synth.DecreaseMasterGain(0.5)
				log.Info().
					Str("button", "down arrow").
					Str("action", "decrease master gain").
					Float64("master_gain", synth.GetMasterGain()).
					Msg("button press")

			}

			return false, nil // Return false to continue listening
		})
	}
}
