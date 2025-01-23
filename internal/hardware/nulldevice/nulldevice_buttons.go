package nulldevice

import (
	"fmt"

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
			case keys.RuneKey:
				if key.String() == "=" {
					gain := synth.IncreaseMasterGain()
					log.Debug().
						Str("button", "=").
						Str("action", "increase master gain").
						Float64("master_gain", gain).
						Msg("button press")
				} else if key.String() == "-" {
					gain := synth.DecreaseMasterGain()
					log.Debug().
						Str("button", "-").
						Str("action", "decrease master gain").
						Float64("master_gain", gain).
						Msg("button press")
				} else if key.String() == "q" {
					log.Debug().
						Str("button", "q").
						Str("action", "quit").
						Msg("button press")

					done <- true

					return true, nil
				}
			case keys.Up:
				profile := config.NextJerkProfile()
				log.Debug().
					Str("button", "up arrow").
					Str("action", "next profile").
					Str("type", "jerk").
					Int("profile", profile).
					Msg("button press")
			case keys.Down:
				profile := config.PreviousJerkProfile()
				log.Debug().
					Str("button", "down arrow").
					Str("action", "previous profile").
					Str("type", "jerk").
					Int("profile", profile).
					Msg("button press")
			case keys.Left:
				profile := config.PreviousSnapProfile()
				log.Debug().
					Str("button", "left arrow").
					Str("action", "previous profile").
					Str("type", "snap").
					Int("profile", profile).
					Msg("button press")
			case keys.Right:
				profile := config.NextSnapProfile()
				log.Debug().
					Str("button", "right arrow").
					Str("action", "next profile").
					Str("type", "snap").
					Int("profile", profile).
					Msg("button press")
			}

			fmt.Printf("Volume %0.2f dB    Profiles:   Force[%02d]    Grain[%02d]\r\n",
				synth.GetMasterGain(),
				config.GetJerkProfile(),
				config.GetSnapProfile(),
			)

			return false, nil // Return false to continue listening
		})
	}
}
