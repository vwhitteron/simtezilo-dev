package waveshare

import (
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

const dataCommPin = "GPIO25"
const resetPin = "GPIO27"
const backlightPin = "GPIO24"
const lcdPixelRows uint16 = 240
const lcdPixelColumns uint16 = 240
const lcdDPI float64 = 265
const lcdRotation = st7789.ROTATION_90
const spiPort = "SPI0.0"
const spiFrequency = 40 * physic.MegaHertz
const spiMode = spi.Mode0
const spiBits uint8 = 8

type DisplayOptions struct {
	Orientation int
	I18n        *i18n.Language
}

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

// setupDisplay initializes the display with the necessary commands and settings.
//
// This function is based on the Waveshare 1.3 inch LCD HAT code and Python ST7789 driver.
// https://files.waveshare.com/upload/b/bd/1.3inch_LCD_HAT_code.7z
// lib/LCD/LCD_1in3.c and python/ST7789.py
func setupDisplayFunc() func(*st7789.Device) {
	return func(d *st7789.Device) {
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
		_ = d.SendData(st7789.DefaultPORCTRL())

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
		_ = d.SendData(st7789.DefaultVDVVRHEN())

		// VHR Set: VAP(GVDD) =  4.2v + (vcom+vcom offset+vdv)
		//          VAN(GVCL) = -4.2v + (vcom+vcom offset+vdv)
		d.Command(st7789.VRHS)
		d.Data(0x0D)

		// Frame Rate Control (normal mode): 60Hz
		d.Command(st7789.FRCTRL2)
		_ = d.SendData(st7789.DefaultFRCTRL2())

		// Power Control: strange behavior in Waveshare drivers
		d.Command(st7789.PWCTRL1)
		_ = d.SendData([]byte{0xA7})

		// Power Control 1: AVDD = 6.8v, AVCL = -4.6v, VDS = 2.3v
		d.Command(st7789.PWCTRL1)
		_ = d.SendData(st7789.DefaultPWCTRL1())

		// Undocumented command: strange behaviour in Waveshare drivers
		d.Command(0xD6)
		_ = d.SendData([]byte{0xA1})

		// Positive Voltage Gamma Control
		d.Command(st7789.PVGAMCTRL)
		_ = d.SendData([]byte{0xF0, 0x00, 0x02, 0x01, 0x00, 0x00, 0x27, 0x43, 0x3F, 0x33, 0x0E, 0x0E, 0x26, 0x2E})

		// Negative Voltage Gamma Control
		d.Command(st7789.NVGAMCTRL)
		_ = d.SendData([]byte{0xF0, 0x07, 0x0D, 0x0D, 0x0B, 0x16, 0x26, 0x43, 0x3E, 0x3F, 0x19, 0x19, 0x31, 0x3A})

		// Display Inversion: on
		d.Command(st7789.INVON)

		// Display On Recovery: on
		d.Command(st7789.DISPON)
	}
}
