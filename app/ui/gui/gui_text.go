package gui

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// vAnchor selects how drawText positions text vertically on the canvas.
type vAnchor int

const (
	// anchorTop places the baseline one text-height below the top edge.
	anchorTop vAnchor = iota
	// anchorMiddle centres the text vertically using its text height.
	anchorMiddle
	// anchorBottom anchors the text near the bottom edge.
	anchorBottom
	// anchorGlyphMiddle centres the glyph bounding box vertically, so values
	// with descenders stay centred rather than pushed down.
	anchorGlyphMiddle
)

// drawText draws text horizontally centred on the canvas, positioned vertically
// according to anchor, using the given face and colour.
func drawText(canvas *image.RGBA, face font.Face, col color.RGBA, text string, anchor vAnchor) {
	drawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(col),
		Face: face,
	}

	bounds, _ := drawer.BoundString(text)
	textHeight := bounds.Max.Y - bounds.Min.Y

	xPos := (fixed.I(canvas.Rect.Max.X) - drawer.MeasureString(text)) / 2

	var yPos fixed.Int26_6

	switch anchor {
	case anchorTop:
		yPos = fixed.I(canvas.Rect.Min.Y + textHeight.Ceil())
	case anchorMiddle:
		yPos = fixed.I(canvas.Rect.Max.Y-textHeight.Ceil())/2 + fixed.I(textHeight.Ceil())
	case anchorBottom:
		yPos = fixed.I(canvas.Rect.Max.Y - textHeight.Ceil()/2)
	case anchorGlyphMiddle:
		yPos = (fixed.I(canvas.Rect.Max.Y) - (bounds.Max.Y + bounds.Min.Y)) / 2
	}

	drawer.Dot = fixed.Point26_6{X: xPos, Y: yPos}
	drawer.DrawString(text)
}

// drawCenteredText draws text horizontally centred. When vMiddle is true, y is
// treated as the vertical centre of the glyph box; otherwise y is the baseline.
func (r *Screen) drawCenteredText(canvas *image.RGBA, text string, scale float64, col color.RGBA, yPos int, vMiddle bool) {
	fontFace := r.face(r.i18n.VariableFont().Font, r.i18n.VariableFont().Scale*scale)

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(col),
		Face: fontFace,
	}

	xPos := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(text)) / 2

	baseline := fixed.I(yPos)

	if vMiddle {
		bounds, _ := fontDrawer.BoundString(text)
		baseline = fixed.I(yPos) - (bounds.Max.Y+bounds.Min.Y)/2
	}

	fontDrawer.Dot = fixed.Point26_6{X: xPos, Y: baseline}
	fontDrawer.DrawString(text)
}

// Only ASCII digits are laid out in a uniform fixed-width cell; every other glyph
// (sign, decimal point, colon, °, %, letters, spaces) keeps its natural advance.
// This stops digits from shifting as a value changes while letting units and
// separators sit naturally rather than floating in wide gaps.

// numericGlyphCell returns the layout cell width for a single glyph: the uniform
// digit cell for ASCII digits, otherwise the glyph's natural advance.
func numericGlyphCell(fontFace font.Face, glyph rune, digitCell fixed.Int26_6) fixed.Int26_6 {
	if glyph >= '0' && glyph <= '9' {
		return digitCell
	}

	if advance, ok := fontFace.GlyphAdvance(glyph); ok {
		return advance
	}

	return digitCell
}

// numericRunWidth returns the total laid-out width of runes for the given face.
func numericRunWidth(fontFace font.Face, runes []rune, digitCell fixed.Int26_6) fixed.Int26_6 {
	var total fixed.Int26_6
	for _, glyph := range runes {
		total += numericGlyphCell(fontFace, glyph, digitCell)
	}

	return total
}

// drawNumericRun draws runes left-to-right starting at startX along baseline,
// centring each glyph within its layout cell.
func drawNumericRun(fontDrawer *font.Drawer, fontFace font.Face, runes []rune, digitCell, startX, baseline fixed.Int26_6) {
	penX := startX

	for _, glyph := range runes {
		glyphCell := numericGlyphCell(fontFace, glyph, digitCell)

		advance, ok := fontFace.GlyphAdvance(glyph)
		if !ok {
			advance = glyphCell
		}

		fontDrawer.Dot = fixed.Point26_6{X: penX + (glyphCell-advance)/2, Y: baseline}
		fontDrawer.DrawString(string(glyph))

		penX += glyphCell
	}
}

// drawCenteredNumeric draws a numeric string horizontally centred on the canvas
// using the numeric font, with yPos as the vertical centre of the glyph box.
// Digits occupy uniform cells so the value does not shift as they change. Vertical
// centring uses a fixed digit reference ("0") rather than the live string's bounds,
// so the baseline is stable regardless of which glyphs are present.
func (r *Screen) drawCenteredNumeric(canvas *image.RGBA, text string, scale float64, col color.RGBA, yPos int) {
	fontFace := r.face(r.i18n.VariableFont().Font, r.i18n.VariableFont().Scale*scale)

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(col),
		Face: fontFace,
	}

	digitCell := numericDigitCell(fontFace)
	runes := []rune(text)

	bounds, _ := fontDrawer.BoundString("0")
	baseline := fixed.I(yPos) - (bounds.Max.Y+bounds.Min.Y)/2

	startX := (fixed.I(canvas.Rect.Max.X) - numericRunWidth(fontFace, runes, digitCell)) / 2

	drawNumericRun(fontDrawer, fontFace, runes, digitCell, startX, baseline)
}

// drawNumericInRect draws a numeric string centred within the given rectangle
// using the numeric font, with the same digit-cell layout as drawCenteredNumeric.
func drawNumericInRect(canvas *image.RGBA, fontFace font.Face, col color.RGBA, text string, left, top, right, bottom int) {
	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(col),
		Face: fontFace,
	}

	digitCell := numericDigitCell(fontFace)
	runes := []rune(text)

	startX := fixed.I(left) + (fixed.I(right-left)-numericRunWidth(fontFace, runes, digitCell))/2

	bounds, _ := fontDrawer.BoundString("0")
	baseline := fixed.I((top+bottom)/2) - (bounds.Max.Y+bounds.Min.Y)/2

	drawNumericRun(fontDrawer, fontFace, runes, digitCell, startX, baseline)
}

// numericDigitCell returns the widest ASCII-digit advance for the given face, used
// as the uniform cell width so all digits occupy identical columns.
func numericDigitCell(fontFace font.Face) fixed.Int26_6 {
	var widest fixed.Int26_6

	for glyph := '0'; glyph <= '9'; glyph++ {
		if advance, ok := fontFace.GlyphAdvance(glyph); ok && advance > widest {
			widest = advance
		}
	}

	return widest
}
