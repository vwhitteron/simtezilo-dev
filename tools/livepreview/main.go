// Command livepreview renders the live-view screen layouts to PNG files using the
// real gui.Screen rendering code and the project's virtual display, so the
// on-device appearance can be reviewed without hardware. It is a developer tool,
// not part of the application build.
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/virtual"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

const (
	panelWidth  = 240
	panelHeight = 240
	panelDPI    = 265
	outputDir   = "out"
)

func savePNG(name string, img *image.RGBA) {
	file, err := os.Create(name)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// SimulatePanelRGB565 quantises to the panel's 5/6/5 bit depth and forces the
	// frame opaque (the rendering code draws text with near-zero alpha, which would
	// otherwise composite over a white PNG background and hide it), so the preview
	// matches what the device displays.
	err = png.Encode(file, display.SimulatePanelRGB565(img))
	if err != nil {
		panic(err)
	}
}

func main() {
	lang := "en"

	i18nInstance, err := i18n.New(&lang, zerolog.Nop())
	if err != nil {
		panic(err)
	}

	disp := virtual.NewDisplay(panelWidth, panelHeight, panelDPI)

	screen, err := gui.NewScreen(&gui.Config{
		DisplayDevice: disp,
		I18n:          i18nInstance,
	})
	if err != nil {
		panic(err)
	}

	renderLiveViews(screen, disp)
	renderFramedViews(screen, disp)
	renderDashboardViews(screen, disp)
	renderMiscViews(screen, disp)
}

// renderFramedViews renders the framed live views (Pred, Tyres, Lap, Fuel) so the
// per-view border colours and layouts can be reviewed as they appear on the panel.
func renderFramedViews(screen *gui.Screen, disp *virtual.Display) {
	green := color.RGBA{R: 40, G: 200, B: 70, A: 255}
	red := color.RGBA{R: 235, G: 45, B: 45, A: 255}

	_ = screen.RenderDeltaScreen(gui.DeltaView{Value: "+0.123", Color: red})

	savePNG(outputDir+"/live_pred_faster.png", disp.GetCanvas())

	_ = screen.RenderDeltaScreen(gui.DeltaView{Value: "-0.456", Color: green})

	savePNG(outputDir+"/live_pred_slower.png", disp.GetCanvas())

	// Synthesized-laptime fallback: same delta with the "SYN" indicator shown.
	_ = screen.RenderDeltaScreen(gui.DeltaView{Value: "+0.123", Color: red, Synth: true})

	savePNG(outputDir+"/live_pred_synth.png", disp.GetCanvas())

	// Tyres: FL cold, FR optimal, RL optimal, RR hot, against a typical window.
	_ = screen.RenderTyresScreen(gui.TyresView{
		TempC: [4]float64{74, 81, 82, 92},
		ColdC: 75, OptLowC: 78, OptHighC: 84, HotC: 87,
		Valid: true,
	})

	savePNG(outputDir+"/live_tyres.png", disp.GetCanvas())

	_ = screen.RenderLapScreen(gui.LapView{Lap: "12", Last: "1:23.456", Best: "1:22.118"})

	savePNG(outputDir+"/live_lap.png", disp.GetCanvas())

	_ = screen.RenderFuelScreen(gui.FuelView{State: gui.FuelAnalysing})

	savePNG(outputDir+"/live_fuel_analysing.png", disp.GetCanvas())

	_ = screen.RenderFuelScreen(gui.FuelView{Percent: "73%", RangeLaps: "12.4 laps", State: gui.FuelNormal})

	savePNG(outputDir+"/live_fuel_normal.png", disp.GetCanvas())

	_ = screen.RenderFuelScreen(gui.FuelView{Percent: "8%", RangeLaps: "1.2 laps", State: gui.FuelPitThisLap})

	savePNG(outputDir+"/live_fuel_pit.png", disp.GetCanvas())

	_ = screen.RenderFuelScreen(gui.FuelView{Percent: "3%", RangeLaps: "0.4 laps", State: gui.FuelInsufficient})

	savePNG(outputDir+"/live_fuel_low.png", disp.GetCanvas())
}

