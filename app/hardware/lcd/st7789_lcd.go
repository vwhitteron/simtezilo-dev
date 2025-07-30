package lcd

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
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
)

type ST7789LCD struct {
	port   spi.PortCloser
	device *st7789.Device

	dpi     float64
	font    *truetype.Font
	sprites *ui.SpriteSet

	rotation  st7789.Rotation
	poweredOn bool
	canvas    *image.RGBA
}

type Config struct {
	DataCommPin      gpio.PinOut
	ResetPin         gpio.PinIO
	BacklightPin     gpio.PinIO
	SPIPort          string
	SPIFrequency     physic.Frequency
	SPIMode          spi.Mode
	SPIBits          int
	PixelRows        int16
	PixelColumns     int16
	DPI              float64
	Rotation         st7789.Rotation
	SetupDisplayFunc func(*st7789.Device)
}

var once sync.Once

func NewDisplay(config *Config) (*ST7789LCD, error) {
	if config == nil {
		return nil, fmt.Errorf("no configuration provided")
	}

	var err error
	var spiPortCloser spi.PortCloser
	var lcdDevice *st7789.Device
	once.Do(func() {
		spiPortCloser, err = spireg.Open(config.SPIPort)
		if err != nil {
			return
		}

		st7789Config := &st7789.SPIDeviceConfig{
			DataCommPin:  config.DataCommPin,
			ResetPin:     config.ResetPin,
			BacklightPin: config.BacklightPin,
			SPIPort:      spiPortCloser.(spi.Port),
			SPIMode:      config.SPIMode,
			SPIFrequency: config.SPIFrequency,
			SPIBits:      config.SPIBits,
			PixelColumns: config.PixelColumns,
			PixelRows:    config.PixelRows,
			Rotation:     config.Rotation,
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

	// initialise the display configuration
	if config.SetupDisplayFunc != nil {
		config.SetupDisplayFunc(lcdDevice)
	} else {
		setupDisplayDefault(lcdDevice)
	}

	// rotation must be set after the display is configured
	lcdDevice.SetRotation(config.Rotation)

	sprites, err := ui.NewSpriteSet()
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	freetypeFont, err := ui.GetRegularFont()
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	lcd := &ST7789LCD{
		port:   spiPortCloser,
		device: lcdDevice,

		dpi:     config.DPI,
		font:    freetypeFont,
		sprites: sprites,

		rotation:  config.Rotation,
		poweredOn: true,
	}

	lcd.Clear()

	return lcd, nil
}
func (l *ST7789LCD) Clear() {
	l.device.FillScreen(color.RGBA{R: 0, G: 0, B: 0, A: 0})
}

func (l *ST7789LCD) Close() {
	l.Clear()
	time.Sleep(1 * time.Second)
	l.device.PowerOff()
	l.port.Close()
}

func (l *ST7789LCD) PowerOn() {
	l.device.PowerOn()
}

func (l *ST7789LCD) PowerOff() {
	l.device.PowerOff()
}

func (l *ST7789LCD) PowerToggle() bool {
	if l.poweredOn {
		l.PowerOff()

		return false
	}

	l.PowerOn()

	return true
}

func (l *ST7789LCD) IsPoweredOn() bool {
	return l.poweredOn
}

func (l *ST7789LCD) Show(sprite string) {
	img := l.sprites.GetSprite(sprite)

	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, image.Point{0, 0}, draw.Src)

	l.canvas = canvas

	l.device.DrawRAW(canvas)
}

func (l *ST7789LCD) ShowTextCentered(canvas *image.RGBA, text string, size int) {
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

func (l *ST7789LCD) ShowTextOverlay(background string, text string, size int) {
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

func (l *ST7789LCD) GetOrientation() int {
	switch l.rotation {
	case st7789.ROTATION_90:
		return 90
	case st7789.ROTATION_180:
		return 180
	case st7789.ROTATION_270:
		return 270
	default:
		return 0
	}
}

func (l *ST7789LCD) SetOrientation(orientation int) {
	var rotation st7789.Rotation

	switch orientation {
	case 90:
		rotation = st7789.ROTATION_90
	case 180:
		rotation = st7789.ROTATION_180
	case 270:
		rotation = st7789.ROTATION_270
	default:
		rotation = st7789.ROTATION_NONE
	}

	l.device.SetRotation(rotation)
	l.rotation = rotation

	l.device.DrawRAW(l.canvas)
}

func (l *ST7789LCD) RotateCW() int {
	switch l.rotation {
	case st7789.ROTATION_NONE:
		l.SetOrientation(90)
		return 90
	case st7789.ROTATION_90:
		l.SetOrientation(180)
		return 180
	case st7789.ROTATION_180:
		l.SetOrientation(270)
		return 270
	case st7789.ROTATION_270:
		l.SetOrientation(0)
		return 0
	default:
		return 0
	}
}

func (l *ST7789LCD) RotateCCW() int {
	switch l.rotation {
	case st7789.ROTATION_NONE:
		l.SetOrientation(270)
		return 270
	case st7789.ROTATION_90:
		l.SetOrientation(0)
		return 0
	case st7789.ROTATION_180:
		l.SetOrientation(90)
		return 90
	case st7789.ROTATION_270:
		l.SetOrientation(180)
		return 180
	default:
		return 0
	}
}

// setupDisplay initializes the display with the necessary commands and settings.
func setupDisplayDefault(d *st7789.Device) {
	d.Command(st7789.SWRESET)
	time.Sleep(150 * time.Millisecond)

	// Porch Setting: Normal(Back Front), PSEN = disabled, Idle(Back, Front)
	d.Command(st7789.PORCTRL)
	d.SendData(st7789.DefaultPORCTRL())

	// Interface pixel format: 16bit/pixel non-RGB
	d.Command(st7789.COLMOD)
	d.Data(st7789.COLMOD_CTRL_65K)

	// Gate Control: High = 12.54v, Low = -9.6v
	d.Command(st7789.GCTRL)
	d.SendData(st7789.DefaultGCTRL())

	// VCOM Setting: 0.575v
	d.Command(st7789.VCOMS)
	d.SendData(st7789.DefaultVCOMS())

	// LCM Control: XOR RGB/BGR order, XOR Display Latch Order, XOR Page/Column order
	d.Command(st7789.LCMCTRL)
	d.SendData(st7789.DefaultLCMCTRL())

	// VDVVRHEN: CMDEN = VDV and VRH register value comes from command write.
	d.Command(st7789.VDVVRHEN)
	d.SendData(st7789.DefaultVDVVRHEN())

	// VAP(GVDD) (V) = 4.45+(vcom+vcom offset+vdv)
	d.Command(st7789.VRHS)
	d.SendData(st7789.DefaultVRHS())

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
	d.SendData(st7789.DefaultPVGAMCTRL())

	// Negative Voltage Gamma Control
	d.Command(st7789.NVGAMCTRL)
	d.SendData(st7789.DefaultNVGAMCTRL())

	// Display Inversion: on
	d.Command(st7789.INVON)

	// Display On Recovery: on
	d.Command(st7789.DISPON)

	// Sleep Mode: off
	d.Command(st7789.SLPOUT)
}
