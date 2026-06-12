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
	fontFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*scale)

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
