package internal

import (
	"image"
)

type Display interface {
	Clear()
	Close()
	PowerOn()
	PowerOff()
	Show(string)
	ShowText(string)
	ShowTextCentered(*image.RGBA, string)
}

type NullDisplay struct{}

func NewNullDisplay() *NullDisplay {
	return &NullDisplay{}
}

func (d NullDisplay) Clear() {}

func (d NullDisplay) Close() {}

func (d NullDisplay) PowerOn() {}

func (d NullDisplay) PowerOff() {}

func (d NullDisplay) Show(string) {}

func (d NullDisplay) ShowText(string) {}

func (d NullDisplay) ShowTextCentered(*image.RGBA, string) {}
