package gui

import (
	"fmt"
	"image"
	"image/draw"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/golang/freetype/truetype"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/ui/sprites"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Config holds the configuration for creating a new Screen instance.
type Config struct {
	DisplayDevice hardware.Display
	I18n          *i18n.I18n
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

// RenderLiveScreen renders the live screen with the provided gear or status value.
func (r *Screen) RenderLiveScreen(value string) error {
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

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(valueColor()),
		Face: fontFace,
	}

	// Safety net: shrink further if the value still overflows the panel width.
	maxWidth := fixed.I(canvas.Rect.Max.X) * liveValueWidthPercent / 100
	if width := fontDrawer.MeasureString(value); width > maxWidth {
		fontSize = fontSize * float64(maxWidth) / float64(width)
		fontDrawer.Face = r.face(r.i18n.RegularFont().Font, fontSize)
	}

	// Center the glyph bounding box vertically. Using the box midpoint
	// (Min.Y is above the baseline and negative, Max.Y below and positive)
	// keeps values with descenders such as "Ready" centered rather than
	// pushed down by the descender height.
	textBounds, _ := fontDrawer.BoundString(value)
	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(value)) / 2
	yPosition := (fixed.I(canvas.Rect.Max.Y) - (textBounds.Max.Y + textBounds.Min.Y)) / 2
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}

	fontDrawer.DrawString(value)

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

// RenderSettingScreen renders the setting screen with the provided header and value.
func (r *Screen) RenderSettingScreen(layout Layout, title string, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch layout { //nolint:exhaustive // only interested in setting screen layouts
	case LayoutSetting:
		return r.renderLeafNode(title, value)
	case LayoutInfo:
		return r.renderLayoutInfo(title, value)
	case LayoutMenuSub:
		return r.renderLayoutMenuSub(title, value)
	default:
		return fmt.Errorf("unknown layout type: %d", layout)
	}
}

