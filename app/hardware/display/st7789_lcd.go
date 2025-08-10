package display

import (
	"fmt"
	"image"
	"image/color"
	"sync"
	"time"

	_ "image/png"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/display/st7789"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/ui/sprites"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
)

type ST7789LCD struct {
	port   spi.PortCloser
	device *st7789.Device

	dpi      float64
	rotation st7789.Rotation
	sleeping bool

	i18n    *i18n.Language
	sprites *sprites.SpriteSet
	canvas  *image.RGBA
}

type Config struct {
	DataCommPin      gpio.PinOut
	ResetPin         gpio.PinIO
	BacklightPin     gpio.PinIO
	SPIPort          string
	SPIFrequency     physic.Frequency
	SPIMode          spi.Mode
	SPIBits          uint8
	PixelRows        uint16
	PixelColumns     uint16
	DPI              float64
	Rotation         st7789.Rotation
	SetupDisplayFunc func(*st7789.Device)
	I18n             *i18n.Language // TODO: move rendering outside of display package
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

	sprites, err := sprites.NewSpriteSet()
	if err != nil {
		return nil, fmt.Errorf("loading sprite set: %w", err)
	}

	if config.I18n == nil {
		return nil, fmt.Errorf("no font provided for display")
	}

	lcd := &ST7789LCD{
		port:   spiPortCloser,
		device: lcdDevice,

		dpi:     config.DPI,
		i18n:    config.I18n,
		sprites: sprites,

		rotation: config.Rotation,
		sleeping: true,
	}

	lcd.Clear()

	return lcd, nil
}
func (l *ST7789LCD) Clear() {
	l.device.FillScreen(color.RGBA{R: 255, G: 0, B: 0, A: 128})
}

func (l *ST7789LCD) Close() {
	l.Clear()
	time.Sleep(1 * time.Second)
	_ = l.device.PowerOff()
	_ = l.port.Close()
}

func (l *ST7789LCD) Wakeup() {
	_ = l.device.PowerOn()
	l.sleeping = false
}

func (l *ST7789LCD) Sleep() {
	if l.sleeping {
		return
	}

	_ = l.device.PowerOff()
	l.sleeping = true
}

func (l *ST7789LCD) ToggleSleep() bool {
	if l.sleeping {
		l.Sleep()

		return true
	}

	l.Wakeup()

	return false
}

func (l *ST7789LCD) IsSleeping() bool {
	return l.sleeping
}

func (l *ST7789LCD) IsAwake() bool {
	return !l.sleeping
}

func (l *ST7789LCD) GetResolution() (uint16, uint16) {
	return l.device.Size()
}

func (l *ST7789LCD) GetDPI() float64 {
	return l.dpi
}

func (l *ST7789LCD) Write(canvas *image.RGBA) error {
	if canvas == nil {
		return fmt.Errorf("canvas is nil")
	}

	l.device.DrawRAW(canvas)
	l.canvas = canvas

	return nil
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

func (l *ST7789LCD) SetOrientation(degrees int) {
	var rotation st7789.Rotation

	switch degrees {
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
		l.SetOrientation(90)
		return 90
	}
}

func (l *ST7789LCD) RotateCCW() int {
	switch l.rotation {
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
		l.SetOrientation(270)
		return 270
	}
}

// setupDisplay initializes the display with the necessary commands and settings.
func setupDisplayDefault(d *st7789.Device) {
	d.Command(st7789.SWRESET)
	time.Sleep(150 * time.Millisecond)

	// Porch Setting: Normal(Back Front), PSEN = disabled, Idle(Back, Front)
	d.Command(st7789.PORCTRL)
	_ = d.SendData(st7789.DefaultPORCTRL())

	// Interface pixel format: 16bit/pixel non-RGB
	d.Command(st7789.COLMOD)
	d.Data(st7789.COLMOD_CTRL_65K)

	// Gate Control: High = 12.54v, Low = -9.6v
	d.Command(st7789.GCTRL)
	_ = d.SendData(st7789.DefaultGCTRL())

	// VCOM Setting: 0.575v
	d.Command(st7789.VCOMS)
	_ = d.SendData(st7789.DefaultVCOMS())

	// LCM Control: XOR RGB/BGR order, XOR Display Latch Order, XOR Page/Column order
	d.Command(st7789.LCMCTRL)
	_ = d.SendData(st7789.DefaultLCMCTRL())

	// VDVVRHEN: CMDEN = VDV and VRH register value comes from command write.
	d.Command(st7789.VDVVRHEN)
	_ = d.SendData(st7789.DefaultVDVVRHEN())

	// VAP(GVDD) (V) = 4.45+(vcom+vcom offset+vdv)
	d.Command(st7789.VRHS)
	_ = d.SendData(st7789.DefaultVRHS())

	// VDV Set: 0v
	d.Command(st7789.VDVS)
	_ = d.SendData(st7789.DefaultVDVS())

	// Power Control 1: AVDD = 6.8v, AVCL = -4.6v, VDS = 2.3v
	d.Command(st7789.PWCTRL1)
	_ = d.SendData(st7789.DefaultPWCTRL1())

	// Frame Rate Control (normal mode): 60Hz
	d.Command(st7789.FRCTRL2)
	_ = d.SendData(st7789.DefaultFRCTRL2())

	// Positive Voltage Gamma Control
	d.Command(st7789.PVGAMCTRL)
	_ = d.SendData(st7789.DefaultPVGAMCTRL())

	// Negative Voltage Gamma Control
	d.Command(st7789.NVGAMCTRL)
	_ = d.SendData(st7789.DefaultNVGAMCTRL())

	// Display Inversion: on
	d.Command(st7789.INVON)

	// Display On Recovery: off
	d.Command(st7789.DISPOFF)

	// Sleep Mode: off
	d.Command(st7789.SLPOUT)
}
