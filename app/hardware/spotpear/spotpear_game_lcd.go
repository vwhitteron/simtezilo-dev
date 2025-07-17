package spotpear

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"sync"

	_ "image/png"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/rubiojr/go-pirateaudio/st7789"
	"github.com/rubiojr/go-pirateaudio/textview"
	"github.com/vwhitteron/simtezilo-dev/app/gui"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"periph.io/x/conn/v3/driver/driverreg"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

type SpotpearGameDisplay struct {
	port spi.PortCloser
	dev  *st7789.Device

	font        *truetype.Font
	orientation int
	sprites     *gui.SpriteSet
}

type SpotpearGameDisplayOpts struct {
	Orientation int
	AssetDir    string
}

var once sync.Once

func init() {
	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}

	if _, err := driverreg.Init(); err != nil {
		log.Fatal(err)
	}

}

func NewSpotpearGameDisplay(opts SpotpearGameDisplayOpts) (*SpotpearGameDisplay, error) {
	var err error
	var spiPort spi.PortCloser
	var lcdDevice *st7789.Device
	once.Do(func() {
		spiPort, err = spireg.Open("SPI0.1")
		if err != nil {
			return
		}
		dataComm := gpioreg.ByName("GPIO25")
		lcdDevice, err = st7789.NewSPI(spiPort.(spi.Port), dataComm, &st7789.DefaultOpts)
	})
	if err != nil {
		return nil, fmt.Errorf("initializing display: %w", err)
	}

	switch opts.Orientation {
	case 90:
		lcdDevice.SetRotation(st7789.ROTATION_90)
	case 180:
		lcdDevice.SetRotation(st7789.ROTATION_180)
	case 270:
		lcdDevice.SetRotation(st7789.ROTATION_270)
	default:
		lcdDevice.SetRotation(st7789.ROTATION_NONE)
	}

	sprites, err := gui.NewSpriteSet(gui.SpriteSetOpts{AssetDir: opts.AssetDir})
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	fontData, err := os.Open(opts.AssetDir + "/font/LeagueGothic-Regular.ttf") // TODO: use go:embed
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

	lcd := &SpotpearGameDisplay{
		port:        spiPort,
		dev:         lcdDevice,
		font:        freetypeFont,
		orientation: opts.Orientation,
		sprites:     sprites,
	}

	lcd.Clear()

	return lcd, nil
}

func (d *SpotpearGameDisplay) Clear() {
	d.dev.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (d *SpotpearGameDisplay) Close() {
	d.Clear()
	d.dev.PowerOff()
	d.port.Close()
}

func (d *SpotpearGameDisplay) PowerOn() {
	d.dev.PowerOn()
}

func (d *SpotpearGameDisplay) PowerOff() {
	d.dev.PowerOff()
}

func (d *SpotpearGameDisplay) Show(sprite string) {
	img := d.sprites.GetSprite(sprite)
	d.dev.DrawRAW(img)
}

func (d *SpotpearGameDisplay) ShowText(text string) {
	d.Clear()
	opts := textview.DefaultOpts
	opts.FGColor = textview.GREEN
	opts.FontSize = 64
	tv := textview.NewWithOptions(opts)
	tv.DrawChars(text)
}

func (d *SpotpearGameDisplay) ShowTextCentered(canvas *image.RGBA, text string, size int) {
	fontFace := truetype.NewFace(d.font, &truetype.Options{
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

	d.dev.DrawRAW(canvas)
}
