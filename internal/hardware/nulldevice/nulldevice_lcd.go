package nulldevice

import "image"

type NullDisplay struct {
	Orientation int
}

func NewNullDeviceDisplay() *NullDisplay {
	return &NullDisplay{}
}

func (d NullDisplay) Clear() {}

func (d NullDisplay) Close() {}

func (d NullDisplay) DrawImage(image.Image) {}

func (d NullDisplay) PowerOn() {}

func (d NullDisplay) PowerOff() {}

func (d NullDisplay) Show(string) {}

func (d NullDisplay) ShowText(string) {}

func (d NullDisplay) ShowTextCentered(*image.RGBA, string, int) {}

func (d NullDisplay) ShowTextOverlay(string, string, int) {}

func (d NullDisplay) GetOrientation() int {
	return d.Orientation
}

func (d NullDisplay) SetOrientation(o int) {
	switch o {
	case 90:
		d.Orientation = 90
	case 180:
		d.Orientation = 180
	case 270:
		d.Orientation = 270
	default:
		d.Orientation = 0
	}

}
