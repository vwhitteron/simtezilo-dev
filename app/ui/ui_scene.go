package ui

import (
	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

// sceneKind identifies which screen a scene describes.
type sceneKind int

const (
	sceneNone      sceneKind = iota // render nothing (sleep/off, or splash owned by the app)
	sceneLive                       // large gear/status text
	sceneDashboard                  // telemetry dashboard
	sceneSetting                    // menu / setting / info screen
)

// scene is an immutable description of what should be on screen. It is comparable
// (no slices/maps), so the event loop can skip drawing when the scene is unchanged
// — this render-on-change diff replaces the old per-screen suppression logic.
type scene struct {
	kind    sceneKind
	text    string             // sceneLive
	dash    gui.DashboardData  // sceneDashboard
	layout  gui.Layout         // sceneSetting
	content gui.SettingContent // sceneSetting
}

// view returns the scene the current state should display. It is a pure function
// of uiState: reading it answers "what is on screen?" in one place.
func (u *UserInterface) view() scene {
	switch u.state.mode {
	case ScreenModeLive:
		return u.liveScene()
	case ScreenModeWait:
		return u.readyScene()
	case ScreenModeSettings:
		return u.state.menuScene
	case ScreenModeStartup, ScreenModeSleep, ScreenModeOff:
		return scene{kind: sceneNone}
	default:
		return scene{kind: sceneNone}
	}
}

// liveScene describes the active live view populated with the latest telemetry.
func (u *UserInterface) liveScene() scene {
	if u.state.activeLiveView == languagedb.UIMenuLiveDashboard {
		return scene{kind: sceneDashboard, dash: dashboardFrame(u.state.lastData, u.state.flashOn)}
	}

	return scene{kind: sceneLive, text: u.state.gearText}
}

// readyScene describes the "waiting for telemetry" view for the active live view.
// Each live view's ready state mirrors its own layout so the user can tell which
// view is selected: the gear view shows large "Ready" text, the dashboard shows
// its dimmed skeleton.
func (u *UserInterface) readyScene() scene {
	ready := u.i18n.GetString(languagedb.UIReady)

	if u.state.activeLiveView == languagedb.UIMenuLiveDashboard {
		return scene{kind: sceneDashboard, dash: gui.DashboardData{Gear: ready, Ready: true}}
	}

	return scene{kind: sceneLive, text: ready}
}

// dashboardFrame maps telemetry into the gui dashboard model. Flash is supplied by
// the caller (advanced in update) so this stays a pure mapping.
func dashboardFrame(data LiveData, flash bool) gui.DashboardData {
	gear := kinematics.GearName(data.Gear)
	if data.Gear == kinematics.NullGear {
		gear = ""
	}

	return gui.DashboardData{
		Gear:        gear,
		SpeedKPH:    data.SpeedKPH,
		RPM:         data.RPM,
		RevLimit:    data.RevLimit,
		RevLightMin: data.RevLightMin,
		RevLightMax: data.RevLightMax,
		ThrottleIn:  data.ThrottleIn,
		ThrottleOut: data.ThrottleOut,
		BrakeIn:     data.BrakeIn,
		BrakeOut:    data.BrakeOut,
		Flash:       flash,
	}
}

// render draws the current scene, but only when it differs from what is already on
// the display (or when a redraw was forced). A live frame refreshes the activity
// timer, keeping the screen awake while telemetry is shown.
func (u *UserInterface) render() {
	sc := u.view()

	if u.state.forceRedraw {
		u.lastScene = scene{}
		u.state.forceRedraw = false
	}

	if sc == u.lastScene {
		return
	}

	u.draw(sc)
	u.lastScene = sc

	if u.state.mode == ScreenModeLive {
		u.registerActivity()
	}
}

// draw issues the gui render call for a scene.
func (u *UserInterface) draw(sc scene) {
	switch sc.kind {
	case sceneNone:
		// Nothing to draw; the display keeps its current content.
	case sceneLive:
		_ = u.Screen.RenderLiveScreen(sc.text)
	case sceneDashboard:
		_ = u.Screen.RenderDashboardScreen(sc.dash)
	case sceneSetting:
		_ = u.Screen.RenderSettingScreen(sc.layout, sc.content)
	}
}
