package pirateaudio

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"time"

	_ "image/png"

	"github.com/golang/freetype/truetype"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/lcd/st7789"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
)

const dataCommPin = "GPIO9"
const resetPin = ""
const backlightPin = "GPIO13"
const lcdPixelRows int16 = 240
const lcdPixelColumns int16 = 240
const lcdDPI float64 = 265
const rotation = st7789.ROTATION_NONE
const spiPort = "SPI0.1"
const spiFrequency = 80 * physic.MegaHertz
const spiMode = spi.Mode0
const spiBits = 8

type PirateAudioLCD struct {
	port   spi.PortCloser
	device *st7789.Device

	dpi     float64
	font    *truetype.Font
	sprites *ui.SpriteSet

	orientation int
	poweredOn   bool
	canvas      *image.RGBA
}

type PirateAudioLCDOpts struct {
	Orientation int
}

var once sync.Once

func NewDisplay(opts PirateAudioLCDOpts) (*PirateAudioLCD, error) {
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

	lcd := &PirateAudioLCD{
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

func (l *PirateAudioLCD) Clear() {
	l.device.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (l *PirateAudioLCD) Close() {
	l.Clear()
	time.Sleep(1 * time.Second)
	l.device.PowerOff()
	l.port.Close()
}

func (l *PirateAudioLCD) GetOrientation() int {
	return l.orientation
}

func (l *PirateAudioLCD) SetOrientation(orientation int) {
	l.orientation = orientation
	switch orientation {
	case 90:
		l.device.SetRotation(st7789.ROTATION_90)
	case 180:
		l.device.SetRotation(st7789.ROTATION_180)
	case 270:
		l.device.SetRotation(st7789.ROTATION_270)
	default:
		l.device.SetRotation(st7789.ROTATION_NONE)
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

	l.device.DrawRAW(canvas)
}

func setupDisplay(d *st7789.Device) {
	d.Command(st7789.SWRESET)
	time.Sleep(150 * time.Millisecond)

	// Memory Data Access Control: X-Y Exchange, X-Mirror, Y-Mirror
	d.Command(st7789.MADCTL)
	d.Data(st7789.MADCTL_MX_RL | st7789.MADCTL_MV_REV | st7789.MADCTL_ML_BT)

	// Porch Setting: Normal(Back Front), PSEN = disabled, Idle(Back, Front)
	d.Command(st7789.PORCTRL)
	d.SendData(st7789.DefaultPORCTRL())

	// Interface pixel format: 16bit/pixel non-RGB
	d.Command(st7789.COLMOD)
	d.Data(st7789.COLMOD_CTRL_65K)

	// Gate Control: High = 12.54v, Low = -9.6v
	d.Command(st7789.GCTRL)
	d.Data(0x14)

	// VCOM Setting: 0.575v
	d.Command(st7789.VCOMS)
	d.Data(0x37)

	// LCM Control: XOR RGB/BGR order, XOR Display Latch Order, XOR Page/Column order
	d.Command(st7789.LCMCTRL)
	d.Data(st7789.LCMCTRL_XBGR | st7789.LCMCTRL_XMH | st7789.LCMCTRL_XMV)

	// VDVVRHEN: CMDEN = VDV and VRH register value comes from command write.
	d.Command(st7789.VDVVRHEN)
	d.SendData(st7789.DefaultVDVVRHEN())

	// VAP(GVDD) (V) = 4.45+(vcom+vcom offset+vdv)
	d.Command(st7789.VRHS)
	d.Data(0x12)

	// VDV Set: 0v
	d.Command(st7789.VDVS)
	d.SendData(st7789.DefaultVDVS())

	// Power Control 1: AVDD = 6.8v, AVCL = -4.6v, VDS = 2.3v
	d.Command(st7789.PWCTRL1)
	d.SendData(st7789.DefaultPWCTRL1())

	// Frame Rate Control (normal mode): 60Hz
	d.Command(st7789.FRCTRL2)
	d.SendData(st7789.DefaultFRCTRL2())

	// Positive Voltage Gamma Control
	d.Command(st7789.PVGAMCTRL)
	d.SendData([]byte{0xD0, 0x04, 0x0D, 0x11, 0x13, 0x2B, 0x3F, 0x54, 0x4C, 0x18, 0x0D, 0x0B, 0x1F, 0x23})

	// Negative Voltage Gamma Control
	d.Command(st7789.NVGAMCTRL)
	d.SendData([]byte{0xD0, 0x04, 0x0C, 0x11, 0x13, 0x2C, 0x3F, 0x44, 0x51, 0x2F, 0x1F, 0x1F, 0x20, 0x23})

	// Display Inversion: on
	d.Command(st7789.INVON)

	// Sleep Mode: off
	d.Command(st7789.SLPOUT)

	// Display On Recovery: on
	d.Command(st7789.DISPON)
}
