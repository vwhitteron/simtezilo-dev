package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

const unknownMenuTitle = "???"

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

const (
	actionIncrease    = "increase"
	actionDecrease    = "decrease"
	actionNext        = "next"
	actionPrevious    = "previous"
	actionEnter       = "enter"
	actionExit        = "exit"
	actionGet         = "get"
	actionNone        = "none"
	menuPageSetupMode = "setupMode"
	menuPageRoot      = "root"
)

// shouldProcessHIDEvent reports whether HID events should be processed yet,
// discarding the first 2 seconds after startup.
func (u *UserInterface) shouldProcessHIDEvent() bool {
	if !u.state.hidReady {
		if u.now().Sub(u.state.startTime) < 2*time.Second {
			return false // discard hid events in the first 2 seconds after app start
		}

		u.state.hidReady = true
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
	state := u.displayToggleOff()
	u.log.Debug().
		Str("key", "power").
		Str("action", "toggle off").
		Str("type", "hardware").
		Bool("value", state).
		Msg("HID event")
}

// handleMenuNavigation handles menu navigation keys and returns the menu page and value.
func (u *UserInterface) handleMenuNavigation(key HIDInputEvent) (title string, value string) {
	title = string(u.menuSystem.GetCurrentMenuPage())

	switch key { //nolint:exhaustive // no need to handle all keys
	case HIDInputUp:
		return u.handleUpKey()
	case HIDInputDown:
		return u.handleDownKey()
	case HIDInputLeft:
		return u.handleLeftKey()
	case HIDInputRight:
		return u.handleRightKey()
	default:
		u.log.Debug().Msgf("HID Input: Unknown (%d)", key)

		return title, ""
	}
}

// handleUpKey handles the up key for navigating to parent node or increasing a value.
func (u *UserInterface) handleUpKey() (title string, value string) {
	// Reset inactivity timer on any menu interaction
	u.state.lastMenuActivity = u.now()

	// Up/Down do nothing on a live view: the fan device has its own physical
	// controls, and live views are paged with Left/Right.
	if isLiveLeaf(u.menuSystem.GetCurrentMenuPage()) {
		return string(u.menuSystem.GetCurrentMenuPage()), ""
	}

	node, action := u.menuSystem.NavigateUp()
	menuPage := node.name

	switch action {
	case actionExit:
		u.log.Debug().
			Str("key", "up").
			Str("action", actionExit).
			Str("type", "navigate").
			Str("value", string(menuPage)).
			Msg("HID event")

		value = ""
		if u.menuSystem.IsCurrentNodeBranch() {
			return string(menuPage), value
		}
	case actionIncrease:
		// On a leaf node - increase value
		value = u.settingAction(menuPage, actionIncrease)
		u.log.Debug().
			Str("key", "up").
			Str("action", actionIncrease).
			Str("type", string(menuPage)).
			Str("value", value).
			Msg("HID event")

		return string(menuPage), value
	}

	if u.menuSystem.IsCurrentNodeLeaf() {
		value = u.settingAction(menuPage, actionGet)
	}

	return string(menuPage), value
}

// handleDownKey handles the down key for entering branches or navigating.
func (u *UserInterface) handleDownKey() (title string, value string) {
	// Reset inactivity timer on any menu interaction
	u.state.lastMenuActivity = u.now()

	// Up/Down do nothing on a live view: the fan device has its own physical
	// controls, and live views are paged with Left/Right.
	if isLiveLeaf(u.menuSystem.GetCurrentMenuPage()) {
		return string(u.menuSystem.GetCurrentMenuPage()), ""
	}

	node, action := u.menuSystem.NavigateDown()
	menuPage := node.name

	switch action {
	case actionEnter:
		u.log.Debug().
			Str("key", "down").
			Str("action", actionEnter).
			Str("type", "navigate").
			Str("value", string(menuPage)).
			Msg("HID event")
	case actionExit:
		u.log.Debug().
			Str("key", "down").
			Str("action", actionExit).
			Str("type", "navigate").
			Str("value", string(menuPage)).
			Msg("HID event")

		value = ""
		if u.menuSystem.IsCurrentNodeBranch() {
			return string(menuPage), value
		}
	case actionDecrease:
		// On a leaf node - decrease value
		value = u.settingAction(menuPage, actionDecrease)
		u.log.Debug().
			Str("key", "down").
			Str("action", actionDecrease).
			Str("type", string(menuPage)).
			Str("value", value).
			Msg("HID event")

		return string(menuPage), value
	}

	// Reset setupMode countdown if we navigated to setupMode
	if string(menuPage) == menuPageSetupMode {
		_ = u.menuSystem.ResetSetupModeCountdown()
	}

	// Get current value if we're on a leaf
	if u.menuSystem.IsCurrentNodeLeaf() {
		value = u.settingAction(menuPage, actionGet)
	} else {
		value = ""
	}

	return string(menuPage), value
}

// handleLeftKey handles the left key for previous sibling or decreasing values.
func (u *UserInterface) handleLeftKey() (title string, value string) {
	// Reset inactivity timer on any menu interaction
	u.state.lastMenuActivity = u.now()

	node, action := u.menuSystem.NavigateLeft()
	menuPage := node.name

	if action == actionDecrease {
		// On a leaf node - decrease value
		value = u.settingAction(menuPage, actionDecrease)
		u.log.Debug().
			Str("key", "left").
			Str("action", actionDecrease).
			Str("type", string(menuPage)).
			Str("value", value).
			Msg("HID event")
	} else {
		// Navigation to previous sibling
		u.log.Debug().
			Str("key", "left").
			Str("action", actionPrevious).
			Str("type", "navigate").
			Str("value", string(menuPage)).
			Msg("HID event")

		// Reset setupMode countdown if we navigated to setupMode
		if string(menuPage) == menuPageSetupMode {
			_ = u.menuSystem.ResetSetupModeCountdown()
		}

		if u.display.IsAwake() && u.menuSystem.IsCurrentNodeLeaf() && !isLiveLeaf(menuPage) {
			value = u.settingAction(menuPage, actionGet)
		} else {
			value = ""
		}
	}

	return string(menuPage), value
}

// handleRightKey handles the right key for next sibling or increasing values.
func (u *UserInterface) handleRightKey() (title string, value string) {
	// Reset inactivity timer on any menu interaction
	u.state.lastMenuActivity = u.now()

	node, action := u.menuSystem.NavigateRight()
	menuPage := node.name

	if action == actionIncrease {
		// On a leaf node - increase value
		value = u.settingAction(menuPage, actionIncrease)
		u.log.Debug().
			Str("key", "right").
			Str("action", actionIncrease).
			Str("type", string(menuPage)).
			Str("value", value).
			Msg("HID event")
	} else {
		// Navigation to next sibling
		u.log.Debug().
			Str("key", "right").
			Str("action", actionNext).
			Str("type", "navigate").
			Str("value", string(menuPage)).
			Msg("HID event")

		// Reset setupMode countdown if we navigated to setupMode
		if string(menuPage) == menuPageSetupMode {
			_ = u.menuSystem.ResetSetupModeCountdown()
		}

		if u.display.IsAwake() && u.menuSystem.IsCurrentNodeLeaf() && !isLiveLeaf(menuPage) {
			value = u.settingAction(menuPage, actionGet)
		} else {
			value = ""
		}
	}

	return string(menuPage), value
}

// determineLayout determines the appropriate layout based on the current node.
func (u *UserInterface) determineLayout() gui.Layout {
	if u.menuSystem.IsCurrentNodeInfo() {
		return gui.LayoutInfo
	}

	if u.menuSystem.IsCurrentNodeBranch() {
		return gui.LayoutMenuSub
	}

	return gui.LayoutSetting
}

// showMenuPage stores the scene for the current menu page and switches to settings
// mode. The scene is drawn by render() after the event is handled.
func (u *UserInterface) showMenuPage(layout gui.Layout, menuPage string, value string) {
	u.setMode(ScreenModeSettings)
	u.state.lastMenuActivity = u.now()
	u.state.forceRedraw = true

	u.state.menuScene = scene{
		kind:    sceneSetting,
		layout:  layout,
		content: u.menuContent(layout, menuPage, value),
	}
}

// menuContent builds the text fields shown for a menu page.
func (u *UserInterface) menuContent(layout gui.Layout, menuPage string, value string) gui.SettingContent {
	switch layout { //nolint:exhaustive // only relevant layout types are handled
	case gui.LayoutMenuSub:
		// Branch nodes: parent at top, current item in centre. Top-level branches
		// (whose parent is root) instead mirror the sub-menu Return layout: their
		// own name at the top and just the enter glyph centred.
		parent := u.menuSystem.GetCurrentNode().parent
		topLevel := parent == nil || parent.name == menuPageRoot

		if menuPage == string(languagedb.UIMenuReturn) {
			// Return item: the exit glyph stands in for the "Return" label.
			title := ""
			if !topLevel {
				title = u.getBranchTitle(string(parent.name))
			}

			return gui.SettingContent{Title: title, Icon: gui.MenuIconExit}
		}

		if topLevel {
			// Name at the top, enter glyph centred (no centre label).
			return gui.SettingContent{Title: u.getBranchTitle(menuPage), Icon: gui.MenuIconEnter}
		}

		return gui.SettingContent{
			Title: u.getBranchTitle(string(parent.name)),
			Value: u.getBranchTitle(menuPage),
			Icon:  gui.MenuIconEnter,
		}

	case gui.LayoutInfo:
		// Info pages: title at top, multi-line value in centre.
		return gui.SettingContent{Title: u.getLeafTitle(menuPage), Value: value}

	default: // gui.LayoutSetting: parent at top, value in centre, setting name at bottom.
		title := u.i18n.GetString(languagedb.UIMenuSettings)
		if parent := u.menuSystem.GetCurrentNode().parent; parent != nil && parent.name != menuPageRoot {
			title = u.getBranchTitle(string(parent.name))
		}

		content := gui.SettingContent{Title: title}
		if menuPage == "return" {
			content.Value = u.i18n.GetString(languagedb.UIMenuReturn)
		} else {
			content.Name = u.getSettingName(menuPage)
			content.Value = value
		}

		return content
	}
}

// getSettingName returns the translated name of the setting (leaf node).
func (u *UserInterface) getSettingName(menuPage string) string {
	// Routing channel leaves ("ui.menu.haptics.routing.<source>.chN") are composed
	// dynamically so the label scales to any output channel count without needing a
	// translation key per channel.
	if label, ok := routingChannelLabel(menuPage); ok {
		return label
	}

	key, err := languagedb.StringToKey(menuPage)
	if err != nil {
		return menuPage
	}

	return u.i18n.GetString(key)
}

// routingChannelLabel detects a routing channel leaf key of the form
// "ui.menu.haptics.routing.<source>.chN" and returns a composed "→ ChN" label.
func routingChannelLabel(menuPage string) (string, bool) {
	const prefix = "ui.menu.haptics.routing."
	if !strings.HasPrefix(menuPage, prefix) {
		return "", false
	}

	lastDot := strings.LastIndex(menuPage, ".")

	segment := menuPage[lastDot+1:]
	if !strings.HasPrefix(segment, "ch") {
		return "", false
	}

	digits := segment[len("ch"):]
	if digits == "" {
		return "", false
	}

	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", false
		}
	}

	return "→ Ch" + digits, true
}

// getBranchTitle returns the title for a branch node.
func (u *UserInterface) getBranchTitle(menuPage string) string {
	key, err := languagedb.StringToKey(menuPage)
	if err != nil {
		u.log.Error().
			Err(err).
			Str("menuPage", menuPage).
			Msg("Failed to convert menu page to translation key")

		return "???"
	}

	return u.i18n.GetString(key)
}

// getLeafTitle returns the title for a leaf node.
func (u *UserInterface) getLeafTitle(menuPage string) string {
	// Just translate the leaf name directly
	key, err := languagedb.StringToKey(menuPage)
	if err != nil {
		u.log.Error().
			Err(err).
			Str("menuPage", menuPage).
			Msg("Failed to convert menu page to translation key")

		return unknownMenuTitle
	}

	return u.i18n.GetString(key)
}
