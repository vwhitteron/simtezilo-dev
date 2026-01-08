package gui

import (
	"fmt"
	"image"
	"image/draw"

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

// Screen holds the state for rendering to a connected display.
type Screen struct {
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
	return r.renderBackgroundScreen(sprites.SplashSprite, value)
}

// RenderErrorScreen renders the error screen with the provided value.
func (r *Screen) RenderErrorScreen(value string) error {
	return r.renderBackgroundScreen(sprites.ErrorSprite, value)
}

// RenderBlankScreen renders a blank screen.
func (r *Screen) RenderBlankScreen() error {
	canvas := r.newBlankCanvas()

	err := r.displayDevice.Write(&display.Content{Canvas: canvas})
	if err != nil {
		return fmt.Errorf("write blank canvas to display: %w", err)
	}

	return nil
}

// RenderLiveScreen renders the live screen with the provided value.
func (r *Screen) RenderLiveScreen(value string) error {
	// value
	fontFace := truetype.NewFace(r.i18n.RegularFont().Font, &truetype.Options{
		Size:    r.i18n.RegularFont().Scale * valueLargeSize,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})

	canvas := r.newBlankCanvas()
	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(valueColor()),
		Face: fontFace,
	}

	textBounds, _ := fontDrawer.BoundString(value)
	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(value)) / 2
	textHeight := textBounds.Max.Y - textBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Max.Y)-textHeight.Ceil())/2 + fixed.I(textHeight.Ceil())
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
	switch layout { //nolint:exhaustive // only interested in setting screen layouts
	case LayoutSetting:
		return r.renderLayoutSetting(title, value)
	case LayoutInfo:
		return r.renderLayoutInfo(title, value)
	default:
		return fmt.Errorf("unknown layout type: %d", layout)
	}
}

// RenderSettingScreen renders the setting screen with the provided header and value.
func (r *Screen) renderLayoutSetting(header string, value string) error {
	// Title
	fontFace := truetype.NewFace(r.i18n.RegularFont().Font, &truetype.Options{
		Size:    r.i18n.RegularFont().Scale * headerSize,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})

	canvas := r.newBlankCanvas()
	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(headerColor()),
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

	// Value
	fontFace = truetype.NewFace(r.i18n.ValueFont().Font, &truetype.Options{
		Size:    r.i18n.ValueFont().Scale * valueSmallSize,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})

	fontDrawer = &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(valueColor()),
		Face: fontFace,
	}

	valueBounds, _ := fontDrawer.BoundString(value)
	xPosition = (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(value)) / 2
	textHeight = valueBounds.Max.Y - valueBounds.Min.Y
	yPosition = fixed.I((canvas.Rect.Max.Y)-textHeight.Ceil())/2 + fixed.I(textHeight.Ceil())
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}
	fontDrawer.DrawString(value)

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

// RenderSettingScreen renders the setting screen with the provided header and value.
func (r *Screen) renderLayoutInfo(header string, value string) error {
	// Title
	fontFace := truetype.NewFace(r.i18n.RegularFont().Font, &truetype.Options{
		Size:    r.i18n.RegularFont().Scale * headerSize,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})

	canvas := r.newBlankCanvas()
	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(headerColor()),
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
	fontFace = truetype.NewFace(r.i18n.ValueFont().Font, &truetype.Options{
		Size:    r.i18n.RegularFont().Scale * footerSize,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})

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

	totalHeight := lineHeight * fixed.Int26_6(len(lines))
	lineSpacing := (lineHeight * 3) / 4 // 75% of line height as spacing
	totalHeightWithSpacing := totalHeight + lineSpacing*fixed.Int26_6(len(lines)-1)

	// Start Y position to center all lines as a group
	startY := (fixed.I(canvas.Rect.Max.Y) - totalHeightWithSpacing) / 2

	// Draw each line
	for i, line := range lines {
		xPosition = (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(line)) / 2
		yPosition = startY + lineHeight*fixed.Int26_6(i+1) + lineSpacing*fixed.Int26_6(i)
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

// newBlankCanvas creates a new blank RGBA canvas with the screen's resolution.
func (r *Screen) newBlankCanvas() *image.RGBA {
	return image.NewRGBA(image.Rect(0, 0, int(r.pixelColumns), int(r.pixelRows)))
}

// renderBackgroundScreen renders a background screen with a centered value.
func (r *Screen) renderBackgroundScreen(sprite sprites.SpriteName, value string) error {
	// footer
	fontFace := truetype.NewFace(r.i18n.RegularFont().Font, &truetype.Options{
		Size:    r.i18n.RegularFont().Scale * footerSize,
		DPI:     r.dpi,
		Hinting: font.HintingFull,
	})

	img := r.sprites.GetSprite(sprite)
	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, image.Point{0, 0}, draw.Src)

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(footerColor()),
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
