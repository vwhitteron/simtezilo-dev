package gui

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/golang/freetype/truetype"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/ui/icons"
	"github.com/vwhitteron/simtezilo-dev/app/ui/sprites"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Config holds the configuration for creating a new Screen instance.
type Config struct {
	DisplayDevice hardware.Display
	I18n          *i18n.I18n
}

// SettingContent holds the text fields for a setting, info, or sub-menu screen.
type SettingContent struct {
	Title string // parent menu name or page title, drawn at the top
	Name  string // setting name, drawn at the bottom; empty for info/sub-menu
	Value string // setting value, menu item, or multi-line info body, drawn centre
}

// faceKey is the cache key for a truetype face.
type faceKey struct {
	font *truetype.Font
	size float64
	dpi  float64
}

// Screen holds the state for rendering to a connected display.
type Screen struct {
	mu            sync.Mutex
	canvases      [2]*image.RGBA // double-buffered: the display retains the previous frame for rotation re-blit while the next is drawn
	canvasIdx     int
	faceCache     map[faceKey]font.Face
	arcSamples    [][]image.Point // precomputed radial pixels per angle step
	displayDevice hardware.Display
	pixelColumns  uint16
	pixelRows     uint16
	dpi           float64
	sprites       *sprites.SpriteSet
	i18n          *i18n.I18n
	fanIcon       *image.Alpha // rasterised once; tinted per draw for the fan footer
}

type Layout int

const (
	LayoutSplash Layout = iota
	LayoutError
	LayoutLive
	LayoutSetting
	LayoutInfo
	LayoutMenuSub
)

// NewScreen creates a new Screen instance.
func NewScreen(config *Config) (*Screen, error) {
	pixelColumns, pixelRows := config.DisplayDevice.GetResolution()
	dpi := config.DisplayDevice.GetDPI()

	sprites, err := sprites.NewSpriteSet()
	if err != nil {
		return nil, fmt.Errorf("loading sprites: %w", err)
	}

	// Rasterise the fan footer icon once. A failure is non-fatal: the footer
	// simply falls back to showing the percentage without an icon.
	fanIcon, _ := icons.Render("fan", fanIconSize)

	return &Screen{
		canvases: [2]*image.RGBA{
			image.NewRGBA(image.Rect(0, 0, int(pixelColumns), int(pixelRows))),
			image.NewRGBA(image.Rect(0, 0, int(pixelColumns), int(pixelRows))),
		},
		faceCache:     make(map[faceKey]font.Face),
		arcSamples:    buildArcSamples(int(pixelColumns), int(pixelRows)),
		displayDevice: config.DisplayDevice,
		pixelColumns:  pixelColumns,
		pixelRows:     pixelRows,
		dpi:           dpi,
		sprites:       sprites,
		i18n:          config.I18n,
		fanIcon:       fanIcon,
	}, nil
}

// RenderSplashScreen renders the splash screen with the provided value.
func (r *Screen) RenderSplashScreen(value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.renderBackgroundScreen(sprites.SplashSprite, value)
}

// RenderErrorScreen renders the error screen with the provided value.
func (r *Screen) RenderErrorScreen(value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.renderBackgroundScreen(sprites.ErrorSprite, value)
}

// RenderBlankScreen renders a blank screen.
func (r *Screen) RenderBlankScreen() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canvas := r.newBlankCanvas()

	err := r.displayDevice.Write(&display.Content{Canvas: canvas})
	if err != nil {
		return fmt.Errorf("write blank canvas to display: %w", err)
	}

	return nil
}

