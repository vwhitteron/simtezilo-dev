package terminal

import (
	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

func SetupNullDeviceButtons(hidEvent chan ui.HIDInputEvent) {
	keyboard.Listen(func(key keys.Key) (stop bool, err error) {
		switch key.Code {
		case keys.CtrlC, keys.Escape:
			hidEvent <- ui.HIDInputEscape

			return true, nil // Return true to stop listener
		case keys.RuneKey:
			if key.String() == "q" {
				hidEvent <- ui.HIDInputEscape

				return true, nil // Return true to stop listener
			}
		case keys.Up:
			hidEvent <- ui.HIDInputUp
		case keys.Down:
			hidEvent <- ui.HIDInputDown
		case keys.Left:
			hidEvent <- ui.HIDInputLeft
		case keys.Right:
			hidEvent <- ui.HIDInputRight
		}

		return false, nil // Return false to continue listening
	})
}
