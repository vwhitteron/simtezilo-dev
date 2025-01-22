package nulldevice

import (
	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/config"
	"github.com/vwhitteron/simtezilo-dev/internal/synth"
)

func SetupNullDeviceButtons(synth *synth.Synthesizer, config *config.Config, done chan bool, log zerolog.Logger) func() {
	return func() {
		keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			switch key.Code {
			case keys.CtrlC, keys.Escape:
				done <- true

				return true, nil // Return true to stop listener
			// case keys.Up:
			// 	gain := synth.IncreaseMasterGain()
			// 	log.Info().
			// 		Str("button", "up arrow").
			// 		Str("action", "increase master gain").
			// 		Float64("master_gain", gain).
			// 		Msg("button press")
			// case keys.Down:
			// 	gain := synth.DecreaseMasterGain()
			// 	log.Info().
			// 		Str("button", "down arrow").
			// 		Str("action", "decrease master gain").
			// 		Float64("master_gain", gain).
			// 		Msg("button press")
			case keys.Up:
				profile := config.NextJerkProfile()
				log.Info().
					Str("button", "up arrow").
					Str("action", "next profile").
					Str("type", "jerk").
					Int("profile", profile).
					Msg("button press")
			case keys.Down:
				profile := config.PreviousJerkProfile()
				log.Info().
					Str("button", "down arrow").
					Str("action", "previous profile").
					Str("type", "jerk").
					Int("profile", profile).
					Msg("button press")
			case keys.Left:
				profile := config.PreviousSnapProfile()
				log.Info().
					Str("button", "left arrow").
					Str("action", "previous profile").
					Str("type", "snap").
					Int("profile", profile).
					Msg("button press")
			case keys.Right:
				profile := config.NextSnapProfile()
				log.Info().
					Str("button", "right arrow").
					Str("action", "next profile").
					Str("type", "snap").
					Int("profile", profile).
					Msg("button press")
			}

			return false, nil // Return false to continue listening
		})
	}
}