// renderLeafNode renders a leaf node: parent menu at top, value in center (larger font), setting name at bottom.
// header = parent menu name, value = "settingName|settingValue" (split on |).
func (r *Screen) renderLeafNode(header string, value string) error {
	// Parse value to extract setting name and setting value
	settingName := ""
	settingValue := value
	// Check if value contains a pipe separator
	for i, char := range value {
		if char == '|' {
			settingName = value[:i]
			settingValue = value[i+1:]

			break
		}
	}

	canvas := r.newBlankCanvas()

	// Parent menu at top
	fontFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontSmall)

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(mediumGrayColor()),
		Face: fontFace,
	}

	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(header)) / 2
	titleBounds, _ := fontDrawer.BoundString(header)
	textHeight := titleBounds.Max.Y - titleBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Min.Y) + textHeight.Ceil())
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}
	fontDrawer.DrawString(header)

	// Setting value in center (larger font)
	fontScale := fontLarge
	if strings.ToLower(header) == "info" {
		fontScale = fontSmall
	}

	fontFace = r.face(r.i18n.ValueFont().Font, r.i18n.ValueFont().Scale*fontScale)

	fontDrawer = &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(whiteColor()),
		Face: fontFace,
	}

	valueBounds, _ := fontDrawer.BoundString(settingValue)
	xPosition = (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(settingValue)) / 2
	textHeight = valueBounds.Max.Y - valueBounds.Min.Y
	yPosition = fixed.I((canvas.Rect.Max.Y)-textHeight.Ceil())/2 + fixed.I(textHeight.Ceil())
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}
	fontDrawer.DrawString(settingValue)

	// Setting name at bottom (if provided)
	if settingName != "" {
		fontFace = r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontMedium)

		fontDrawer = &font.Drawer{
			Dst:  canvas,
			Src:  image.NewUniform(lightGrayColor()),
			Face: fontFace,
		}

		xPosition = (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(settingName)) / 2
		nameBounds, _ := fontDrawer.BoundString(settingName)
		textHeight = nameBounds.Max.Y - nameBounds.Min.Y
		yPosition = fixed.I((canvas.Rect.Max.Y) - (textHeight.Ceil() / 2))
		fontDrawer.Dot = fixed.Point26_6{
			X: xPosition,
			Y: yPosition,
		}
		fontDrawer.DrawString(settingName)
	}

	content := &display.Content{
		Text:   "Setting " + header + ": " + settingName + "=" + settingValue,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(content)
	if err != nil {
		return fmt.Errorf("write settings canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// renderLayoutMenuSub renders a branch menu with optional parent name at top and current item in center.
func (r *Screen) renderLayoutMenuSub(parentName string, currentItem string) error {
	canvas := r.newBlankCanvas()

	// Parent name at top (if provided)
	if parentName != "" {
		fontFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontMedium)

		fontDrawer := &font.Drawer{
			Dst:  canvas,
			Src:  image.NewUniform(whiteColor()),
			Face: fontFace,
		}

		xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(parentName)) / 2
		titleBounds, _ := fontDrawer.BoundString(parentName)
		textHeight := titleBounds.Max.Y - titleBounds.Min.Y
		yPosition := fixed.I((canvas.Rect.Min.Y) + textHeight.Ceil())
		fontDrawer.Dot = fixed.Point26_6{
			X: xPosition,
			Y: yPosition,
		}
		fontDrawer.DrawString(parentName)
	}

	// Current item in center
	fontFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontLarge)

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(valueColor()),
		Face: fontFace,
	}

	itemBounds, _ := fontDrawer.BoundString(currentItem)
	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(currentItem)) / 2
	textHeight := itemBounds.Max.Y - itemBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Max.Y)-textHeight.Ceil())/2 + fixed.I(textHeight.Ceil())
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}
	fontDrawer.DrawString(currentItem)

	content := &display.Content{
		Text:   "Menu " + parentName + ": " + currentItem,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(content)
	if err != nil {
		return fmt.Errorf("write submenu canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// RenderSettingScreen renders the setting screen with the provided header and value.
func (r *Screen) renderLayoutInfo(header string, value string) error {
	// Title
	fontFace := r.face(r.i18n.RegularFont().Font, r.i18n.RegularFont().Scale*fontMedium)

	canvas := r.newBlankCanvas()
	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(whiteColor()),
		Face: fontFace,
	}

	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(header)) / 2
	titleBounds, _ := fontDrawer.BoundString(header)
	textHeight := titleBounds.Max.Y - titleBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Min.Y) + textHeight.Ceil())
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}
	fontDrawer.DrawString(header)

	// Value - split into multiple lines
	fontFace = r.face(r.i18n.ValueFont().Font, r.i18n.RegularFont().Scale*fontSmall)

	fontDrawer = &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(valueColor()),
		Face: fontFace,
	}

	// Split value into lines
	lines := []string{}
	currentLine := ""

	for _, char := range value {
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
		xPosition = (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(line)) / 2
		yPosition = startY + lineHeight*fixed.Int26_6(i+1) + lineSpacing*fixed.Int26_6(i) //nolint:gosec // pixel res too small for overflow
		fontDrawer.Dot = fixed.Point26_6{
			X: xPosition,
			Y: yPosition,
		}
		fontDrawer.DrawString(line)
	}

	content := &display.Content{
		Text:   "Setting " + header + ": " + value,
		Canvas: canvas,
	}

	err := r.displayDevice.Write(content)
	if err != nil {
		return fmt.Errorf("write settings canvas to display: %w", err)
	}

	r.displayDevice.Wakeup()

	return nil
}

// face returns a cached truetype.Face for the given font and size, building one
// on the first miss. Callers must hold r.mu.
func (r *Screen) face(f *truetype.Font, size float64) font.Face {
	k := faceKey{font: f, size: size, dpi: r.dpi}
	if fc, ok := r.faceCache[k]; ok {
		return fc
	}

	fc := truetype.NewFace(f, &truetype.Options{
		Size:    size,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})
	r.faceCache[k] = fc

	return fc
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

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(mediumGrayColor()),
		Face: fontFace,
	}

	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(value)) / 2
	textBounds, _ := fontDrawer.BoundString(value)
	textHeight := textBounds.Max.Y - textBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Max.Y) - (textHeight.Ceil() / 2))
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}

	fontDrawer.DrawString(value)

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
