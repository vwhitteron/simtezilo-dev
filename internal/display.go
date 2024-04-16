package internal

import (
	"fmt"
	"image/color"

	_ "image/png"

	"github.com/vwhitteron/go-pirateaudio/display"
	"github.com/vwhitteron/go-pirateaudio/textview"
)

type spriteBox struct {
	x, y, w, h int
}

type Display struct {
	lcd      *display.Display
	rotation int
	sprites  *spriteSet
}

func NewPirateAudioDisplay(rotation int, spriteFile string) (*Display, error) {
	lcd, err := display.Init()
	if err != nil {
		return nil, fmt.Errorf("initializing display: %w", err)
	}

	switch rotation {
	case 90:
		lcd.Rotate(display.ROTATION_90)
	case 180:
		lcd.Rotate(display.ROTATION_180)
	case 270:
		lcd.Rotate(display.ROTATION_270)
	default:
		lcd.Rotate(display.NO_ROTATION)
	}

	lcd.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})

	sprites, err := NewSpriteSet(spriteFile)
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	return &Display{
		lcd:      lcd,
		rotation: rotation,
		sprites:  sprites,
	}, nil
}

func (d *Display) Clear() {
	d.lcd.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (d *Display) Show(sprite string) {
	img := d.sprites.GetSprite(sprite)
	d.lcd.DrawRAW(img)
}

func (d *Display) ShowText(text string) {
	d.Clear()
	opts := textview.DefaultOpts
	opts.FGColor = textview.GREEN
	opts.FontSize = 64
	tv := textview.NewWithOptions(opts)
	tv.DrawChars(text)
}

func (d *Display) Close() {
	d.lcd.PowerOff()
	d.lcd.Close()
}
