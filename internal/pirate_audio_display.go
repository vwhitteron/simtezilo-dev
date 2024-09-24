package internal

import (
	"fmt"
	"image"
	"image/color"
	"os"

	_ "image/png"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/rubiojr/go-pirateaudio/display"
	"github.com/rubiojr/go-pirateaudio/textview"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type PirateAudioDisplay struct {
	font        *truetype.Font
	lcd         *display.Display
	orientation int
	sprites     *spriteSet
}

type PirateAudioDisplayOpts struct {
	Orientation int
	AssetDir    string
}

func NewPirateAudioDisplay(opts PirateAudioDisplayOpts) (*PirateAudioDisplay, error) {
	lcd, err := display.Init()
	if err != nil {
		return nil, fmt.Errorf("initializing display: %w", err)
	}

	switch opts.Orientation {
	case 90:
		lcd.Rotate(display.ROTATION_90)
	case 180:
		lcd.Rotate(display.ROTATION_180)
	case 270:
		lcd.Rotate(display.ROTATION_270)
	default:
		lcd.Rotate(display.NO_ROTATION)
	}

	sprites, err := NewSpriteSet(SpriteSetOpts{AssetDir: opts.AssetDir})
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	fontData, err := os.Open(opts.AssetDir + "/font/LeagueGothic-Regular.ttf")
	if err != nil {
		return nil, fmt.Errorf("open font file: %w", err)
	}

	fontBytes := make([]byte, 1024*100)
	_, err = fontData.Read(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("reading font data: %w", err)
	}

	freetypeFont, err := freetype.ParseFont(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	display := &PirateAudioDisplay{
		font:        freetypeFont,
		lcd:         lcd,
		orientation: opts.Orientation,
		sprites:     sprites,
	}

	display.Clear()

	return display, nil
}

func (d *PirateAudioDisplay) Clear() {
	d.lcd.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (d *PirateAudioDisplay) Close() {
	d.Clear()
	d.lcd.PowerOff()
	d.lcd.Close()
}

func (d *PirateAudioDisplay) GetOrientation() int {
	return d.orientation
}

func (d *PirateAudioDisplay) SetOrientation(orientation int) {
	d.orientation = orientation
	switch orientation {
	case 90:
		d.lcd.Rotate(display.ROTATION_90)
	case 180:
		d.lcd.Rotate(display.ROTATION_180)
	case 270:
		d.lcd.Rotate(display.ROTATION_270)
	default:
		d.lcd.Rotate(display.NO_ROTATION)
	}
}

func (d *PirateAudioDisplay) PowerOn() {
	d.lcd.PowerOn()
}

func (d *PirateAudioDisplay) PowerOff() {
	d.lcd.PowerOff()
}

func (d *PirateAudioDisplay) Show(sprite string) {
	img := d.sprites.GetSprite(sprite)
	d.lcd.DrawRAW(img)
}

func (d *PirateAudioDisplay) ShowText(text string) {
	d.Clear()
	opts := textview.DefaultOpts
	opts.FGColor = textview.GREEN
	opts.FontSize = 64
	tv := textview.NewWithOptions(opts)
	tv.DrawChars(text)
}

func (d *PirateAudioDisplay) ShowTextCentered(canvas *image.RGBA, text string, size int) {
	fontFace := truetype.NewFace(d.font, &truetype.Options{
		Size:    float64(size),
		DPI:     265,
		Hinting: font.HintingFull,
	})

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(color.RGBA{255, 255, 255, 1}),
		Face: fontFace,
		// Face: basicfont.Face7x13,
		// Face: inconsolata.Bold8x16,
		// Face: bitmapfont.Gothic12r,
	}

	textBounds, _ := fontDrawer.BoundString(text)
	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(text)) / 2
	textHeight := textBounds.Max.Y - textBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Max.Y)-textHeight.Ceil())/2 + fixed.I(textHeight.Ceil())
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}

	fontDrawer.DrawString(text)

	d.lcd.DrawRAW(canvas)
}
