package hardware

import (
	"image"
)

type Display interface {
	Clear()
	Close()
	PowerOn()
	PowerOff()
	PowerToggle() bool
	IsPoweredOn() bool
	GetResolution() (uint16, uint16)
	GetDPI() float64
	Write(*image.RGBA) error
	GetOrientation() int
	SetOrientation(int)
	RotateCW() int
	RotateCCW() int
}
