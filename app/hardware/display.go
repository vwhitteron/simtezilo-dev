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
	Show(string)
	ShowTextCentered(*image.RGBA, string, float64)
	ShowTextOverlay(string, string, float64)
	ShowTextSetting(*image.RGBA, string, float64, string, float64)
	GetOrientation() int
	SetOrientation(int)
	RotateCW() int
	RotateCCW() int
}
