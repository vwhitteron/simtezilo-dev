package ui

import (
	"strconv"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
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
		if !u.shouldProcessHIDEvent(&ready) {
			continue
		}

		if u.handleSpecialKeys(key) {
			continue
		}

		menuPage, value := u.handleMenuNavigation(key)
		u.renderSettingScreen(menuPage, value)
	}
}

// shouldProcessHIDEvent checks if HID events should be processed, handling startup delay.
func (u *UserInterface) shouldProcessHIDEvent(ready *bool) bool {
	if !*ready {
		if time.Since(u.startTime) < 2*time.Second {
			return false // discard hid events in the first 2 seconds after app start
		}

		*ready = true
	}

	return true
}

// handleSpecialKeys handles special keys that don't require menu updates.
func (u *UserInterface) handleSpecialKeys(key HIDInputEvent) bool {
	switch key { //nolint:exhaustive // no need to handle all keys
	case HIDInputTab:
		u.handleTabKey()

		return true
	case HIDInputEscape:
		u.handleEscapeKey()

		return true
	case HIDInputPower:
		u.handlePowerKey()

		return true
	default:
		return false
	}
}

// handleTabKey handles the tab key for screen rotation.
func (u *UserInterface) handleTabKey() {
	orientation := u.display.RotateCW()
	u.log.Debug().
		Str("key", "tab").
		Str("action", "screen rotate").
		Str("type", "hardware").
		Str("value", strconv.Itoa(orientation)).
		Msg("HID event")
}

// handleEscapeKey handles the escape key for quitting the application.
func (u *UserInterface) handleEscapeKey() {
	u.log.Debug().
		Str("key", "escape").
		Str("action", "quit").
		Str("type", "app").
		Msg("HID event")

	u.done <- exitcode.Success
}

// handlePowerKey handles the power key for toggling display off.
func (u *UserInterface) handlePowerKey() {
	state := u.DisplayToggleOff()
	u.log.Debug().
		Str("key", "power").
		Str("action", "toggle off").
		Str("type", "hardware").
		Bool("value", state).
		Msg("HID event")
}

// handleMenuNavigation handles menu navigation keys and returns the menu page and value.
func (u *UserInterface) handleMenuNavigation(key HIDInputEvent) (string, string) {
	menuPage := u.menuSystem.GetCurrentMenuPage()

	switch key { //nolint:exhaustive // no need to handle all keys
	case HIDInputUp:
		return u.handleUpKey(menuPage)
	case HIDInputDown:
		return u.handleDownKey(menuPage)
	case HIDInputLeft:
		return u.handleLeftKey()
	case HIDInputRight:
		return u.handleRightKey()
	default:
		u.log.Debug().Msgf("HID Input: Unknown (%d)", key)

		return menuPage, ""
	}
}

// handleUpKey handles the up key for increasing values.
func (u *UserInterface) handleUpKey(menuPage string) (string, string) {
	value := u.SettingAction(menuPage, "increase")
	u.log.Debug().
		Str("key", "up").
		Str("action", "increase").
		Str("type", menuPage).
		Str("value", value).
		Msg("HID event")

	return menuPage, value
}

// handleDownKey handles the down key for decreasing values.
func (u *UserInterface) handleDownKey(menuPage string) (string, string) {
	value := u.SettingAction(menuPage, "decrease")
	u.log.Debug().
		Str("key", "down").
		Str("action", "decrease").
		Str("type", menuPage).
		Str("value", value).
		Msg("HID event")

	return menuPage, value
}

// handleLeftKey handles the left key for previous menu page.
func (u *UserInterface) handleLeftKey() (string, string) {
	menuPage := u.menuSystem.GetCurrentMenuPage()
	if u.display.IsAwake() {
		menuPage = u.menuSystem.PreviousMenuPage()
	}

	value := u.SettingAction(menuPage, "get")
	u.log.Debug().
		Str("key", "left").
		Str("action", "previous").
		Str("type", "menuPage").
		Str("value", menuPage).
		Msg("HID event")

	return menuPage, value
}

// handleRightKey handles the right key for next menu page.
func (u *UserInterface) handleRightKey() (string, string) {
	menuPage := u.menuSystem.GetCurrentMenuPage()
	if u.display.IsAwake() {
		menuPage = u.menuSystem.NextMenuPage()
	}

	value := u.SettingAction(menuPage, "get")
	u.log.Debug().
		Str("key", "right").
		Str("action", "next").
		Str("type", "menuPage").
		Str("value", menuPage).
		Msg("HID event")

	return menuPage, value
}

// renderSettingScreen renders the setting screen with the given title and value.
func (u *UserInterface) renderSettingScreen(menuPage, value string) {
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
