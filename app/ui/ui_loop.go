package ui

import (
	"context"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
)

// command is an out-of-band instruction to the UI event loop, used by callers on
// other goroutines that need to affect the display without sending telemetry.
type command int

const (
	cmdForceRedraw command = iota // force the next live render even if data is unchanged
)

// Run is the UI event loop and the sole owner of the UserInterface's mutable
// state. HID events, display ticks, and commands are all delivered here and
// handled on this one goroutine, so no locking is required. Each event updates the
// state and then renders the resulting scene. It returns when ctx is cancelled.
func (u *UserInterface) Run(ctx context.Context) {
	u.log.Debug().Str("component", "UI event loop").Msg("Start")

	for {
		select {
		case <-ctx.Done():
			return
		case key := <-u.hidEvents:
			u.handleHIDEvent(key)
			u.render()
		case data := <-u.ticks:
			u.handleTick(data)
			u.render()
		case cmd := <-u.commands:
			u.handleCommand(cmd)
			u.render()
		}
	}
}

// UpdateDisplay queues the latest telemetry for the event loop to render. It is
// called from the application's main tick loop and never blocks: the ticks
// channel holds at most one frame, and a newer frame replaces an unconsumed one
// (the loop only ever needs the most recent telemetry).
func (u *UserInterface) UpdateDisplay(data LiveData) {
	for {
		select {
		case u.ticks <- data:
			return
		default:
			select {
			case <-u.ticks:
			default:
			}
		}
	}
}

// ForceRedraw asks the event loop to refresh the live view on the next tick even
// if the telemetry is unchanged (e.g. after a display-orientation change). It is
// called from other goroutines and never blocks; if a refresh is already queued
// the request is dropped.
func (u *UserInterface) ForceRedraw() {
	select {
	case u.commands <- cmdForceRedraw:
	default:
	}
}

// handleCommand applies an out-of-band command on the loop goroutine.
func (u *UserInterface) handleCommand(cmd command) {
	if cmd == cmdForceRedraw {
		u.state.forceRedraw = true
	}
}

// handleHIDEvent processes a single HID input event on the loop goroutine.
func (u *UserInterface) handleHIDEvent(key HIDInputEvent) {
	if !u.shouldProcessHIDEvent() {
		return
	}

	if u.handleSpecialKeys(key) {
		return
	}

	title, value := u.handleMenuNavigation(key)

	// Entering or paging between live views selects the live screen.
	if u.menuSystem.IsCurrentNodeLive() {
		u.enterLiveView(languagedb.Key(title))

		return
	}

	u.showMenuPage(u.determineLayout(), title, value)
}

// enterLiveView selects a live view and shows it immediately: the live data if we
// have a recent gear, otherwise the ready view. The live-data tick then keeps it
// updated via handleTick.
func (u *UserInterface) enterLiveView(view languagedb.Key) {
	u.state.activeLiveView = view
	u.state.lastMenuActivity = u.now()
	// Entering or paging between live views reverts the live-view +/- control to fan speed.
	u.state.activeSetting = languagedb.UIMenuFanManualSpeed
	u.registerActivity()

	if u.state.lastData.Gear == kinematics.NullGear {
		u.setMode(ScreenModeWait)
	} else {
		u.state.gearText = kinematics.GearName(u.state.lastData.Gear)
		u.setMode(ScreenModeLive)
	}

	u.state.forceRedraw = true
}
