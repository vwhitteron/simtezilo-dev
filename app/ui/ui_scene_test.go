package ui //nolint:testpackage // white-box: asserts the unexported scene view-model

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/virtual"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

// countingDisplay wraps the virtual display and counts Write calls, so tests can
// assert the render-on-change behaviour.
type countingDisplay struct {
	*virtual.Display

	writes int
}

func (c *countingDisplay) Write(content *display.Content) error {
	c.writes++

	return c.Display.Write(content)
}

func newCountingUI(t *testing.T) (*UserInterface, *countingDisplay) {
	t.Helper()

	lang := "en"

	i18nInstance, err := i18n.New(&lang, zerolog.Nop())
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	disp := &countingDisplay{Display: virtual.NewDisplay(240, 240, 265)}

	iface := NewUserInterface(&Config{
		I18n:             i18nInstance,
		HIDEvents:        make(chan HIDInputEvent, 8),
		Display:          disp,
		SettingsCallback: func(languagedb.Key, string) string { return "" },
		ExitCodeChan:     make(chan exitcode.Code, 1),
		Log:              zerolog.Nop(),
	})

	return iface, disp
}

// TestViewSceneByMode checks that view() maps each mode to the expected scene.
func TestViewSceneByMode(t *testing.T) {
	t.Parallel()

	predPlaceholder := scene{kind: sceneDelta, delta: gui.DeltaView{Value: "--.---", Color: deltaPlaceholderColor()}}

	tests := []struct {
		name       string
		mode       ScreenMode
		activeView languagedb.Key
		want       scene
	}{
		{"sleep", ScreenModeSleep, languagedb.UIMenuLivePred, scene{kind: sceneNone}},
		{"off", ScreenModeOff, languagedb.UIMenuLivePred, scene{kind: sceneNone}},
		{"startup", ScreenModeStartup, languagedb.UIMenuLivePred, scene{kind: sceneNone}},
		{"live pred placeholder", ScreenModeLive, languagedb.UIMenuLivePred, predPlaceholder},
		{"wait pred placeholder", ScreenModeWait, languagedb.UIMenuLivePred, predPlaceholder},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Each subtest gets its own UI so the parallel runs do not share
			// mutable state (state.mode/activeLiveView).
			iface := newTestUI(t)
			iface.state.mode = testCase.mode
			iface.state.activeLiveView = testCase.activeView

			if got := iface.view(); got != testCase.want {
				t.Errorf("view() = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// TestViewWaitDashboardIsReadySkeleton checks the dashboard ready view.
func TestViewWaitDashboardIsReadySkeleton(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)
	iface.state.mode = ScreenModeWait
	iface.state.activeLiveView = languagedb.UIMenuLiveDashboard

	got := iface.view()
	if got.kind != sceneDashboard || !got.dash.Ready {
		t.Fatalf("dashboard wait view = %+v, want a ready dashboard", got)
	}
}

// TestViewSettingsReturnsMenuScene checks settings mode renders the stored menu scene.
func TestViewSettingsReturnsMenuScene(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)
	want := scene{
		kind:    sceneSetting,
		layout:  gui.LayoutSetting,
		content: gui.SettingContent{Title: "Display", Name: "Brightness", Value: "75%"},
	}
	iface.state.menuScene = want
	iface.state.mode = ScreenModeSettings

	if got := iface.view(); got != want {
		t.Errorf("view() = %+v, want stored menu scene %+v", got, want)
	}
}

// TestRenderOnChangeSuppressesUnchangedScene verifies an unchanged scene is not
// redrawn, a changed scene is, and forceRedraw forces a redraw.
func TestRenderOnChangeSuppressesUnchangedScene(t *testing.T) {
	t.Parallel()

	iface, disp := newCountingUI(t)
	iface.state.mode = ScreenModeLive
	iface.state.activeLiveView = languagedb.UIMenuLivePred
	iface.state.lastData = LiveData{PredValid: true, PredDelta: 0.5}

	iface.render()

	if disp.writes != 1 {
		t.Fatalf("first render writes = %d, want 1", disp.writes)
	}

	iface.render() // unchanged scene

	if disp.writes != 1 {
		t.Fatalf("unchanged render writes = %d, want 1 (suppressed)", disp.writes)
	}

	iface.state.lastData = LiveData{PredValid: true, PredDelta: -0.5} // scene changes
	iface.render()

	if disp.writes != 2 {
		t.Fatalf("changed render writes = %d, want 2", disp.writes)
	}

	iface.state.forceRedraw = true // force redraw of identical scene
	iface.render()

	if disp.writes != 3 {
		t.Fatalf("forced render writes = %d, want 3", disp.writes)
	}
}

// TestUpdateLiveModelKeepsGearOnNull checks that a NULL gear leaves the last gear
// text in place rather than blanking it on a transient reading.
func TestUpdateLiveModelKeepsGearOnNull(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)
	iface.state.activeLiveView = languagedb.UIMenuLivePred

	iface.updateLiveModel(LiveData{Gear: 3})

	want := kinematics.GearName(3)

	if iface.state.gearText != want {
		t.Fatalf("gearText = %q, want %q", iface.state.gearText, want)
	}

	iface.updateLiveModel(LiveData{Gear: kinematics.NullGear})

	if iface.state.gearText != want {
		t.Fatalf("gearText after NULL = %q, want unchanged %q", iface.state.gearText, want)
	}
}

// TestUpdateLiveModelFlashAdvances checks the rev-limit flash toggles each tick at
// or above the rev light max, and clears below it.
func TestUpdateLiveModelFlashAdvances(t *testing.T) {
	t.Parallel()

	iface := newTestUI(t)
	iface.state.activeLiveView = languagedb.UIMenuLiveDashboard

	over := LiveData{RPM: 8000, RevLightMax: 7800}

	iface.updateLiveModel(over)
	first := iface.state.flashOn
	iface.updateLiveModel(over)

	if iface.state.flashOn == first {
		t.Fatal("flashOn should toggle each tick at the rev limit")
	}

	iface.updateLiveModel(LiveData{RPM: 3000, RevLightMax: 7800})

	if iface.state.flashOn {
		t.Fatal("flashOn should clear below the rev light max")
	}
}
