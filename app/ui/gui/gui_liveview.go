package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// The framed live views (Delta, Tyres, Lap, Fuel) share a common background: a
// rounded-rectangle border and a header title box, both drawn in a per-view
// colour, with the title in black. The frame proportions are derived from
// live_view_border.svg, scaled to the panel; the SVG itself is a visual reference
// only (its transforms and even-odd ring are beyond the icons rasteriser).

const (
	liveFrameInset     = 0   // gap from panel edge to the outer border (0 = flush with the edge)
	liveFrameRadius    = 8.0 // outer corner radius
	liveFrameThickness = 3   // border stroke width
	liveHeaderWidthPct = 40  // header box width as a percentage of panel width
	liveHeaderHeight   = 34  // header box height in pixels
	liveHeaderRadius   = 6.0 // header box bottom-corner radius
	liveTitleFont      = 10

	liveDeltaFont      = 18.0
	liveLapNumFont     = 30.0
	liveRowFont        = 12.0
	liveTyreFont       = 14
	liveEstimatingFont = 10
	liveSynthFont      = 8.0
)

// Colour helpers (blackColor, frame*, live*, tyre*) live in gui_palette.go.

// DeltaView is the lap-time difference view model: a single pre-formatted,
// pre-coloured delta value (e.g. "+0.123").
type DeltaView struct {
	Value string
	Color color.RGBA
	Synth bool // when true, draw the "SYN" indicator (running on synthesized lap time)
}

// TyresView holds the four tyre temperatures (FL, FR, RL, RR in °C) and the
// thresholds used to colour each quadrant.
type TyresView struct {
	TempC    [4]float64
	ColdC    float64
	OptLowC  float64
	OptHighC float64
	HotC     float64
	Valid    bool // false until real telemetry arrives (ready state)
}

// LapView holds the current lap number and the last/best lap time strings.
type LapView struct {
	Lap  string
	Last string
	Best string
}

// FuelState selects the Fuel view's background/text treatment.
type FuelState int

const (
	FuelNormal       FuelState = iota // light text on black
	FuelAnalysing                     // "Analysing..." placeholder
	FuelPitThisLap                    // inverted: dark text on yellow
	FuelInsufficient                  // light text on red
)

// FuelView holds the fuel percentage and estimated range, plus the state that
// drives the colour treatment.
type FuelView struct {
	Percent   string
	RangeLaps string
	State     FuelState
}

// RenderDeltaScreen renders the lap-time difference view.
func (r *Screen) RenderDeltaScreen(view DeltaView) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canvas := r.newBlankCanvas()
	fillBackground(canvas, blackColor())

	r.drawCenteredNumeric(canvas, view.Value, liveDeltaFont, view.Color, bodyCentreY(canvas))

	// On the synthesized-laptime fallback, mark the view with a small grey "SYN"
	// label in the space below the delta, without shifting the delta itself.
	if view.Synth {
		_, _, _, bottom := innerRect(canvas)
		centre := bodyCentreY(canvas)
		synthY := centre + (bottom-centre)/2
		r.drawCenteredText(canvas, "SYN", liveSynthFont, MaterialGrey600(), synthY, true)
	}

	r.drawFrame(canvas, "Delta", frameViolet())

	return r.writeLiveView(canvas, "Delta: "+view.Value)
}

// RenderTyresScreen renders the tyre-temperature quadrant view.
func (r *Screen) RenderTyresScreen(view TyresView) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canvas := r.newBlankCanvas()
	fillBackground(canvas, blackColor())

	r.drawTyreQuadrants(canvas, view)

	r.drawFrame(canvas, "Tyres", frameYellow())

	return r.writeLiveView(canvas, "Tyres")
}

// RenderLapScreen renders the lap-info view.
func (r *Screen) RenderLapScreen(view LapView) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canvas := r.newBlankCanvas()
	fillBackground(canvas, blackColor())

	bottom := canvas.Rect.Max.Y - (liveFrameInset + liveFrameThickness)

	// Lay out within the space below the header box. The lap number is large, so
	// the last-lap row is biased downwards (not the exact midpoint) to even out
	// the visual gaps; the best lap anchors near the bottom.
	top := liveFrameInset + liveHeaderHeight
	height := bottom - top

	lapY := top + height*24/100
	lastY := top + height*62/100
	bestY := top + height*90/100

	r.drawCenteredNumeric(canvas, view.Lap, liveLapNumFont, MaterialGrey300(), lapY)
	r.drawCenteredNumeric(canvas, view.Last, liveRowFont, dashColorText(), lastY)
	r.drawCenteredNumeric(canvas, view.Best, liveRowFont, liveBestLapColor(), bestY)

	r.drawFrame(canvas, "Lap", frameBlue())

	return r.writeLiveView(canvas, "Lap: "+view.Lap)
}

