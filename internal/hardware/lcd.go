package hardware

import (
	"image"
)

type LCD interface {
	Clear()
	Close()
	PowerOn()
	PowerOff()
	Show(string)
	ShowText(string)
	ShowTextCentered(*image.RGBA, string, int)
	GetOrientation() int
	SetOrientation(int)
}
