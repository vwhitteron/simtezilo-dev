package ui //nolint:testpackage // white-box: drives unexported handlers and state directly

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

	iface := NewUserInterface(&Config{
		I18n:             i18nInstance,
		HIDEvents:        make(chan HIDInputEvent, 8),
		Display:          virtual.NewDisplay(240, 240, 265),
		SettingsCallback: func(languagedb.Key, string) string { return "" },
		ExitCodeChan:     make(chan exitcode.Code, 1),
		Log:              zerolog.Nop(),
	})

	return iface
}

// TestUpdateDisplayConflatesAndNeverBlocks verifies the public UpdateDisplay
// never blocks (cap-1 conflated channel) and keeps only the most recent frame.
func TestUpdateDisplayConflatesAndNeverBlocks(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)

	// No Run goroutine is consuming; if UpdateDisplay blocked, the test would hang.
	for i := range 100 {
		iface.UpdateDisplay(LiveData{SpeedKPH: i})
	}

	select {
	case data := <-iface.ticks:
		if data.SpeedKPH != 99 {
			t.Fatalf("expected latest frame SpeedKPH=99, got %d", data.SpeedKPH)
		}
	default:
		t.Fatal("expected one conflated frame in ticks, channel was empty")
	}

	select {
	case <-iface.ticks:
		t.Fatal("ticks should hold at most one frame")
	default:
	}
}

// TestHandleTickStartupToWaitWhenIdle verifies that, once the splash timeout has
// elapsed with no telemetry, a tick moves the mode from Startup to Wait.
func TestHandleTickStartupToWaitWhenIdle(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)

	base := time.Now()
	clock := base
	iface.now = func() time.Time { return clock }

	iface.state.mode = ScreenModeStartup
	iface.state.lastActivity = base
	clock = base.Add(3 * time.Second) // past the 2s splash timeout

	iface.handleTick(LiveData{TelemetryActive: false, Gear: kinematics.NullGear})

	if iface.state.mode != ScreenModeWait {
		t.Fatalf("expected mode Wait after idle startup timeout, got %v", iface.state.mode)
	}
}

// TestHandleTickWaitPowerOffSleeps verifies the 30s power-off timeout in Wait
// mode transitions to Sleep and sleeps the display.
func TestHandleTickWaitPowerOffSleeps(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)

	base := time.Now()
	clock := base
	iface.now = func() time.Time { return clock }

	iface.state.mode = ScreenModeWait
	iface.state.lastActivity = base
	clock = base.Add(31 * time.Second) // past the 30s power-off timeout

	iface.handleTick(LiveData{TelemetryActive: false})

	if iface.state.mode != ScreenModeSleep {
		t.Fatalf("expected mode Sleep after power-off timeout, got %v", iface.state.mode)
	}

	disp, ok := iface.display.(*virtual.Display)
	if ok && !disp.IsSleeping() {
		t.Fatal("expected display to be sleeping after power-off timeout")
	}
}

// TestHandleTickSettingsMenuTimeoutSleeps verifies the 10s menu-inactivity
// timeout in Settings mode sleeps the display when telemetry is inactive.
func TestHandleTickSettingsMenuTimeoutSleeps(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)

	base := time.Now()
	clock := base
	iface.now = func() time.Time { return clock }

	iface.state.mode = ScreenModeSettings
	iface.state.lastMenuActivity = base
	clock = base.Add(11 * time.Second) // past the 10s menu timeout

	iface.handleTick(LiveData{TelemetryActive: false})

	if iface.state.mode != ScreenModeSleep {
		t.Fatalf("expected mode Sleep after menu timeout, got %v", iface.state.mode)
	}
}

// TestHIDLiveLeafSetsActiveViewAndWait verifies that paging onto a live-view
// leaf sets activeLiveView and transitions to Wait. The menu starts on the
// UIMenuLiveView leaf; Right pages to its UIMenuLiveDashboard sibling.
func TestHIDLiveLeafSetsActiveViewAndWait(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)
	iface.state.hidReady = true

	iface.handleHIDEvent(HIDInputRight)

	if iface.state.activeLiveView != languagedb.UIMenuLiveDashboard {
		t.Fatalf("expected activeLiveView=LiveDashboard, got %v", iface.state.activeLiveView)
	}

	if iface.state.mode != ScreenModeWait {
		t.Fatalf("expected mode Wait on live leaf, got %v", iface.state.mode)
	}
}

// TestForceRedrawSetsFlagOnCommand verifies the forceRedraw command sets the
// refresh flag when handled on the loop goroutine.
func TestForceRedrawSetsFlagOnCommand(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)

	iface.handleCommand(cmdForceRedraw)

	if !iface.state.forceRedraw {
		t.Fatal("expected forceRedraw to be set after cmdForceRedraw")
	}
}

// TestRunConcurrentProducersNoRace drives the real cross-goroutine surface: the
// Run loop consuming while separate goroutines hammer UpdateDisplay (main tick
// loop), ForceRedraw (orientation watcher), and the hidEvents channel (HID
// producer). Under -race this proves the single-owner design is race-free. The
// test asserts nothing beyond completing without a race report or deadlock.
func TestRunConcurrentProducersNoRace(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)
	iface.state.hidReady = true // exercise the navigation path, not the startup discard

	ctx, cancel := context.WithCancel(context.Background())

	var loop sync.WaitGroup

	loop.Go(func() {
		iface.Run(ctx)
	})

	// stop is closed once to signal all producers to finish; a closed channel is
	// observable by every goroutine (unlike a one-shot timer channel).
	stop := make(chan struct{})

	var producers sync.WaitGroup

	// Telemetry tick producer (the app's main loop).

	producers.Go(func() {
		for idx := 0; ; idx++ {
			select {
			case <-stop:
				return
			default:
			}

			iface.UpdateDisplay(LiveData{TelemetryActive: true, Gear: idx % 8, SpeedKPH: idx})
		}
	})

	// Orientation-change producer.

	producers.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}

			iface.ForceRedraw()
		}
	})

	// HID producer cycling through navigation keys.

	producers.Go(func() {
		keys := []HIDInputEvent{HIDInputRight, HIDInputLeft, HIDInputDown, HIDInputUp}

		for idx := 0; ; idx++ {
			select {
			case <-stop:
				return
			case iface.hidEvents <- keys[idx%len(keys)]:
			}
		}
	})

	time.Sleep(200 * time.Millisecond)
	close(stop)
	producers.Wait()
	cancel()
	loop.Wait()
}
