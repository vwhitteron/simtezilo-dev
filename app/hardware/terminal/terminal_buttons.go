package terminal

import (
	"fmt"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
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
				} else if key.String() == "m" {
					algo := synth.Mixer.NextAlgorithm()
					log.Debug().
						Str("button", "m").
						Str("action", "change algorithm").
						Str("algorithm", algo).
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
				value := config.IncreaseJerkExponent()
				log.Debug().
					Str("button", "up arrow").
					Str("action", "increase").
					Str("type", "jerk exp").
					Int("value", value).
					Msg("button press")
			case keys.Down:
				value := config.DecreaseJerkExponent()
				log.Debug().
					Str("button", "down arrow").
					Str("action", "decrease").
					Str("type", "jerk exp").
					Int("value", value).
					Msg("button press")
			case keys.Left:
				value := config.DecreaseJerkMax()
				log.Debug().
					Str("button", "left arrow").
					Str("action", "decrease").
					Str("type", "jerk max").
					Int("value", value).
					Msg("button press")
			case keys.Right:
				value := config.IncreaseJerkMax()
				log.Debug().
					Str("button", "right arrow").
					Str("action", "increase").
					Str("type", "jerk max").
					Int("value", value).
					Msg("button press")
			case keys.ShiftUp:
				value := config.IncreaseSnapExponent()
				log.Debug().
					Str("button", "up arrow").
					Str("action", "increase").
					Str("type", "snap exp").
					Int("value", value).
					Msg("button press")
			case keys.ShiftDown:
				value := config.DecreaseSnapExponent()
				log.Debug().
					Str("button", "down arrow").
					Str("action", "decrease").
					Str("type", "snap exp").
					Int("value", value).
					Msg("button press")
			case keys.ShiftLeft:
				value := config.DecreaseSnapMax()
				log.Debug().
					Str("button", "left arrow").
					Str("action", "decrease").
					Str("type", "snap max").
					Int("value", value).
					Msg("button press")
			case keys.ShiftRight:
				value := config.IncreaseSnapMax()
				log.Debug().
					Str("button", "right arrow").
					Str("action", "increase").
					Str("type", "snap max").
					Int("value", value).
					Msg("button press")
			}

			fmt.Printf("Volume %0.2f dB    Profiles:   Jerk[%0.3f, %02d]    Snap[%0.3f, %02d]\r\n",
				synth.GetMasterGain(),
				config.GetJerkExponent(),
				config.GetJerkMax(),
				config.GetSnapExponent(),
				config.GetSnapMax(),
			)

			return false, nil // Return false to continue listening
		})
	}
}