// RenderLiveScreen renders the live screen with the provided gear or status value
// and a fan-speed footer (e.g. "Fan 42%") centered along the bottom edge.
func (r *Screen) RenderLiveScreen(value string, fan string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	canvas := r.newBlankCanvas()

	// Gears are single characters and use the large font; multi-character status
	// values such as "Ready" or "Calibrating" render in a smaller font.
	fontSize := r.i18n.RegularFont().Scale * fontXLarge
	if utf8.RuneCountInString(value) > 1 {
		fontSize = r.i18n.RegularFont().Scale * fontStatus
	}

	fontFace := r.face(r.i18n.RegularFont().Font, fontSize)

	// Safety net: shrink further if the value still overflows the panel width.
	maxWidth := fixed.I(canvas.Rect.Max.X) * liveValueWidthPercent / 100

	measurer := &font.Drawer{Face: fontFace}
	if width := measurer.MeasureString(value); width > maxWidth {
		fontSize = fontSize * float64(maxWidth) / float64(width)
		fontFace = r.face(r.i18n.RegularFont().Font, fontSize)
	}

	drawText(canvas, fontFace, valueColor(), value, anchorGlyphMiddle)

	r.drawFanFooter(canvas, fan)

	content := &display.Content{
		Text:   "Live: " + value,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(content)
	if err != nil {
		return fmt.Errorf("write settings canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// RenderSettingScreen renders a setting, info, or sub-menu screen.
func (r *Screen) RenderSettingScreen(layout Layout, content SettingContent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch layout { //nolint:exhaustive // only interested in setting screen layouts
	case LayoutSetting:
		return r.renderLeafNode(content)
	case LayoutInfo:
		return r.renderLayoutInfo(content)
	case LayoutMenuSub:
		return r.renderLayoutMenuSub(content)
	default:
		return fmt.Errorf("unknown layout type: %d", layout)
	}
}

// renderLeafNode renders a leaf node: parent menu at top, value in centre
// (larger font), setting name at bottom.
func (r *Screen) renderLeafNode(content SettingContent) error {
	canvas := r.newBlankCanvas()

	// Parent menu at top.
	headerFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontSmall)
	drawText(canvas, headerFace, mediumGrayColor(), content.Title, anchorTop)

	// Setting value in centre (larger font; the Info page uses a smaller font).
	fontScale := fontLarge
	if strings.ToLower(content.Title) == "info" {
		fontScale = fontSmall
	}

	valueFace := r.face(r.i18n.ValueFont().Font, r.i18n.ValueFont().Scale*fontScale)
	drawText(canvas, valueFace, valueColor(), content.Value, anchorMiddle)

	// Setting name at bottom (if provided).
	if content.Name != "" {
		nameFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontMedium)
		drawText(canvas, nameFace, lightGrayColor(), content.Name, anchorBottom)
	}

	dspContent := &display.Content{
		Text:   "Setting " + content.Title + ": " + content.Name + "=" + content.Value,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(dspContent)
	if err != nil {
		return fmt.Errorf("write settings canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// renderLayoutMenuSub renders a branch menu with optional parent name at top and
// current item in centre.
func (r *Screen) renderLayoutMenuSub(content SettingContent) error {
	canvas := r.newBlankCanvas()

	// Parent name at top (if provided).
	if content.Title != "" {
		titleFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontMedium)
		drawText(canvas, titleFace, valueColor(), content.Title, anchorTop)
	}

	// Current item in centre.
	itemFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontLarge)
	drawText(canvas, itemFace, valueColor(), content.Value, anchorMiddle)

	dspContent := &display.Content{
		Text:   "Menu " + content.Title + ": " + content.Value,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(dspContent)
	if err != nil {
		return fmt.Errorf("write submenu canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// renderLayoutInfo renders an info page: title at top, multi-line value centred
// as a group.
func (r *Screen) renderLayoutInfo(content SettingContent) error {
	canvas := r.newBlankCanvas()

	// Title.
	titleFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontMedium)
	drawText(canvas, titleFace, valueColor(), content.Title, anchorTop)

	// Value - split into multiple lines.
	fontFace := r.face(r.i18n.ValueFont().Font, r.i18n.RegularFont().Scale*fontSmall)

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(valueColor()),
		Face: fontFace,
	}

	// Split value into lines
	lines := []string{}
	currentLine := ""

	for _, char := range content.Value {
		if char == '\n' {
			lines = append(lines, currentLine)
			currentLine = ""
		} else {
			currentLine += string(char)
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	// Calculate total height needed for all lines
	var lineHeight fixed.Int26_6

	if len(lines) > 0 {
		sampleBounds, _ := fontDrawer.BoundString(lines[0])
		lineHeight = sampleBounds.Max.Y - sampleBounds.Min.Y
	}

	totalHeight := lineHeight * fixed.Int26_6(len(lines))                           //nolint:gosec // pixel res too small for overflow
	lineSpacing := (lineHeight * 3) / 4                                             // 75% of line height as spacing
	totalHeightWithSpacing := totalHeight + lineSpacing*fixed.Int26_6(len(lines)-1) //nolint:gosec // pixel res too small for overflow

	// Start Y position to center all lines as a group
	startY := (fixed.I(canvas.Rect.Max.Y) - totalHeightWithSpacing) / 2

	// Draw each line
	for i, line := range lines {
		xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(line)) / 2
		yPosition := startY + lineHeight*fixed.Int26_6(i+1) + lineSpacing*fixed.Int26_6(i) //nolint:gosec // pixel res too small for overflow
		fontDrawer.Dot = fixed.Point26_6{
			X: xPosition,
			Y: yPosition,
		}
		fontDrawer.DrawString(line)
	}

	dspContent := &display.Content{
		Text:   "Setting " + content.Title + ": " + content.Value,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(dspContent)
	if err != nil {
		return fmt.Errorf("write settings canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// face returns a cached truetype.Face for the given font and size, building one
// on the first miss. Callers must hold r.mu.
//
//nolint:ireturn // font.Face is a library interface
func (r *Screen) face(fontDef *truetype.Font, size float64) font.Face {
	key := faceKey{font: fontDef, size: size, dpi: r.dpi}
	if faceCache, ok := r.faceCache[key]; ok {
		return faceCache
	}

	faceCache := truetype.NewFace(fontDef, &truetype.Options{
		Size:    size,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})
	r.faceCache[key] = faceCache

	return faceCache
}

// newBlankCanvas advances to the next of the two shared canvas buffers, zeroes
// it, and returns it. Double-buffering ensures the buffer the display still holds
// for a rotation re-blit (the previous frame) is never the one being drawn into.
// Callers must hold r.mu.
func (r *Screen) newBlankCanvas() *image.RGBA {
	r.canvasIdx ^= 1
	canvas := r.canvases[r.canvasIdx]

	for i := range canvas.Pix {
		canvas.Pix[i] = 0
	}

	return canvas
}

// renderBackgroundScreen renders a background screen with a centered value.
func (r *Screen) renderBackgroundScreen(sprite sprites.SpriteName, value string) error {
	// footer
	fontFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontSmall)

	img := r.sprites.GetSprite(sprite)
	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, image.Point{0, 0}, draw.Src)

	drawText(canvas, fontFace, mediumGrayColor(), value, anchorBottom)

	content := &display.Content{
		Text:   "Splash: " + value,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(content)
	if err != nil {
		return fmt.Errorf("write splash canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// fanFooterBottomPadding is the gap in pixels between the fan footer baseline and
// the bottom edge of the canvas.
const fanFooterBottomPadding = 6

// drawFanFooter draws the fan icon followed by the fan percentage (e.g. "42%")
// horizontally centered along the bottom edge of the canvas. An empty string
// draws nothing.
func (r *Screen) drawFanFooter(canvas *image.RGBA, fan string) {
	if fan == "" {
		return
	}

	r.drawIconLabel(canvas, r.fanIcon, fan, fanFooterColor(), canvas.Rect.Max.Y-fanFooterBottomPadding)
}

// drawIconLabel draws an icon mask immediately to the left of a small label, the
// pair centered horizontally, with the label baseline at baselineY. icon may be
// nil, in which case only the label is drawn. The icon is tinted with col.
func (r *Screen) drawIconLabel(canvas *image.RGBA, icon *image.Alpha, label string, col color.RGBA, baselineY int) {
	fontFace := truetype.NewFace(r.i18n.RegularFont().Font, &truetype.Options{
		Size:    r.i18n.RegularFont().Scale * fontSmall,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(col),
		Face: fontFace,
	}

	labelWidth := fontDrawer.MeasureString(label)

	iconWidth, gap := 0, 0
	if icon != nil {
		iconWidth = icon.Bounds().Dx()
		gap = fanIconGap
	}

	startX := (fixed.I(canvas.Rect.Max.X) - (fixed.I(iconWidth+gap) + labelWidth)) / 2

	if icon != nil {
		bounds, _ := fontDrawer.BoundString(label)
		labelTop := baselineY + bounds.Min.Y.Floor()
		labelBottom := baselineY + bounds.Max.Y.Ceil()

		iconHeight := icon.Bounds().Dy()
		iconX := startX.Round()
		iconY := (labelTop+labelBottom)/2 - iconHeight/2

		draw.DrawMask(
			canvas,
			image.Rect(iconX, iconY, iconX+iconWidth, iconY+iconHeight),
			image.NewUniform(col), image.Point{},
			icon, icon.Bounds().Min,
			draw.Over,
		)
	}

	fontDrawer.Dot = fixed.Point26_6{X: startX + fixed.I(iconWidth+gap), Y: fixed.I(baselineY)}
	fontDrawer.DrawString(label)
}

// ImageToRGBA converts an image.Image to *image.RGBA.
func ImageToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}

	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}

	return rgba
}
