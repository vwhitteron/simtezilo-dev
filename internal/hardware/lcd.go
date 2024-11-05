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
	ShowTextOverlay(string, string, int)
	GetOrientation() int
	SetOrientation(int)
}
