package pirateaudio

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"

	_ "image/png"

	"github.com/golang/freetype/truetype"
	"github.com/rubiojr/go-pirateaudio/display"
	"github.com/rubiojr/go-pirateaudio/textview"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const displayDPI float64 = 265

type PirateAudioLCD struct {
	font        *truetype.Font
	device      *display.Display
	orientation int
	sprites     *ui.SpriteSet
	dpi         float64
	poweredOn   bool
}

type PirateAudioLCDOpts struct {
	Orientation int
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

	sprites, err := ui.NewSpriteSet()
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	freetypeFont, err := ui.GetRegularFont()
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	lcd := &PirateAudioLCD{
		font:        freetypeFont,
		device:      lcdDevice,
		orientation: opts.Orientation,
		sprites:     sprites,
		dpi:         displayDPI,
		poweredOn:   true,
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
	l.poweredOn = true
}

func (l *PirateAudioLCD) PowerOff() {
	l.device.PowerOff()
	l.poweredOn = false
}

func (l *PirateAudioLCD) PowerToggle() bool {
	if l.poweredOn {
		l.PowerOff()
		return false
	}

	l.PowerOn()
	return true
}

func (l *PirateAudioLCD) IsPoweredOn() bool {
	return l.poweredOn
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

func (l *PirateAudioLCD) ShowTextOverlay(background string, text string, size int) {
	img := l.sprites.GetSprite(background)
	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, image.Point{0, 0}, draw.Src)

	fontFace := truetype.NewFace(l.font, &truetype.Options{
		Size:    float64(size),
		DPI:     l.dpi,
		Hinting: font.HintingFull,
	})

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(color.RGBA{6, 6, 6, 1}),
		Face: fontFace,
	}

	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(text)) / 2
	textBounds, _ := fontDrawer.BoundString(text)
	textHeight := textBounds.Max.Y - textBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Max.Y) - (textHeight.Ceil() / 2))
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}

	fontDrawer.DrawString(text)

	l.device.DrawRAW(canvas)
}
