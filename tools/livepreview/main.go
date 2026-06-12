// Command livepreview renders the live-view screen layouts to PNG files using the
// real gui.Screen rendering code and the project's virtual display, so the
// on-device appearance can be reviewed without hardware. It is a developer tool,
// not part of the application build.
package main

import (
	"image"
	"image/png"
	"os"

	"github.com/rs/zerolog"
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

// flatten mimics the ST7789 DrawRAW path, which ignores the alpha channel and
// reads the RGB values directly onto an opaque (black) panel. The rendering code
// draws text with a near-zero alpha, so saving the canvas as-is would let a PNG
// viewer composite the text over white and hide it. virtual.Display.SavePNG
// encodes the raw canvas; this tool flattens first so the preview is viewable.
func flatten(src *image.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	for i := 0; i < len(src.Pix); i += 4 {
		out.Pix[i] = src.Pix[i]     // R
		out.Pix[i+1] = src.Pix[i+1] // G
		out.Pix[i+2] = src.Pix[i+2] // B
		out.Pix[i+3] = 255          // force opaque
	}

	return out
}

func savePNG(name string, img *image.RGBA) {
	file, err := os.Create(name)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	err = png.Encode(file, flatten(img))
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
	renderDashboardViews(screen, disp)
	renderMiscViews(screen, disp)
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
