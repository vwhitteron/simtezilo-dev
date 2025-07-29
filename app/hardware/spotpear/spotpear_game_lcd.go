package spotpear

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"sync"
	"time"

	_ "image/png"

	"github.com/golang/freetype/truetype"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/lcd/st7789"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"periph.io/x/conn/v3/driver/driverreg"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

const dataCommPin = "GPIO25"
const resetPin = "GPIO27"
const backlightPin = ""
const lcdPixelRows int16 = 240
const lcdPixelColumns int16 = 240
const lcdDPI float64 = 265
const rotation = st7789.ROTATION_90
const spiPort = "SPI0.0"
const spiFrequency = 40 * physic.MegaHertz
const spiMode = spi.Mode0
const spiBits = 8

type SpotpearGameLCD struct {
	port   spi.PortCloser
	device *st7789.Device

	dpi     float64
	font    *truetype.Font
	sprites *ui.SpriteSet

	orientation int
	poweredOn   bool
	canvas      *image.RGBA
}

type LCDOpts struct {
	Orientation int
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

func NewDisplay(opts LCDOpts) (*SpotpearGameLCD, error) {
	var err error
	var spiPortCloser spi.PortCloser
	var lcdDevice *st7789.Device
	once.Do(func() {
		spiPortCloser, err = spireg.Open(spiPort)
		if err != nil {
			return
		}

		st7789Config := &st7789.SPIDeviceConfig{
			PixelColumns: lcdPixelColumns,
			PixelRows:    lcdPixelRows,
			Rotation:     rotation,
			DataCommPin:  gpioreg.ByName(dataCommPin),
			ResetPin:     gpioreg.ByName(resetPin),
			BacklightPin: gpioreg.ByName(backlightPin),
			SPIPort:      spiPortCloser.(spi.Port),
			SPIMode:      spiMode,
			SPIFrequency: spiFrequency,
			SPIBits:      spiBits,
		}

		lcdDevice, err = st7789.NewSPI(st7789Config)
	})
	if err != nil {
		return nil, fmt.Errorf("initializing lcd device: %w", err)
	}

	err = lcdDevice.Reset()
	if err != nil {
		return nil, fmt.Errorf("resetting display: %w", err)
	}

	setupDisplay(lcdDevice)

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

	sprites, err := ui.NewSpriteSet()
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	freetypeFont, err := ui.GetRegularFont()
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	lcd := &SpotpearGameLCD{
		port:   spiPortCloser,
		device: lcdDevice,

		dpi:     lcdDPI,
		font:    freetypeFont,
		sprites: sprites,

		orientation: opts.Orientation,
		poweredOn:   true,
	}

	lcd.Clear()

	return lcd, nil
}
func (l *SpotpearGameLCD) Clear() {
	l.device.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (l *SpotpearGameLCD) Close() {
	l.Clear()
	time.Sleep(1 * time.Second)
	l.device.PowerOff()
	l.port.Close()
}

func (l *SpotpearGameLCD) PowerOn() {
	l.device.PowerOn()
}

func (l *SpotpearGameLCD) PowerOff() {
	l.device.PowerOff()
}

func (l *SpotpearGameLCD) PowerToggle() bool {
	if l.poweredOn {
		l.PowerOff()

		return false
	}

	l.PowerOn()

	return true
}

func (l *SpotpearGameLCD) IsPoweredOn() bool {
	return l.poweredOn
}

func (l *SpotpearGameLCD) Show(sprite string) {
	img := l.sprites.GetSprite(sprite)

	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, image.Point{0, 0}, draw.Src)

	l.canvas = canvas

	l.device.DrawRAW(canvas)
}

func (l *SpotpearGameLCD) ShowTextCentered(canvas *image.RGBA, text string, size int) {
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

	l.device.DrawRAW(canvas)
}

func (l *SpotpearGameLCD) ShowTextOverlay(background string, text string, size int) {
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
		Src:  image.NewUniform(color.RGBA{128, 128, 128, 1}),
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

	l.device.DrawRAW(canvas)
}

func (l *SpotpearGameLCD) GetOrientation() int {
	return l.orientation
}

func (l *SpotpearGameLCD) SetOrientation(rotation int) {
	switch rotation {
	case 90:
		l.device.SetRotation(st7789.ROTATION_90)
		l.orientation = rotation
	case 180:
		l.device.SetRotation(st7789.ROTATION_180)
		l.orientation = rotation
	case 270:
		l.device.SetRotation(st7789.ROTATION_270)
		l.orientation = rotation
	default:
		l.device.SetRotation(st7789.ROTATION_NONE)
		l.orientation = 0
	}

	l.device.DrawRAW(l.canvas)
}

// setupDisplay initializes the display with the necessary commands and settings.
//
// This function is based on the Waveshare 1.3 inch LCD HAT code and Python ST7789 driver.
// https://files.waveshare.com/upload/b/bd/1.3inch_LCD_HAT_code.7z
// lib/LCD/LCD_1in3.c and python/ST7789.py
func setupDisplay(d *st7789.Device) {
	d.Command(st7789.SWRESET)
	time.Sleep(150 * time.Millisecond)

	// Memory Data Access Control: X-Y Exchange, X-Mirror, Y-Mirror
	d.Command(st7789.MADCTL)
	d.Data(st7789.MADCTL_MX_RL | st7789.MADCTL_MV_REV | st7789.MADCTL_ML_BT)

	// Sleep Mode: off
	d.Command(st7789.SLPOUT)

	time.Sleep(120 * time.Millisecond)

	// Memory Access Control: All defaults
	d.Command(st7789.MADCTL)
	d.Data(0x00)

	// Interface pixel format: 16bit/pixel non-RGB
	d.Command(st7789.COLMOD)
	d.Data(st7789.COLMOD_CTRL_65K)

	// Porch Setting: Normal(Back Front), PSEN = disabled, Idle(Back, Front)
	d.Command(st7789.PORCTRL)
	d.SendData(st7789.DefaultPORCTRL())

	// Gate Control: High = 12.2v, Low = -7.16v
	d.Command(st7789.GCTRL)
	d.Data(0x00)

	// VCOM Setting = 1.675v
	d.Command(st7789.VCOMS)
	d.Data(0x3F)

	// LCM Control: XOR RGB/BGR order, XOR Display Latch Order, XOR Page/Column order
	d.Command(st7789.LCMCTRL)
	d.Data(st7789.LCMCTRL_XBGR | st7789.LCMCTRL_XMH | st7789.LCMCTRL_XMV)

	// VDVVRHEN: CMDEN = VDV and VRH register value comes from command write.
	d.Command(st7789.VDVVRHEN)
	d.SendData(st7789.DefaultVDVVRHEN())

	// VHR Set: VAP(GVDD) =  4.2v + (vcom+vcom offset+vdv)
	//          VAN(GVCL) = -4.2v + (vcom+vcom offset+vdv)
	d.Command(st7789.VRHS)
	d.Data(0x0D)

	// VDV Set: 0v
	// d.Command(st7789.VDVS)
	// d.SendData(st7789.DefaultVDVS())

	// Frame Rate Control (normal mode): 60Hz
	d.Command(st7789.FRCTRL2)
	d.SendData(st7789.DefaultFRCTRL2())

	// Power Control: strange behavior in Waveshare drivers
	d.Command(st7789.PWCTRL1)
	d.SendData([]byte{0xA7})

	// Power Control 1: AVDD = 6.8v, AVCL = -4.6v, VDS = 2.3v
	d.Command(st7789.PWCTRL1)
	d.SendData(st7789.DefaultPWCTRL1())

	// Undocumented command: strange behaviour in Waveshare drivers
	d.Command(0xD6)
	d.SendData([]byte{0xA1})

	// Positive Voltage Gamma Control
	d.Command(st7789.PVGAMCTRL)
	d.SendData([]byte{0xF0, 0x00, 0x02, 0x01, 0x00, 0x00, 0x27, 0x43, 0x3F, 0x33, 0x0E, 0x0E, 0x26, 0x2E})

	// Negative Voltage Gamma Control
	d.Command(st7789.NVGAMCTRL)
	d.SendData([]byte{0xF0, 0x07, 0x0D, 0x0D, 0x0B, 0x16, 0x26, 0x43, 0x3E, 0x3F, 0x19, 0x19, 0x31, 0x3A})

	// Display Inversion: on
	d.Command(st7789.INVON)

	// Display On Recovery: on
	d.Command(st7789.DISPON)
}
