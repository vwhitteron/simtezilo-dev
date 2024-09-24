package internal

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"sync"
	"time"

	_ "image/png"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/rubiojr/go-pirateaudio/textview"
	"github.com/vwhitteron/gt-pi/internal/st7789"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"periph.io/x/conn/v3/driver/driverreg"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

type Waveshare14972Display struct {
	port spi.PortCloser
	dev  *st7789.Device

	font    *truetype.Font
	sprites *spriteSet

	Orientation int
}

type Waveshare14972DisplayOpts struct {
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

func NewWaveshare14972Display(opts Waveshare14972DisplayOpts) (*Waveshare14972Display, error) {
	var err error
	var spiPort spi.PortCloser
	var spiDev *st7789.Device
	once.Do(func() {
		spiPort, err = spireg.Open("SPI0.0")
		if err != nil {
			return
		}

		dataComm := gpioreg.ByName("GPIO25")

		st7789Opts := &st7789.Opts{
			Width:     240,
			Height:    240,
			Rotation:  st7789.ROTATION_90,
			Reset:     gpioreg.ByName("GPIO27"),
			Backlight: gpioreg.ByName("GPIO24"),
		}

		spiDev, err = st7789.NewSPI(spiPort.(spi.Port), dataComm, st7789Opts)
	})
	if err != nil {
		return nil, fmt.Errorf("initializing display: %w", err)
	}

	err = spiDev.Reset()
	if err != nil {
		return nil, fmt.Errorf("resetting display: %w", err)
	}

	waveshareDisplayInit(spiDev)

	switch opts.Orientation {
	case 90:
		spiDev.SetRotation(st7789.ROTATION_90)
	case 180:
		spiDev.SetRotation(st7789.ROTATION_180)
	case 270:
		spiDev.SetRotation(st7789.ROTATION_270)
	default:
		spiDev.SetRotation(st7789.ROTATION_NONE)
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

	display := &Waveshare14972Display{
		port:        spiPort,
		dev:         spiDev,
		font:        freetypeFont,
		Orientation: opts.Orientation,
		sprites:     sprites,
	}

	display.Clear()

	return display, nil
}

func (d *Waveshare14972Display) Clear() {
	d.dev.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (d *Waveshare14972Display) Close() {
	d.Clear()
	d.dev.PowerOff()
	d.port.Close()
}

func (d *Waveshare14972Display) PowerOn() {
	d.dev.PowerOn()
}

func (d *Waveshare14972Display) PowerOff() {
	d.dev.PowerOff()
}

func (d *Waveshare14972Display) Show(sprite string) {
	img := d.sprites.GetSprite(sprite)
	d.dev.DrawRAW(img)
}

func (d *Waveshare14972Display) ShowText(text string) {
	d.Clear()
	opts := textview.DefaultOpts
	opts.FGColor = textview.GREEN
	opts.FontSize = 64
	tv := textview.NewWithOptions(opts)
	tv.DrawChars(text)
}

func (d *Waveshare14972Display) ShowTextCentered(canvas *image.RGBA, text string, size int) {
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

	d.dev.DrawRAW(canvas)
}

func (d *Waveshare14972Display) GetOrientation() int {
	return d.Orientation
}

func (d *Waveshare14972Display) SetOrientation(rotation int) {
	switch rotation {
	case 90:
		d.dev.SetRotation(st7789.ROTATION_90)
		d.Orientation = rotation
	case 180:
		d.dev.SetRotation(st7789.ROTATION_180)
		d.Orientation = rotation
	case 270:
		d.dev.SetRotation(st7789.ROTATION_270)
		d.Orientation = rotation
	default:
		d.dev.SetRotation(st7789.ROTATION_NONE)
		d.Orientation = 0
	}
}

func waveshareDisplayInit(d *st7789.Device) {
	d.Command(st7789.SWRESET)
	time.Sleep(150 * time.Millisecond)

	//01,0x11
	// d.Command(st7789.SLPOUT)

	// -2,120?

	//-1,0x36,0x70
	d.Command(st7789.MADCTL)
	d.Data(st7789.MADCTL_MX_RL | st7789.MADCTL_MV_REV | st7789.MADCTL_ML_BT)

	// -1,0x3A,0x05
	d.Command(st7789.COLMOD)
	d.Data(st7789.COLMOD_CTRL_65K)

	// -1,0xB2,0x0C,0x0C,0x00,0x33,0x33
	d.Command(st7789.PORCTRL)
	d.SendData([]byte{0x0C, 0x0C, 0x00, 0x33, 0x33})

	// -1,0xB7,0x35
	d.Command(st7789.GCTRL)
	d.Data(0x35)

	// -1,0xBB,0x1A
	// d.Command(st7789.VCOMS)
	// d.Data(0x1A)

	// -1,0xBB,0x19
	d.Command(st7789.VCOMS)
	d.Data(0x19)

	// -1,0xC0,0x2C
	d.Command(st7789.LCMCTRL)
	d.Data(st7789.LCMCTRL_XBGR | st7789.LCMCTRL_XMH | st7789.LMCTRL_XMV)

	// -1,0xC2,0x01
	d.Command(st7789.VDVVRHEN)
	d.Data(st7789.VDVVRHEN_CMDEN_WRITE)

	// -1,0xC3,0x0B
	// d.Command(st7789.VRHS)
	// d.Data(0x0B)

	// -1,0xC3,0x12
	d.Command(st7789.VRHS)
	d.Data(0x12)

	// -1,0xC4,0x20
	d.Command(st7789.VDVS)
	d.Data(0x20)

	// -1,0xC6,0x0F
	d.Command(st7789.FRCTRL2)
	d.Data(st7789.FRAMERATE_60)

	// -1,0xD0,0xA4,0xA1
	d.Command(st7789.PWCTRL1)
	d.SendData([]byte{0xA4, 0xA1})

	// -1,0x21
	// d.Command(st7789.INVON)

	// -1,0xE0,0x00,0x19,0x1E,0x0A,0x09,0x15,0x3D,0x44,0x51,0x12,0x03,0x00,0x3F,0x3F
	// d.Command(st7789.PVGAMCTRL)
	// d.SendData([]byte{0x00, 0x19, 0x1E, 0x0A, 0x09, 0x15, 0x3D, 0x44, 0x51, 0x12, 0x03, 0x00, 0x3F, 0x3F})

	// -1,0xE0,0xD0,0x04,0x0D,0x11,0x13,0x2B,0x3F,0x54,0x4C,0x18,0x0D,0x0B,0x1F,0x23
	d.Command(st7789.PVGAMCTRL)
	d.SendData([]byte{0xD0, 0x04, 0x0D, 0x11, 0x13, 0x2B, 0x3F, 0x54, 0x4C, 0x18, 0x0D, 0x0B, 0x1F, 0x23})

	// -1,0xE1,0x00,0x18,0x1E,0x0A,0x09,0x25,0x3F,0x43,0x52,0x33,0x03,0x00,0x3F,0x3F
	// d.Command(st7789.NVGAMCTRL)
	// d.SendData([]byte{0x00, 0x18, 0x1E, 0x0A, 0x09, 0x25, 0x3F, 0x43, 0x52, 0x33, 0x03, 0x00, 0x3F, 0x3F})

	// -1,0xE1,0xD0,0x04,0x0C,0x11,0x13,0x2C,0x3F,0x44,0x51,0x2F,0x1F,0x1F,0x20,0x23
	d.Command(st7789.NVGAMCTRL)
	d.SendData([]byte{0xD0, 0x04, 0x0C, 0x11, 0x13, 0x2C, 0x3F, 0x44, 0x51, 0x2F, 0x1F, 0x1F, 0x20, 0x23})

	// -1,0x21
	d.Command(st7789.INVON)

	//01,0x11
	d.Command(st7789.SLPOUT)

	// -1,0x29
	d.Command(st7789.DISPON)

	// -3
}
