package pirateaudio

import (
	"sync"
	"time"

	_ "image/png"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display/st7789"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/utils"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
)

const dataCommPin = "GPIO9"
const resetPin = ""
const backlightPin = "GPIO13"
const lcdPixelRows int16 = 240
const lcdPixelColumns int16 = 240
const lcdDPI float64 = 265
const lcdRotation = st7789.ROTATION_NONE
const spiPort = "SPI0.1"
const spiFrequency = 80 * physic.MegaHertz
const spiMode = spi.Mode0
const spiBits = 8

type DisplayOptions struct {
	Orientation int
	I18n        *i18n.Language
}

var once sync.Once

func NewDisplay(opts DisplayOptions) (*display.ST7789LCD, error) {
	angle := st7789.RotationToDegrees(lcdRotation)
	angle = utils.SumAngle90(angle, opts.Orientation)

	return display.NewDisplay(&display.Config{
		DataCommPin:      gpioreg.ByName(dataCommPin),
		ResetPin:         gpioreg.ByName(resetPin),
		BacklightPin:     gpioreg.ByName(backlightPin),
		SPIPort:          spiPort,
		SPIFrequency:     spiFrequency,
		SPIMode:          spiMode,
		SPIBits:          spiBits,
		PixelRows:        lcdPixelRows,
		PixelColumns:     lcdPixelColumns,
		DPI:              lcdDPI,
		Rotation:         st7789.DegreesToRotation(angle),
		SetupDisplayFunc: setupDisplayFunc(),
		I18n:             opts.I18n,
	})
}

func setupDisplayFunc() func(*st7789.Device) {
	return func(d *st7789.Device) {
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
}
