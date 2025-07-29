package hardware

import (
	"image"
)

type LCD interface {
	Clear()
	Close()
	PowerOn()
	PowerOff()
	PowerToggle() bool
	IsPoweredOn() bool
	Show(string)
	ShowTextCentered(*image.RGBA, string, int)
	ShowTextOverlay(string, string, int)
	GetOrientation() int
	SetOrientation(int)
}
