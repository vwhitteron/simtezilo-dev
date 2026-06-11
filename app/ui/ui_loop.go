package ui

import (
	"context"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
)

// command is an out-of-band instruction to the UI event loop, used by callers on
// other goroutines that need to affect the display without sending telemetry.
type command int

const (
	cmdForceRedraw command = iota // force the next live render even if data is unchanged
)

// Run is the UI event loop and the sole owner of the UserInterface's mutable
// state. HID events, display ticks, and commands are all delivered here and
// handled on this one goroutine, so no locking is required. It returns when ctx
// is cancelled.
func (u *UserInterface) Run(ctx context.Context) {
	u.log.Debug().Str("component", "UI event loop").Msg("Start")

	for {
		select {
		case <-ctx.Done():
			return
		case key := <-u.hidEvents:
			u.handleHIDEvent(key)
		case data := <-u.ticks:
			u.handleTick(data)
		case cmd := <-u.commands:
			u.handleCommand(cmd)
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
	switch cmd {
	case cmdForceRedraw:
		u.displayData.forceRefresh = true
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

	// Entering or paging between live views renders the selected live screen.
	// activeLiveView is set before the mode so the loop never renders a stale
	// view. The live-data tick re-enters naturally via handleWaitMode.
	if u.menuSystem.IsCurrentNodeLive() {
		u.activeLiveView = languagedb.Key(title)
		u.lastMenuActivity = u.now()
		u.registerActivity()
		u.renderActiveLiveView()
		u.setMode(ScreenModeWait)

		return
	}

	layout := u.determineLayout()
	u.renderSettingScreen(layout, title, value)
}
