package hardware

import (
	"image"
)

type LCD interface {
	Clear()
	Close()
	DrawImage(image.Image)
	PowerOn()
	PowerOff()
	Show(string)
	ShowText(string)
	ShowTextCentered(*image.RGBA, string, int)
	GetOrientation() int
	SetOrientation(int)
}
