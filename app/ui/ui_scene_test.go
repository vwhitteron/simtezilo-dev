package ui

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

	ui := NewUserInterface(&Config{
		I18n:             i18nInstance,
		HIDEvents:        make(chan HIDInputEvent, 8),
		Display:          disp,
		SettingsCallback: func(languagedb.Key, string) string { return "" },
		ExitCodeChan:     make(chan exitcode.Code, 1),
		Log:              zerolog.Nop(),
	})

	return ui, disp
}

// TestViewSceneByMode checks that view() maps each mode to the expected scene.
func TestViewSceneByMode(t *testing.T) {
	ui := newTestUI(t)
	ready := ui.i18n.GetString(languagedb.UIReady)

	tests := []struct {
		name       string
		mode       ScreenMode
		activeView languagedb.Key
		gearText   string
		want       scene
	}{
		{"sleep", ScreenModeSleep, languagedb.UIMenuLiveView, "3", scene{kind: sceneNone}},
		{"off", ScreenModeOff, languagedb.UIMenuLiveView, "3", scene{kind: sceneNone}},
		{"startup", ScreenModeStartup, languagedb.UIMenuLiveView, "3", scene{kind: sceneNone}},
		{"live gear", ScreenModeLive, languagedb.UIMenuLiveView, "3", scene{kind: sceneLive, text: "3"}},
		{"wait gear", ScreenModeWait, languagedb.UIMenuLiveView, "3", scene{kind: sceneLive, text: ready}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ui.state.mode = tc.mode
			ui.state.activeLiveView = tc.activeView
			ui.state.gearText = tc.gearText

			if got := ui.view(); got != tc.want {
				t.Errorf("view() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestViewWaitDashboardIsReadySkeleton checks the dashboard ready view.
func TestViewWaitDashboardIsReadySkeleton(t *testing.T) {
	ui := newTestUI(t)
	ui.state.mode = ScreenModeWait
	ui.state.activeLiveView = languagedb.UIMenuLiveDashboard

	got := ui.view()
	if got.kind != sceneDashboard || !got.dash.Ready {
		t.Fatalf("dashboard wait view = %+v, want a ready dashboard", got)
	}
}

// TestViewSettingsReturnsMenuScene checks settings mode renders the stored menu scene.
func TestViewSettingsReturnsMenuScene(t *testing.T) {
	ui := newTestUI(t)
	want := scene{
		kind:    sceneSetting,
		layout:  gui.LayoutSetting,
		content: gui.SettingContent{Title: "Display", Name: "Brightness", Value: "75%"},
	}
	ui.state.menuScene = want
	ui.state.mode = ScreenModeSettings

	if got := ui.view(); got != want {
		t.Errorf("view() = %+v, want stored menu scene %+v", got, want)
	}
}

// TestRenderOnChangeSuppressesUnchangedScene verifies an unchanged scene is not
// redrawn, a changed scene is, and forceRedraw forces a redraw.
func TestRenderOnChangeSuppressesUnchangedScene(t *testing.T) {
	ui, disp := newCountingUI(t)
	ui.state.mode = ScreenModeLive
	ui.state.activeLiveView = languagedb.UIMenuLiveView
	ui.state.gearText = "3"

	ui.render()

	if disp.writes != 1 {
		t.Fatalf("first render writes = %d, want 1", disp.writes)
	}

	ui.render() // unchanged scene

	if disp.writes != 1 {
		t.Fatalf("unchanged render writes = %d, want 1 (suppressed)", disp.writes)
	}

	ui.state.gearText = "4" // scene changes
	ui.render()

	if disp.writes != 2 {
		t.Fatalf("changed render writes = %d, want 2", disp.writes)
	}

	ui.state.forceRedraw = true // force redraw of identical scene
	ui.render()

	if disp.writes != 3 {
		t.Fatalf("forced render writes = %d, want 3", disp.writes)
	}
}

// TestUpdateLiveModelKeepsGearOnNull checks that a NULL gear leaves the last gear
// text in place rather than blanking it on a transient reading.
func TestUpdateLiveModelKeepsGearOnNull(t *testing.T) {
	ui := newTestUI(t)
	ui.state.activeLiveView = languagedb.UIMenuLiveView

	ui.updateLiveModel(LiveData{Gear: 3})
	want := kinematics.GearName(3)

	if ui.state.gearText != want {
		t.Fatalf("gearText = %q, want %q", ui.state.gearText, want)
	}

	ui.updateLiveModel(LiveData{Gear: kinematics.NullGear})

	if ui.state.gearText != want {
		t.Fatalf("gearText after NULL = %q, want unchanged %q", ui.state.gearText, want)
	}
}

// TestUpdateLiveModelFlashAdvances checks the rev-limit flash toggles each tick at
// or above the rev light max, and clears below it.
func TestUpdateLiveModelFlashAdvances(t *testing.T) {
	ui := newTestUI(t)
	ui.state.activeLiveView = languagedb.UIMenuLiveDashboard

	over := LiveData{RPM: 8000, RevLightMax: 7800}

	ui.updateLiveModel(over)
	first := ui.state.flashOn
	ui.updateLiveModel(over)

	if ui.state.flashOn == first {
		t.Fatal("flashOn should toggle each tick at the rev limit")
	}

	ui.updateLiveModel(LiveData{RPM: 3000, RevLightMax: 7800})

	if ui.state.flashOn {
		t.Fatal("flashOn should clear below the rev light max")
	}
}
