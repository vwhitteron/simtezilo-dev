package ui

import (
	"strconv"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
)

type HIDInputEvent int

const (
	HIDInputNone HIDInputEvent = iota
	HIDInputUp
	HIDInputDown
	HIDInputLeft
	HIDInputRight
	HIDInputPageUp
	HIDInputPageDown
	HIDInputHome
	HIDInputEnd
	HIDInputEnter
	HIDInputTab
	HIDInputEscape
	HIDInputPower
)

// HIDEventHandler processes HID input events and updates the UI based on the event type.
func (u *UserInterface) HIDEventHandler() {
	u.log.Debug().
		Str("component", "HID event handler").
		Msg("Start")

	ready := false
	for key := range u.hidEvents {
		// discard hid events in the first 2 seconds after app start
		if !ready {
			if time.Since((u.startTime)) < 2*time.Second {
				continue
			}

			ready = true
		}

		menuPage := u.menuSystem.GetCurrentMenuPage()
		value := ""

		switch key { //nolint:exhaustive // no need to handle all keys
		case HIDInputUp:
			menuPage = u.menuSystem.GetCurrentMenuPage()
			value = u.SettingAction(menuPage, "increase")

			u.log.Debug().
				Str("key", "up").
				Str("action", "increase").
				Str("type", menuPage).
				Str("value", value).
				Msg("HID event")
		case HIDInputDown:
			menuPage = u.menuSystem.GetCurrentMenuPage()
			value = u.SettingAction(menuPage, "decrease")

			u.log.Debug().
				Str("key", "down").
				Str("action", "decrease").
				Str("type", menuPage).
				Str("value", value).
				Msg("HID event")
		case HIDInputLeft:
			if u.display.IsAwake() {
				menuPage = u.menuSystem.PreviousMenuPage()
			}

			value = u.SettingAction(menuPage, "get")

			u.log.Debug().
				Str("key", "left").
				Str("action", "previous").
				Str("type", "menuPage").
				Str("value", menuPage).
				Msg("HID event")
		case HIDInputRight:
			if u.display.IsAwake() {
				menuPage = u.menuSystem.NextMenuPage()
			}

			value = u.SettingAction(menuPage, "get")

			u.log.Debug().
				Str("key", "left").
				Str("action", "previous").
				Str("type", "menuPage").
				Str("value", menuPage).
				Msg("HID event")
		case HIDInputTab:
			orientation := u.display.RotateCW()

			u.log.Debug().
				Str("key", "tab").
				Str("action", "screen rotate").
				Str("type", "hardware").
				Str("value", strconv.Itoa(orientation)).
				Msg("HID event")

		case HIDInputEscape:
			u.log.Debug().
				Str("key", "escape").
				Str("action", "quit").
				Str("type", "app").
				Msg("HID event")

			u.done <- true
		case HIDInputPower:
			state := u.DisplayToggleOff()

			u.log.Debug().
				Str("key", "power").
				Str("action", "toggle off").
				Str("type", "hardware").
				Bool("value", state).
				Msg("HID event")

			continue
		default:
			u.log.Debug().Msgf("HID Input: Unknown (%d)", key)

			continue
		}

		title := "???"

		key, err := languagedb.StringToKey("ui.menu." + menuPage)
		if err != nil {
			u.log.Error().
				Err(err).
				Str("menuPage", menuPage).
				Msg("Failed to convert menu page to translation key")
		} else {
			title = u.i18n.GetString(key)
		}

		_ = u.Screen.RenderSettingScreen(title, value)
	}
}
