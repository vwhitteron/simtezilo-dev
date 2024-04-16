package internal

import (
	"image/color"

	_ "image/png"

	"github.com/vwhitteron/go-pirateaudio/display"
)

type spriteBox struct {
	x, y, w, h int
}

type Display struct {
	LCD      *display.Display
	rotation int
}

func NewPirateAudioDisplay(rotation int) (*Display, error) {
	lcd, err := display.Init()
	if err != nil {
		return nil, err
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

	return &Display{LCD: lcd, rotation: rotation}, nil
}

// func (d *Display) Show(screen string) {
// 	d.LCD.DrawRaw(img)
// }

func (d *Display) Close() {
	d.LCD.PowerOff()
	d.LCD.Close()
}
