package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strconv"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
)

// DashboardData holds the live telemetry values rendered on the dashboard live view.
// Throttle/brake values are percentages in the range 0..100.
type DashboardData struct {
	Gear        string
	SpeedKPH    int
	RPM         int
	RevLimit    int
	RevLightMin int
	RevLightMax int
	ThrottleIn  float64
	ThrottleOut float64
	BrakeIn     float64
	BrakeOut    float64
	// Fan is the fan-speed footer text (e.g. "Fan 42%"), matching the other live view.
	Fan   string
	Flash bool // rev-limit background flash
	Ready bool // waiting for active telemetry
}

// Dashboard geometry (240x240 panel).
const (
	dashArcStartDeg = 240.0 // 0 RPM at the 7 o'clock position
	dashArcSweepDeg = 270.0 // sweeps clockwise over the top to 4 o'clock at max RPM

	// dashLineThickness is shared by the RPM arc and the brake/throttle bars so
	// all three read as the same weight.
	dashLineThickness = 6
	dashArcInner      = 90.0
	dashArcOuter      = dashArcInner + dashLineThickness

	dashBarWidth  = dashLineThickness
	dashBarTop    = 0   // bars use the full vertical height...
	dashBarBottom = 240 // ...edge to edge
	dashBarInset  = 0   // ...and hard against the left/right edges
	dashBarHeight = dashBarBottom - dashBarTop
	dashPanelEdge = 240
	dashCenterX   = 120.0
	dashCenterY   = 108.0 // arc centre shifted up a little, fan footer stays at the bottom
	degreesToRad  = math.Pi / 180.0
	arcAngleStep  = 0.5 // degrees between arc samples
	arcRadiusStep = 0.5 // radial sampling for arc thickness

	dashFontGear  = 24.0
	dashFontSpeed = 10.0

	dashGearCenterY   = 98  // hero gear, centred a touch above mid to clear the speed
	dashSpeedBaseline = 192 // below the gear, clear of its descenders
	// Fan footer baseline, matching the gear live view exactly, which draws at
	// canvas.Rect.Max.Y - fanFooterBottomPadding.
	dashFanBaseline = dashPanelEdge - fanFooterBottomPadding
)

func dashColorBlue() color.RGBA   { return color.RGBA{R: 50, G: 120, B: 235, A: 255} }
func dashColorYellow() color.RGBA { return color.RGBA{R: 235, G: 210, B: 40, A: 255} }
func dashColorRed() color.RGBA    { return color.RGBA{R: 235, G: 45, B: 45, A: 255} }

// Brake bar (red) and throttle bar (blue). The darker shade marks the
// input-beyond-output delta. Alpha is ignored by the panel, so the distinction
// must come from the RGB value, not transparency.
func dashColorBrake() color.RGBA           { return color.RGBA{R: 235, G: 45, B: 45, A: 255} }
func dashColorBrakeDelta() color.RGBA      { return color.RGBA{R: 110, G: 22, B: 22, A: 255} }
func dashColorThrottle() color.RGBA        { return color.RGBA{R: 40, G: 200, B: 70, A: 255} }
func dashColorThrottleDelta() color.RGBA   { return color.RGBA{R: 20, G: 95, B: 38, A: 255} }
func dashColorBackground() color.RGBA      { return color.RGBA{R: 0, G: 0, B: 0, A: 255} }
func dashColorBackgroundFlash() color.RGBA { return color.RGBA{R: 130, G: 0, B: 0, A: 255} }

// Text colours are fully opaque so they composite correctly over the coloured
// flash background (the shared whiteColor/mediumGrayColor use near-zero alpha,
// which only renders correctly over black).
func dashColorText() color.RGBA    { return color.RGBA{R: 235, G: 235, B: 235, A: 255} }
func dashColorReady() color.RGBA   { return color.RGBA{R: 70, G: 70, B: 70, A: 255} }
func dashColorTextDim() color.RGBA { return color.RGBA{R: 150, G: 150, B: 150, A: 255} }

