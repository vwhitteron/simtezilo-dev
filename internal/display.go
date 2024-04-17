package internal

import (
	"fmt"
	"image"
	"image/color"
	"os"

	_ "image/png"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/vwhitteron/go-pirateaudio/display"
	"github.com/vwhitteron/go-pirateaudio/textview"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type spriteBox struct {
	x, y, w, h int
}

type Display struct {
	lcd      *display.Display
	rotation int
	sprites  *spriteSet
	font     *truetype.Font
}

func NewPirateAudioDisplay(rotation int, assetDir string) (*Display, error) {
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

	sprites, err := NewSpriteSet(assetDir)
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	// fontData, err := os.Open(goregular.TTF)
	fontData, err := os.Open(assetDir + "/font/LeagueGothic-Regular.ttf")
	if err != nil {
		return nil, fmt.Errorf("open font file: %w", err)
	}

	fontBytes := make([]byte, 1024*100)
	bytesRead, err := fontData.Read(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("reading font data: %w", err)
	}

	fmt.Printf("Read %d bytes from font file\n", bytesRead)

	ftFont, err := freetype.ParseFont(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	return &Display{
		lcd:      lcd,
		rotation: rotation,
		sprites:  sprites,
		font:     ftFont,
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

func (d *Display) ShowTextCentered(canvas *image.RGBA, text string) {
	col := color.RGBA{255, 255, 255, 1}

	fontFace := truetype.NewFace(d.font, &truetype.Options{
		Size:    48,
		DPI:     265,
		Hinting: font.HintingFull,
	})

	fontDrawer := &font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(col),
		Face: fontFace,
		// Face: basicfont.Face7x13,
		// Face: inconsolata.Bold8x16,
		// Face: bitmapfont.Gothic12r,
		// Dot: point,
	}

	textBounds, _ := fontDrawer.BoundString(text)
	xPosition := (fixed.I(canvas.Rect.Max.X) - fontDrawer.MeasureString(text)) / 2
	textHeight := textBounds.Max.Y - textBounds.Min.Y
	yPosition := fixed.I((canvas.Rect.Max.Y)-textHeight.Ceil())/2 + fixed.I(textHeight.Ceil())
	fontDrawer.Dot = fixed.Point26_6{
		X: xPosition,
		Y: yPosition,
	}

	fmt.Printf("Text bounds: %+v\n", textBounds)
	fmt.Printf("Text position: %+v\n", fontDrawer.Dot)

	fontDrawer.DrawString(text)

	d.lcd.DrawRAW(canvas)
}

func (d *Display) Close() {
	d.lcd.PowerOff()
	d.lcd.Close()
}
