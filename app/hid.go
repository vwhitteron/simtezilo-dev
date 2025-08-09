package app

import (
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

func (a *App) hidEventHandler() {
	ready := false
	for key := range a.hidEvents {
		// discard hid events in the first 2 seconds after app start
		if !ready {
			if time.Since((a.state.startTime)) < 2*time.Second {
				continue
			}

			ready = true
		}

		menuPage := a.menuSystem.GetCurrentMenuPage()
		value := ""

		switch key {
		case ui.HIDInputUp:
			menuPage = a.menuSystem.GetCurrentMenuPage()
			value = a.alterSetting(menuPage, "increase")

			log.Debug().
				Str("key", "up").
				Str("action", "increase").
				Str("type", menuPage).
				Str("value", value).
				Msg("HID event")
		case ui.HIDInputDown:
			menuPage = a.menuSystem.GetCurrentMenuPage()
			value = a.alterSetting(menuPage, "decrease")

			log.Debug().
				Str("key", "down").
				Str("action", "decrease").
				Str("type", menuPage).
				Str("value", value).
				Msg("HID event")
		case ui.HIDInputLeft:
			if a.display.device.IsPoweredOn() {
				menuPage = a.menuSystem.PreviousMenuPage()
			}
			value = a.alterSetting(menuPage, "get")

			log.Debug().
				Str("key", "left").
				Str("action", "previous").
				Str("type", "menuPage").
				Str("value", menuPage).
				Msg("HID event")
		case ui.HIDInputRight:
			if a.display.device.IsPoweredOn() {
				menuPage = a.menuSystem.NextMenuPage()
			}
			value = a.alterSetting(menuPage, "get")

			log.Debug().
				Str("key", "left").
				Str("action", "previous").
				Str("type", "menuPage").
				Str("value", menuPage).
				Msg("HID event")
		case ui.HIDInputTab:
			orientation := a.display.device.RotateCW()

			a.log.Debug().
				Str("key", "tab").
				Str("action", "screen rotate").
				Str("type", "hardware").
				Str("value", strconv.Itoa(orientation)).
				Msg("HID event")

		case ui.HIDInputEscape:
			a.log.Debug().
				Str("key", "escape").
				Str("action", "quit").
				Str("type", "app").
				Msg("HID event")
			a.done <- true
		case ui.HIDInputPower:
			backlightState := a.display.device.PowerToggle()

			log.Debug().
				Str("key", "power").
				Str("action", "toggle backlight").
				Str("type", "hardware").
				Bool("value", backlightState).
				Msg("HID event")

			continue
		default:
			a.log.Debug().Msgf("HID Input: Unknown (%d)", key)

			continue
		}

		title := a.i18n.GetString("ui.menu." + menuPage)

		a.display.screen.RenderSettingScreen(title, value)
	}
}
