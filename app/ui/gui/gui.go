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
func (r *Screen) RenderSettingScreen(header string, value string) error {
	// header
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

	// value
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