// RenderFuelScreen renders the fuel-info view.
func (r *Screen) RenderFuelScreen(view FuelView) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canvas := r.newBlankCanvas()

	background := blackColor()
	textColor := MaterialGrey300()

	switch view.State {
	case FuelPitThisLap:
		background = liveFuelPitBG()
		textColor = liveDarkText()
	case FuelInsufficient:
		background = liveFuelLowBG()
		textColor = dashColorText()
	case FuelNormal, FuelAnalysing:
		// defaults
	}

	fillBackground(canvas, background)

	_, top, _, bottom := innerRect(canvas)

	if view.State == FuelAnalysing {
		r.drawCenteredText(canvas, "Estimating...", liveEstimatingFont, textColor, bodyCentreY(canvas), true)
	} else {
		// Two equal-size rows in the body below the header: estimated range (laps)
		// on top, fuel percentage below.
		body := bottom - top
		rangeY := top + body*38/100
		percentY := top + body*72/100

		r.drawCenteredNumeric(canvas, view.RangeLaps, liveRowFont, textColor, rangeY)
		r.drawCenteredNumeric(canvas, view.Percent, liveRowFont, textColor, percentY)
	}

	r.drawFrame(canvas, "Fuel", frameGreen())

	return r.writeLiveView(canvas, "Fuel: "+view.Percent)
}

