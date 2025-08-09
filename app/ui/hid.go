package ui

import (
	"strconv"
	"time"
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

func (u *UserInterface) HIDEventHandler() {
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

		switch key {
		case HIDInputUp:
			menuPage = u.menuSystem.GetCurrentMenuPage()
			value = u.AlterSetting(menuPage, "increase")

			u.log.Debug().
				Str("key", "up").
				Str("action", "increase").
				Str("type", menuPage).
				Str("value", value).
				Msg("HID event")
		case HIDInputDown:
			menuPage = u.menuSystem.GetCurrentMenuPage()
			value = u.AlterSetting(menuPage, "decrease")

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
			value = u.AlterSetting(menuPage, "get")

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
			value = u.AlterSetting(menuPage, "get")

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

		title := u.i18n.GetString("ui.menu." + menuPage)

		u.Screen.RenderSettingScreen(title, value)
	}
}