// RenderDashboardScreen renders the dashboard live view: brake/throttle bars on
// the sides, an RPM arc across the top, and the speed in the centre.
func (r *Screen) RenderDashboardScreen(dash DashboardData) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canvas := r.newBlankCanvas()

	background := dashColorBackground()
	if dash.Flash {
		background = dashColorBackgroundFlash()
	}

	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)

	if dash.Ready {
		r.drawArc(canvas, 0, 1, dashColorReady())
		fillRect(canvas, dashBarInset, dashBarTop, dashBarInset+dashBarWidth, dashBarBottom, dashColorReady())
		fillRect(canvas, dashPanelEdge-dashBarInset-dashBarWidth, dashBarTop, dashPanelEdge-dashBarInset, dashBarBottom, dashColorReady())
	} else {
		r.drawPedalBar(canvas, dashBarInset, dash.BrakeOut, dash.BrakeIn, dashColorBrake(), dashColorBrakeDelta())
		r.drawPedalBar(canvas, dashPanelEdge-dashBarInset-dashBarWidth, dash.ThrottleOut, dash.ThrottleIn, dashColorThrottle(), dashColorThrottleDelta())
		r.drawRPMArc(canvas, dash)
	}

	r.drawDashboardText(canvas, dash)

	content := &display.Content{
		Text:   fmt.Sprintf("Dash: %dkm/h %drpm", dash.SpeedKPH, dash.RPM),
		Canvas: canvas,
	}

	err := r.displayDevice.Write(content)
	if err != nil {
		return fmt.Errorf("write dashboard canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// drawPedalBar draws a vertical bar growing from the bottom: the output portion in
// bright red and the input-beyond-output delta in a darker shade above it.
func (r *Screen) drawPedalBar(canvas *image.RGBA, xPos int, output, input float64, mainColor, deltaColor color.RGBA) {
	output = clampPercent(output)
	input = clampPercent(input)

	outputY := dashBarBottom - int(output/100.0*dashBarHeight)
	inputY := dashBarBottom - int(input/100.0*dashBarHeight)

	// Input delta extends beyond the output (input is usually >= output).
	if inputY < outputY {
		fillRect(canvas, xPos, inputY, xPos+dashBarWidth, outputY, deltaColor)
	}

	fillRect(canvas, xPos, outputY, xPos+dashBarWidth, dashBarBottom, mainColor)
}

// drawRPMArc draws the tachometer arc: a dim track for the full sweep and a filled
// arc up to the current RPM. The whole fill is a single colour that shifts
// green->yellow->red as the rev limit approaches.
func (r *Screen) drawRPMArc(canvas *image.RGBA, dash DashboardData) {
	if dash.RevLimit <= 0 {
		return
	}

	rpmFraction := clamp01(float64(dash.RPM) / float64(dash.RevLimit))

	r.drawArc(canvas, 0, rpmFraction, revColor(dash.RPM, dash.RevLightMin, dash.RevLightMax))
}

// arcSampleCount is the number of angle steps in the precomputed arc table (N).
// Fractions [0,1] map to table indices [0, arcSampleCount].
const arcSampleCount = int(dashArcSweepDeg / arcAngleStep) // 540

// buildArcSamples precomputes the radial pixel coordinates for each angle step
// in the full arc sweep. The outer slice has arcSampleCount+1 entries; each inner
// slice holds the canvas points at that angle that fall within the panel bounds.
// cols/rows are used as the bounds filter; they must match the canvas size used
// at render time.
func buildArcSamples(cols, rows int) [][]image.Point {
	n := arcSampleCount
	samples := make([][]image.Point, n+1)

	for idx := 0; idx <= n; idx++ {
		fraction := float64(idx) / float64(n)
		angle := (dashArcStartDeg - dashArcSweepDeg*fraction) * degreesToRad

		var pts []image.Point

		for radius := dashArcInner; radius <= dashArcOuter; radius += arcRadiusStep {
			x := int(dashCenterX + radius*math.Cos(angle))
			y := int(dashCenterY - radius*math.Sin(angle))

			if x >= 0 && y >= 0 && x < cols && y < rows {
				pts = append(pts, image.Point{X: x, Y: y})
			}
		}

		samples[idx] = pts
	}

	return samples
}

// drawArc fills the arc band between fractions [from, to] of the sweep in a
// single colour. It uses the precomputed r.arcSamples table to avoid per-pixel
// trig and bounds checks; the outer loop matches the original step count and
// fraction formula exactly, so the set of lit pixels is identical to the old
// code.
func (r *Screen) drawArc(canvas *image.RGBA, from, target float64, col color.RGBA) {
	if target <= from {
		return
	}

	count := arcSampleCount
	steps := max(int(dashArcSweepDeg*(target-from)/arcAngleStep), 1)

	for i := 0; i <= steps; i++ {
		fraction := from + (target-from)*float64(i)/float64(steps)
		// Map fraction to the nearest precomputed sample index.
		sIdx := int(math.Round(fraction * float64(count)))
		if sIdx < 0 {
			sIdx = 0
		} else if sIdx > count {
			sIdx = count
		}

		for _, p := range r.arcSamples[sIdx] {
			canvas.SetRGBA(p.X, p.Y, col)
		}
	}
}

// drawDashboardText draws the gear (small, above centre), the speed (large,
// centre), and the fan-speed footer along the bottom edge.
func (r *Screen) drawDashboardText(canvas *image.RGBA, dash DashboardData) {
	// Gear centred both ways within the arc. Ready state uses a smaller font
	// since it's a word rather than a single gear character.
	if dash.Gear != "" {
		gearFont := dashFontGear
		if dash.Ready {
			gearFont = fontStatus
		}

		r.drawCenteredText(canvas, dash.Gear, gearFont, dashColorText(), dashGearCenterY, true)
	}

	// Speed along the bottom of the arc; suppressed in ready state and leaving
	// the very bottom for the fan footer.
	if !dash.Ready {
		r.drawCenteredText(canvas, strconv.Itoa(dash.SpeedKPH), dashFontSpeed, dashColorText(), dashSpeedBaseline, false)
	}

	// Fan setting at the very bottom, matching the other live view.
	if dash.Fan != "" {
		r.drawIconLabel(canvas, r.fanIcon, dash.Fan, dashColorTextDim(), dashFanBaseline)
	}
}

// revColor returns the arc colour for a given RPM: blue below the rev-light
// minimum, a blue->yellow->red ramp through the rev-light band, and red at or
// above the maximum.
func revColor(rpm, lightMin, lightMax int) color.RGBA {
	if lightMax <= lightMin {
		if rpm >= lightMax && lightMax > 0 {
			return dashColorRed()
		}

		return dashColorBlue()
	}

	if rpm <= lightMin {
		return dashColorBlue()
	}

	if rpm >= lightMax {
		return dashColorRed()
	}

	t := float64(rpm-lightMin) / float64(lightMax-lightMin)
	if t < 0.5 {
		return lerpColor(dashColorBlue(), dashColorYellow(), t/0.5)
	}

	return lerpColor(dashColorYellow(), dashColorRed(), (t-0.5)/0.5)
}

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	lerp := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t)
	}

	return color.RGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: 255}
}

func fillRect(canvas *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}

	draw.Draw(canvas, image.Rect(x0, y0, x1, y1), image.NewUniform(col), image.Point{}, draw.Src)
}

func clampPercent(value float64) float64 { return clamp(value, 0, 100) }
func clamp01(value float64) float64      { return clamp(value, 0, 1) }

func clamp(value, lo, high float64) float64 {
	if value < lo {
		return lo
	}

	if value > high {
		return high
	}

	return value
}