// renderLiveViews renders the live-view (gear/ready) screens.
func renderLiveViews(screen *gui.Screen, disp *virtual.Display) {
	// Live view with an active gear.
	err := screen.RenderLiveScreen("3")
	if err != nil {
		panic(err)
	}

	savePNG("live_gear.png", disp.GetCanvas())

	// Live view while waiting for telemetry / in the game menus.
	err = screen.RenderLiveScreen("Ready")
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/live_ready.png", disp.GetCanvas())
}

// renderDashboardViews renders all dashboard screen variations.
func renderDashboardViews(screen *gui.Screen, disp *virtual.Display) {
	renderDash(screen, disp, gui.DashboardData{
		Gear: "4", SpeedKPH: 187, RPM: 6200, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 84,
	}, "dash_throttle.png")

	renderDash(screen, disp, gui.DashboardData{
		Gear: "2", SpeedKPH: 64, RPM: 3100, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		BrakeIn: 95, BrakeOut: 70,
	}, "dash_brake.png")

	// Both pedals applied: input matches output (solid bars, no delta).
	renderDash(screen, disp, gui.DashboardData{
		Gear: "3", SpeedKPH: 120, RPM: 5200, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 65, ThrottleOut: 65, BrakeIn: 45, BrakeOut: 45,
	}, "dash_pedals_match.png")

	// Both pedals: input above output (visible delta in darker shade).
	renderDash(screen, disp, gui.DashboardData{
		Gear: "3", SpeedKPH: 120, RPM: 5200, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 78, BrakeIn: 90, BrakeOut: 62,
	}, "dash_pedals_delta.png")

	// Mid rev-light band (6500..7800) → yellow arc.
	renderDash(screen, disp, gui.DashboardData{
		Gear: "4", SpeedKPH: 210, RPM: 7150, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 100,
	}, "dash_yellow.png")

	// At/above rev-light max → red arc, not yet at rev limit (no flash).
	renderDash(screen, disp, gui.DashboardData{
		Gear: "5", SpeedKPH: 235, RPM: 7850, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 100,
	}, "dash_red.png")

	// At the rev limit: background flashing.
	renderDash(screen, disp, gui.DashboardData{
		Gear: "5", SpeedKPH: 244, RPM: 7900, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 100, Flash: true,
	}, "dash_redline.png")
}

// renderDash renders one dashboard frame and saves it.
func renderDash(screen *gui.Screen, disp *virtual.Display, data gui.DashboardData, filename string) {
	err := screen.RenderDashboardScreen(data)
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/"+filename, disp.GetCanvas())
}

// renderMiscViews renders splash, error, and settings screens.
func renderMiscViews(screen *gui.Screen, disp *virtual.Display) {
	// Splash screen with footer text over the splash sprite.
	err := screen.RenderSplashScreen("Starting")
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/splash.png", disp.GetCanvas())

	// Error screen with footer text over the error sprite.
	err = screen.RenderErrorScreen("GT client init")
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/error.png", disp.GetCanvas())

	// Setting leaf: parent at top, value in centre, setting name at bottom.
	err = screen.RenderSettingScreen(gui.LayoutSetting, gui.SettingContent{Title: "Settings", Name: "Brightness", Value: "75%"})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/setting_leaf.png", disp.GetCanvas())

	// Branch menu: parent at top, current item in centre.
	err = screen.RenderSettingScreen(gui.LayoutMenuSub, gui.SettingContent{Title: "Settings", Value: "Display"})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/setting_menusub.png", disp.GetCanvas())

	// Info page: title at top, multi-line value centred as a group.
	err = screen.RenderSettingScreen(gui.LayoutInfo, gui.SettingContent{Title: "Version", Value: "1.2.3\nbuild 456\nlinux/arm64"})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/setting_info.png", disp.GetCanvas())
}
