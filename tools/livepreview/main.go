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

	// Live view with an active gear.
	err = screen.RenderLiveScreen("3")
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

	// Dashboard live view: on throttle, mid revs.
	err = screen.RenderDashboardScreen(gui.DashboardData{
		Gear: "4", SpeedKPH: 187, RPM: 6200, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 84, BrakeIn: 0, BrakeOut: 0,
	})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/dash_throttle.png", disp.GetCanvas())

	// Dashboard live view: braking with ABS (output below input), low revs.
	err = screen.RenderDashboardScreen(gui.DashboardData{
		Gear: "2", SpeedKPH: 64, RPM: 3100, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 0, ThrottleOut: 0, BrakeIn: 95, BrakeOut: 70,
	})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/dash_brake.png", disp.GetCanvas())

	// Dashboard live view: both pedals applied with input matching output (no delta,
	// solid bars).
	err = screen.RenderDashboardScreen(gui.DashboardData{
		Gear: "3", SpeedKPH: 120, RPM: 5200, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 65, ThrottleOut: 65, BrakeIn: 45, BrakeOut: 45,
	})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/dash_pedals_match.png", disp.GetCanvas())

	// Dashboard live view: both pedals with input above output (visible delta in the
	// darker shade above the solid output).
	err = screen.RenderDashboardScreen(gui.DashboardData{
		Gear: "3", SpeedKPH: 120, RPM: 5200, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 78, BrakeIn: 90, BrakeOut: 62,
	})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/dash_pedals_delta.png", disp.GetCanvas())

	// Dashboard live view: mid rev-light band (6500..7800) -> yellow arc.
	err = screen.RenderDashboardScreen(gui.DashboardData{
		Gear: "4", SpeedKPH: 210, RPM: 7150, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 100,
	})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/dash_yellow.png", disp.GetCanvas())

	// Dashboard live view: at/above the rev-light max -> red arc, but not yet at the
	// rev limit so the background is not flashing.
	err = screen.RenderDashboardScreen(gui.DashboardData{
		Gear: "5", SpeedKPH: 235, RPM: 7850, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 100,
	})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/dash_red.png", disp.GetCanvas())

	// Dashboard live view: at the rev limit, background flashing.
	err = screen.RenderDashboardScreen(gui.DashboardData{
		Gear: "5", SpeedKPH: 244, RPM: 7900, RevLimit: 8000, RevLightMin: 6500, RevLightMax: 7800,
		ThrottleIn: 100, ThrottleOut: 100, BrakeIn: 0, BrakeOut: 0, Flash: true,
	})
	if err != nil {
		panic(err)
	}

	savePNG(outputDir+"/dash_redline.png", disp.GetCanvas())

	// Splash screen with footer text over the splash sprite.
	if err = screen.RenderSplashScreen("Starting"); err != nil {
		panic(err)
	}

	savePNG(outputDir+"/splash.png", disp.GetCanvas())

	// Error screen with footer text over the error sprite.
	if err = screen.RenderErrorScreen("GT client init"); err != nil {
		panic(err)
	}

	savePNG(outputDir+"/error.png", disp.GetCanvas())

	// Setting leaf: parent at top, value in centre, setting name at bottom.
	if err = screen.RenderSettingScreen(gui.LayoutSetting, gui.SettingContent{Title: "Settings", Name: "Brightness", Value: "75%"}); err != nil {
		panic(err)
	}

	savePNG(outputDir+"/setting_leaf.png", disp.GetCanvas())

	// Branch menu: parent at top, current item in centre.
	if err = screen.RenderSettingScreen(gui.LayoutMenuSub, gui.SettingContent{Title: "Settings", Value: "Display"}); err != nil {
		panic(err)
	}

	savePNG(outputDir+"/setting_menusub.png", disp.GetCanvas())

	// Info page: title at top, multi-line value centred as a group.
	if err = screen.RenderSettingScreen(gui.LayoutInfo, gui.SettingContent{Title: "Version", Value: "1.2.3\nbuild 456\nlinux/arm64"}); err != nil {
		panic(err)
	}

	savePNG(outputDir+"/setting_info.png", disp.GetCanvas())
}
