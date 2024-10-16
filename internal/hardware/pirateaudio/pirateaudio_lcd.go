package pirateaudio

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
	"github.com/vwhitteron/gt-pi/internal/display/sprites"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type PirateAudioLCD struct {
	font        *truetype.Font
	device      *display.Display
	orientation int
	sprites     *sprites.SpriteSet
}

type PirateAudioLCDOpts struct {
	Orientation int
	AssetDir    string
}

func NewPirateAudioLCD(opts PirateAudioLCDOpts) (*PirateAudioLCD, error) {
	lcdDevice, err := display.Init()
	if err != nil {
		return nil, fmt.Errorf("initializing display: %w", err)
	}

	switch opts.Orientation {
	case 90:
		lcdDevice.Rotate(display.ROTATION_90)
	case 180:
		lcdDevice.Rotate(display.ROTATION_180)
	case 270:
		lcdDevice.Rotate(display.ROTATION_270)
	default:
		lcdDevice.Rotate(display.NO_ROTATION)
	}

	sprites, err := sprites.NewSpriteSet(sprites.SpriteSetOpts{AssetDir: opts.AssetDir})
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

	lcd := &PirateAudioLCD{
		font:        freetypeFont,
		device:      lcdDevice,
		orientation: opts.Orientation,
		sprites:     sprites,
	}

	lcd.Clear()

	return lcd, nil
}

func (l *PirateAudioLCD) Clear() {
	l.device.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (l *PirateAudioLCD) Close() {
	l.Clear()
	l.device.PowerOff()
	l.device.Close()
}

func (l *PirateAudioLCD) GetOrientation() int {
	return l.orientation
}

func (l *PirateAudioLCD) SetOrientation(orientation int) {
	l.orientation = orientation
	switch orientation {
	case 90:
		l.device.Rotate(display.ROTATION_90)
	case 180:
		l.device.Rotate(display.ROTATION_180)
	case 270:
		l.device.Rotate(display.ROTATION_270)
	default:
		l.device.Rotate(display.NO_ROTATION)
	}
}

func (l *PirateAudioLCD) PowerOn() {
	l.device.PowerOn()
}

func (l *PirateAudioLCD) PowerOff() {
	l.device.PowerOff()
}

func (l *PirateAudioLCD) Show(sprite string) {
	img := l.sprites.GetSprite(sprite)
	l.device.DrawRAW(img)
}

func (l *PirateAudioLCD) ShowText(text string) {
	l.Clear()
	opts := textview.DefaultOpts
	opts.FGColor = textview.GREEN
	opts.FontSize = 64
	tv := textview.NewWithOptions(opts)
	tv.DrawChars(text)
}

func (l *PirateAudioLCD) ShowTextCentered(canvas *image.RGBA, text string, size int) {
	fontFace := truetype.NewFace(l.font, &truetype.Options{
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

	l.device.DrawRAW(canvas)
}
