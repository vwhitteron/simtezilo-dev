package ui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/virtual"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
)

// newTestUI builds a UserInterface backed by the in-memory virtual display, with
// a stub settings callback. Tests drive it directly (not via Run) so each event
// handler can be exercised synchronously.
func newTestUI(t *testing.T) *UserInterface {
	t.Helper()

	lang := "en"

	i18nInstance, err := i18n.New(&lang, zerolog.Nop())
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	ui := NewUserInterface(&Config{
		I18n:             i18nInstance,
		HIDEvents:        make(chan HIDInputEvent, 8),
		Display:          virtual.NewDisplay(240, 240, 265),
		SettingsCallback: func(languagedb.Key, string) string { return "" },
		ExitCodeChan:     make(chan exitcode.Code, 1),
		Log:              zerolog.Nop(),
	})

	return ui
}

// TestUpdateDisplayConflatesAndNeverBlocks verifies the public UpdateDisplay
// never blocks (cap-1 conflated channel) and keeps only the most recent frame.
func TestUpdateDisplayConflatesAndNeverBlocks(t *testing.T) {
	ui := newTestUI(t)

	// No Run goroutine is consuming; if UpdateDisplay blocked, the test would hang.
	for i := range 100 {
		ui.UpdateDisplay(LiveData{SpeedKPH: i})
	}

	select {
	case data := <-ui.ticks:
		if data.SpeedKPH != 99 {
			t.Fatalf("expected latest frame SpeedKPH=99, got %d", data.SpeedKPH)
		}
	default:
		t.Fatal("expected one conflated frame in ticks, channel was empty")
	}

	select {
	case <-ui.ticks:
		t.Fatal("ticks should hold at most one frame")
	default:
	}
}

// TestHandleTickStartupToWaitWhenIdle verifies that, once the splash timeout has
// elapsed with no telemetry, a tick moves the mode from Startup to Wait.
func TestHandleTickStartupToWaitWhenIdle(t *testing.T) {
	ui := newTestUI(t)

	base := time.Now()
	clock := base
	ui.now = func() time.Time { return clock }

	ui.mode = ScreenModeStartup
	ui.lastActivity = base
	clock = base.Add(3 * time.Second) // past the 2s splash timeout

	ui.handleTick(LiveData{TelemetryActive: false, Gear: kinematics.NullGear})

	if ui.mode != ScreenModeWait {
		t.Fatalf("expected mode Wait after idle startup timeout, got %v", ui.mode)
	}
}

// TestHandleTickWaitPowerOffSleeps verifies the 30s power-off timeout in Wait
// mode transitions to Sleep and sleeps the display.
func TestHandleTickWaitPowerOffSleeps(t *testing.T) {
	ui := newTestUI(t)

	base := time.Now()
	clock := base
	ui.now = func() time.Time { return clock }

	ui.mode = ScreenModeWait
	ui.lastActivity = base
	clock = base.Add(31 * time.Second) // past the 30s power-off timeout

	ui.handleTick(LiveData{TelemetryActive: false})

	if ui.mode != ScreenModeSleep {
		t.Fatalf("expected mode Sleep after power-off timeout, got %v", ui.mode)
	}

	disp, ok := ui.display.(*virtual.Display)
	if ok && !disp.IsSleeping() {
		t.Fatal("expected display to be sleeping after power-off timeout")
	}
}

// TestHandleTickSettingsMenuTimeoutSleeps verifies the 10s menu-inactivity
// timeout in Settings mode sleeps the display when telemetry is inactive.
func TestHandleTickSettingsMenuTimeoutSleeps(t *testing.T) {
	ui := newTestUI(t)

	base := time.Now()
	clock := base
	ui.now = func() time.Time { return clock }

	ui.mode = ScreenModeSettings
	ui.lastMenuActivity = base
	clock = base.Add(11 * time.Second) // past the 10s menu timeout

	ui.handleTick(LiveData{TelemetryActive: false})

	if ui.mode != ScreenModeSleep {
		t.Fatalf("expected mode Sleep after menu timeout, got %v", ui.mode)
	}
}

// TestHIDLiveLeafSetsActiveViewAndWait verifies that paging onto a live-view
// leaf sets activeLiveView and transitions to Wait. The menu starts on the
// UIMenuLiveView leaf; Right pages to its UIMenuLiveDashboard sibling.
func TestHIDLiveLeafSetsActiveViewAndWait(t *testing.T) {
	ui := newTestUI(t)
	ui.hidReady = true

	ui.handleHIDEvent(HIDInputRight)

	if ui.activeLiveView != languagedb.UIMenuLiveDashboard {
		t.Fatalf("expected activeLiveView=LiveDashboard, got %v", ui.activeLiveView)
	}

	if ui.mode != ScreenModeWait {
		t.Fatalf("expected mode Wait on live leaf, got %v", ui.mode)
	}
}

// TestForceRedrawSetsFlagOnCommand verifies the forceRedraw command sets the
// refresh flag when handled on the loop goroutine.
func TestForceRedrawSetsFlagOnCommand(t *testing.T) {
	ui := newTestUI(t)

	ui.handleCommand(cmdForceRedraw)

	if !ui.displayData.forceRefresh {
		t.Fatal("expected forceRefresh to be set after cmdForceRedraw")
	}
}

// TestRunConcurrentProducersNoRace drives the real cross-goroutine surface: the
// Run loop consuming while separate goroutines hammer UpdateDisplay (main tick
// loop), ForceRedraw (orientation watcher), and the hidEvents channel (HID
// producer). Under -race this proves the single-owner design is race-free. The
// test asserts nothing beyond completing without a race report or deadlock.
func TestRunConcurrentProducersNoRace(t *testing.T) {
	ui := newTestUI(t)
	ui.hidReady = true // exercise the navigation path, not the startup discard

	ctx, cancel := context.WithCancel(context.Background())

	var loop sync.WaitGroup

	loop.Add(1)

	go func() {
		defer loop.Done()
		ui.Run(ctx)
	}()

	// stop is closed once to signal all producers to finish; a closed channel is
	// observable by every goroutine (unlike a one-shot timer channel).
	stop := make(chan struct{})

	var producers sync.WaitGroup

	// Telemetry tick producer (the app's main loop).
	producers.Add(1)

	go func() {
		defer producers.Done()

		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}

			ui.UpdateDisplay(LiveData{TelemetryActive: true, Gear: i % 8, SpeedKPH: i})
		}
	}()

	// Orientation-change producer.
	producers.Add(1)

	go func() {
		defer producers.Done()

		for {
			select {
			case <-stop:
				return
			default:
			}

			ui.ForceRedraw()
		}
	}()

	// HID producer cycling through navigation keys.
	producers.Add(1)

	go func() {
		defer producers.Done()

		keys := []HIDInputEvent{HIDInputRight, HIDInputLeft, HIDInputDown, HIDInputUp}
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case ui.hidEvents <- keys[i%len(keys)]:
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	producers.Wait()
	cancel()
	loop.Wait()
}
