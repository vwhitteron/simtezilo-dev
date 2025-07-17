package waveshare

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"sync"
	"time"

	_ "image/png"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/rubiojr/go-pirateaudio/textview"
	"github.com/vwhitteron/simtezilo-dev/app/gui"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/lcd/st7789"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"periph.io/x/conn/v3/driver/driverreg"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

const displayDPI float64 = 265

type Waveshare14972LCD struct {
	port spi.PortCloser
	dev  *st7789.Device

	dpi     float64
	font    *truetype.Font
	sprites *gui.SpriteSet

	Orientation int
	poweredOn   bool
	canvas      *image.RGBA
}

type Waveshare14972LCDOpts struct {
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

func NewWaveshare14972Display(opts Waveshare14972LCDOpts) (*Waveshare14972LCD, error) {
	var err error
	var spiPort spi.PortCloser
	var lcdDevice *st7789.Device
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

		lcdDevice, err = st7789.NewSPI(spiPort.(spi.Port), dataComm, st7789Opts)
	})
	if err != nil {
		return nil, fmt.Errorf("initializing display: %w", err)
	}

	err = lcdDevice.Reset()
	if err != nil {
		return nil, fmt.Errorf("resetting display: %w", err)
	}

	waveshareDisplayInit(lcdDevice)

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

	lcd := &Waveshare14972LCD{
		port: spiPort,
		dev:  lcdDevice,

		dpi:     displayDPI,
		font:    freetypeFont,
		sprites: sprites,

		Orientation: opts.Orientation,
		poweredOn:   true,
	}

	lcd.Clear()

	return lcd, nil
}

func (l *Waveshare14972LCD) Clear() {
	l.dev.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (l *Waveshare14972LCD) Close() {
	l.Clear()
	l.dev.PowerOff()
	l.port.Close()
}

func (l *Waveshare14972LCD) PowerOn() {
	l.dev.PowerOn()
	l.poweredOn = true
}

func (l *Waveshare14972LCD) PowerOff() {
	l.dev.PowerOff()
	l.poweredOn = false
}

func (l *Waveshare14972LCD) PowerToggle() bool {
	if l.poweredOn {
		l.PowerOff()
		return false
	}

	l.PowerOn()
	return true
}

func (l *Waveshare14972LCD) IsPoweredOn() bool {
	return l.poweredOn
}

func (l *Waveshare14972LCD) Show(sprite string) {
	img := l.sprites.GetSprite(sprite)

	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, image.Point{0, 0}, draw.Src)

	l.canvas = canvas

	l.dev.DrawRAW(canvas)
}

func (l *Waveshare14972LCD) ShowText(text string) {
	l.Clear()
	opts := textview.DefaultOpts
	opts.FGColor = textview.GREEN
	opts.FontSize = 64
	tv := textview.NewWithOptions(opts)
	tv.DrawChars(text)
}

func (l *Waveshare14972LCD) ShowTextCentered(canvas *image.RGBA, text string, size int) {
	fontFace := truetype.NewFace(l.font, &truetype.Options{
		Size:    float64(size),
		DPI:     l.dpi,
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

	l.canvas = canvas

	l.dev.DrawRAW(canvas)
}

func (l *Waveshare14972LCD) ShowTextOverlay(background string, text string, size int) {
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

	l.canvas = canvas

	l.dev.DrawRAW(canvas)
}

func (l *Waveshare14972LCD) GetOrientation() int {
	return l.Orientation
}

func (l *Waveshare14972LCD) SetOrientation(rotation int) {
	switch rotation {
	case 90:
		l.dev.SetRotation(st7789.ROTATION_90)
		l.Orientation = rotation
	case 180:
		l.dev.SetRotation(st7789.ROTATION_180)
		l.Orientation = rotation
	case 270:
		l.dev.SetRotation(st7789.ROTATION_270)
		l.Orientation = rotation
	default:
		l.dev.SetRotation(st7789.ROTATION_NONE)
		l.Orientation = 0
	}

	l.dev.DrawRAW(l.canvas)
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
