package ui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

// Lap-time delta colours: faster green, slower red, dim grey
// placeholder until a reference lap exists. Sourced from the gui Material palette.
func deltaFasterColor() color.RGBA      { return gui.MaterialGreenA700() }
func deltaSlowerColor() color.RGBA      { return gui.MaterialRed600() }
func deltaPlaceholderColor() color.RGBA { return gui.MaterialGrey600() }

// sceneKind identifies which screen a scene describes.
type sceneKind int

const (
	sceneNone      sceneKind = iota // render nothing (sleep/off, or splash owned by the app)
	sceneDashboard                  // telemetry dashboard
	sceneDelta                      // predictive lap-time view
	sceneTyres                      // tyre-temperature quadrants
	sceneLap                        // lap info
	sceneFuel                       // fuel info
	sceneSetting                    // menu / setting / info screen
)

// scene is an immutable description of what should be on screen. It is comparable
// (no slices/maps), so the event loop can skip drawing when the scene is unchanged
// — this render-on-change diff replaces the old per-screen suppression logic.
type scene struct {
	kind    sceneKind
	dash    gui.DashboardData  // sceneDashboard
	delta   gui.DeltaView      // scenePred
	tyres   gui.TyresView      // sceneTyres
	lap     gui.LapView        // sceneLap
	fuel    gui.FuelView       // sceneFuel
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
	switch u.state.activeLiveView {
	case languagedb.UIMenuLiveDashboard:
		return scene{kind: sceneDashboard, dash: dashboardFrame(u.state.lastData, u.state.flashOn)}
	case languagedb.UIMenuLiveTyres:
		return scene{kind: sceneTyres, tyres: tyresFrame(u.state.lastData)}
	case languagedb.UIMenuLiveLap:
		return scene{kind: sceneLap, lap: lapFrame(u.state.lastData)}
	case languagedb.UIMenuLiveFuel:
		return scene{kind: sceneFuel, fuel: fuelFrame(u.state.lastData)}
	default: // UIMenuLivePred
		return scene{kind: sceneDelta, delta: predFrame(u.state.lastData)}
	}
}

// readyScene describes the "waiting for telemetry" view for the active live view.
// Each live view's ready state mirrors its own layout so the user can tell which
// view is selected: the dashboard shows its dimmed skeleton, the framed views
// render their own "no data" placeholders.
func (u *UserInterface) readyScene() scene {
	if u.state.activeLiveView == languagedb.UIMenuLiveDashboard {
		ready := u.i18n.GetString(languagedb.UIReady)

		return scene{kind: sceneDashboard, dash: gui.DashboardData{Gear: ready, Ready: true}}
	}

	// The framed views map invalid/zero telemetry to placeholders, so the live
	// mapping doubles as the ready state.
	return u.liveScene()
}

// predFrame maps telemetry into the predictive lap-time view model. The delta is
// pre-formatted and pre-coloured here so the gui stays a dumb renderer.
func predFrame(data LiveData) gui.DeltaView {
	if !data.PredValid {
		return gui.DeltaView{Value: "--.---", Color: deltaPlaceholderColor()}
	}

	col := deltaFasterColor()
	if data.PredDelta > 0 {
		col = deltaSlowerColor()
	}

	return gui.DeltaView{Value: formatPredDelta(data.PredDelta), Color: col, Synth: data.PredSynth}
}

// formatPredDelta formats a signed delta in seconds as "+0.123" / "-0.123".
func formatPredDelta(secs float64) string {
	sign := "+"
	if secs < 0 {
		sign = "-"
	}

	return fmt.Sprintf("%s%.3f", sign, math.Abs(secs))
}

// tyresFrame maps telemetry into the tyre-temperature view model. Temperatures are
// rounded to whole degrees — the resolution actually shown ("%.0f") and used for
// the colour ramp — so the comparable scene only changes when a displayed degree
// changes, rather than redrawing on every sub-degree drift.
func tyresFrame(data LiveData) gui.TyresView {
	return gui.TyresView{
		TempC: [4]float64{
			math.Round(data.TyreTempC[0]),
			math.Round(data.TyreTempC[1]),
			math.Round(data.TyreTempC[2]),
			math.Round(data.TyreTempC[3]),
		},
		ColdC:    data.TyreColdC,
		OptLowC:  data.TyreOptLowC,
		OptHighC: data.TyreOptHighC,
		HotC:     data.TyreHotC,
		Valid:    data.TyreValid,
	}
}

// lapFrame maps telemetry into the lap-info view model.
func lapFrame(data LiveData) gui.LapView {
	lap := "--"
	if data.LapNumber > 0 {
		lap = strconv.Itoa(data.LapNumber)
	}

	return gui.LapView{Lap: lap, Last: data.LastLapText, Best: data.BestLapText}
}

// fuelFrame maps telemetry into the fuel-info view model.
func fuelFrame(data LiveData) gui.FuelView {
	if !data.FuelReady {
		return gui.FuelView{State: gui.FuelAnalysing}
	}

	state := gui.FuelNormal

	switch {
	case data.FuelInsufficient:
		state = gui.FuelInsufficient
	case data.FuelPitThisLap:
		state = gui.FuelPitThisLap
	}

	return gui.FuelView{
		Percent:   fmt.Sprintf("%.0f%%", data.FuelPercent),
		RangeLaps: fmt.Sprintf("%.1f laps", data.FuelRangeLaps),
		State:     state,
	}
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
	currentScene := u.view()

	if u.state.forceRedraw {
		u.lastScene = scene{}
		u.state.forceRedraw = false
	}

	if currentScene == u.lastScene {
		return
	}

	u.draw(currentScene)
	u.lastScene = currentScene

	if u.state.mode == ScreenModeLive {
		u.registerActivity()
	}
}

// draw issues the gui render call for a scene.
func (u *UserInterface) draw(currentScene scene) {
	switch currentScene.kind {
	case sceneNone:
		// Nothing to draw; the display keeps its current content.
	case sceneDashboard:
		_ = u.Screen.RenderDashboardScreen(currentScene.dash)
	case sceneDelta:
		_ = u.Screen.RenderDeltaScreen(currentScene.delta)
	case sceneTyres:
		_ = u.Screen.RenderTyresScreen(currentScene.tyres)
	case sceneLap:
		_ = u.Screen.RenderLapScreen(currentScene.lap)
	case sceneFuel:
		_ = u.Screen.RenderFuelScreen(currentScene.fuel)
	case sceneSetting:
		_ = u.Screen.RenderSettingScreen(currentScene.layout, currentScene.content)
	}
}