// writeLiveView pushes a finished live-view canvas to the display.
func (r *Screen) writeLiveView(canvas *image.RGBA, text string) error {
	err := r.displayDevice.Write(&display.Content{Text: text, Canvas: canvas})
	if err != nil {
		return fmt.Errorf("write live view canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// drawTyreQuadrants fills the inner area as four tyre quadrants (TL=FL, TR=FR,
// BL=RL, BR=RR), each coloured by temperature, with the numeric temperature
// centred over it.
func (r *Screen) drawTyreQuadrants(canvas *image.RGBA, view TyresView) {
	left, top, right, bottom := innerRect(canvas)
	// Split the screen into even quadrants: both dividers sit at 50% of the
	// canvas, regardless of the header box.
	midX := canvas.Rect.Max.X / 2
	midY := canvas.Rect.Max.Y / 2

	rects := [4][4]int{
		{left, top, midX, midY},     // FL
		{midX, top, right, midY},    // FR
		{left, midY, midX, bottom},  // RL
		{midX, midY, right, bottom}, // RR
	}

	// Dividing lines between the quadrants, in gray.
	divider := MaterialGrey600()
	blendRect(canvas, midX-1, top, midX+1, bottom, divider)
	blendRect(canvas, left, midY-1, right, midY+1, divider)

	face := r.face(r.i18n.VariableFont().Font, r.i18n.VariableFont().Scale*liveTyreFont)

	for idx, rect := range rects {
		// Background stays the default (black); the temperature is conveyed by the
		// text colour on the cold->optimal->hot ramp.
		col := MaterialGrey600()
		label := "--"

		if view.Valid {
			col = tyreColor(view.TempC[idx], view.ColdC, view.OptLowC, view.OptHighC, view.HotC)
			label = fmt.Sprintf("%.0f", view.TempC[idx])
		}

		drawNumericInRect(canvas, face, col, label, rect[0], rect[1], rect[2], rect[3])
	}
}

// drawFrame draws the rounded border and header title box on top of whatever has
// already been drawn, in the given colour with a black title.
func (r *Screen) drawFrame(canvas *image.RGBA, title string, col color.RGBA) {
	width := canvas.Rect.Max.X
	height := canvas.Rect.Max.Y

	strokeRoundRect(canvas,
		liveFrameInset, liveFrameInset,
		width-liveFrameInset, height-liveFrameInset,
		liveFrameRadius, liveFrameThickness, col)

	headerWidth := width * liveHeaderWidthPct / 100
	hx0 := (width - headerWidth) / 2
	hx1 := hx0 + headerWidth
	hy0 := liveFrameInset
	hy1 := hy0 + liveHeaderHeight

	fillHeaderBox(canvas, hx0, hy0, hx1, hy1, liveHeaderRadius, col)

	face := r.face(r.i18n.VariableFont().Font, r.i18n.VariableFont().Scale*liveTitleFont)
	drawTextInRect(canvas, face, blackColor(), title, hx0, hy0, hx1, hy1)
}

// tyreColor maps a tyre temperature to a colour on the cold->optimal->hot ramp.
func tyreColor(temp, cold, optLow, optHigh, hot float64) color.RGBA {
	switch {
	case temp <= cold:
		return tyreColdColor()
	case temp >= hot:
		return tyreHotColor()
	case temp < optLow:
		return lerpColor(tyreColdColor(), tyreOptimalColor(), (temp-cold)/(optLow-cold))
	case temp <= optHigh:
		return tyreOptimalColor()
	default:
		return lerpColor(tyreOptimalColor(), tyreHotColor(), (temp-optHigh)/(hot-optHigh))
	}
}

// fillBackground fills the whole canvas with a solid colour.
func fillBackground(canvas *image.RGBA, col color.RGBA) {
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(col), image.Point{}, draw.Src)
}

// blendOver composites col over the existing canvas pixel, treating col.A as a
// straight (non-premultiplied) opacity. A == 255 is an opaque overwrite. The
// panel has no alpha channel, so this bakes the blend into the RGB values it
// shows; the result is always written fully opaque.
func blendOver(canvas *image.RGBA, xPos, yPos int, col color.RGBA) {
	if col.A == 255 {
		canvas.SetRGBA(xPos, yPos, col)

		return
	}

	alpha := float64(col.A) / 255
	off := canvas.PixOffset(xPos, yPos)

	blend := func(src byte, dst byte) byte {
		return byte(float64(src)*alpha + float64(dst)*(1-alpha) + 0.5)
	}

	canvas.Pix[off] = blend(col.R, canvas.Pix[off])
	canvas.Pix[off+1] = blend(col.G, canvas.Pix[off+1])
	canvas.Pix[off+2] = blend(col.B, canvas.Pix[off+2])
	canvas.Pix[off+3] = 255
}

// blendRect composites col over a filled rectangle of the canvas.
func blendRect(canvas *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	for yPos := y0; yPos < y1; yPos++ {
		for xPos := x0; xPos < x1; xPos++ {
			blendOver(canvas, xPos, yPos, col)
		}
	}
}

// innerRect returns the drawable area inside the border stroke.
func innerRect(canvas *image.RGBA) (left, top, right, bottom int) {
	edge := liveFrameInset + liveFrameThickness

	return edge, edge, canvas.Rect.Max.X - edge, canvas.Rect.Max.Y - edge
}

// bodyCentreY returns the vertical centre of the area below the header box.
func bodyCentreY(canvas *image.RGBA) int {
	edge := liveFrameInset + liveFrameThickness
	bottom := canvas.Rect.Max.Y - edge
	headerBottom := liveFrameInset + liveHeaderHeight

	return (headerBottom + bottom) / 2
}

// strokeRoundRect draws a rounded-rectangle border of the given thickness.
func strokeRoundRect(canvas *image.RGBA, left, top, right, bottom int, radius float64, thickness int, col color.RGBA) {
	innerRadius := radius - float64(thickness)
	if innerRadius < 0 {
		innerRadius = 0
	}

	for yPos := top; yPos < bottom; yPos++ {
		for xPos := left; xPos < right; xPos++ {
			outside := !roundRectContains(xPos, yPos, left, top, right, bottom, radius, true)
			inside := roundRectContains(xPos, yPos, left+thickness, top+thickness, right-thickness, bottom-thickness, innerRadius, true)

			if !outside && !inside {
				blendOver(canvas, xPos, yPos, col)
			}
		}
	}
}

// fillHeaderBox fills a box whose bottom corners are rounded and whose top corners
// are square (so it merges into the top of the frame).
func fillHeaderBox(canvas *image.RGBA, left, top, right, bottom int, radius float64, col color.RGBA) {
	for yPos := top; yPos < bottom; yPos++ {
		for xPos := left; xPos < right; xPos++ {
			if roundRectContains(xPos, yPos, left, top, right, bottom, radius, false) {
				blendOver(canvas, xPos, yPos, col)
			}
		}
	}
}

// rectContains reports whether (pointX,pointY) lies inside the half-open rectangle.
func rectContains(pointX, pointY, left, top, right, bottom int) bool {
	return pointX >= left && pointX < right && pointY >= top && pointY < bottom
}

// nearestCornerCentre returns the centre of the rounded corner nearest the point.
func nearestCornerCentre(pointX, pointY, left, top, right, bottom, rad int) (int, int) {
	centreX := left + rad
	if pointX >= right-rad {
		centreX = right - 1 - rad
	}

	centreY := top + rad
	if pointY >= bottom-rad {
		centreY = bottom - 1 - rad
	}

	return centreX, centreY
}

// roundRectContains reports whether the point lies inside the rounded rectangle.
// When roundTop is false only the bottom corners are rounded.
func roundRectContains(pointX, pointY, left, top, right, bottom int, radius float64, roundTop bool) bool {
	rad := int(math.Round(radius))
	if rad <= 0 {
		return rectContains(pointX, pointY, left, top, right, bottom)
	}

	if !rectContains(pointX, pointY, left, top, right, bottom) {
		return false
	}

	// Inside the straight bands, or in a square (non-rounded) top corner.
	if pointX >= left+rad && pointX < right-rad {
		return true
	}

	if pointY >= top+rad && pointY < bottom-rad {
		return true
	}

	if !roundTop && pointY < top+rad {
		return true
	}

	centreX, centreY := nearestCornerCentre(pointX, pointY, left, top, right, bottom, rad)
	distX := float64(pointX - centreX)
	distY := float64(pointY - centreY)

	return distX*distX+distY*distY <= radius*radius
}

// drawTextInRect draws text centred within the given rectangle.
func drawTextInRect(canvas *image.RGBA, face font.Face, col color.RGBA, text string, left, top, right, bottom int) {
	drawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(col),
		Face: face,
	}

	bounds, _ := drawer.BoundString(text)
	textWidth := drawer.MeasureString(text)

	xPos := fixed.I(left) + (fixed.I(right-left)-textWidth)/2
	yPos := fixed.I((top+bottom)/2) - (bounds.Max.Y+bounds.Min.Y)/2

	drawer.Dot = fixed.Point26_6{X: xPos, Y: yPos}
	drawer.DrawString(text)
}
