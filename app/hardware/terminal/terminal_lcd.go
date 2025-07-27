package terminal

import "image"

type NullDisplay struct {
	Orientation int
}

func NewNullDeviceDisplay() *NullDisplay {
	return &NullDisplay{}
}

func (d *NullDisplay) Clear() {}

func (d *NullDisplay) Close() {}

func (d *NullDisplay) PowerOn() {}

func (d *NullDisplay) PowerOff() {}

func (d *NullDisplay) PowerToggle() bool {
	return false
}

func (d *NullDisplay) IsPoweredOn() bool {
	return false
}

func (d *NullDisplay) Show(string) {}

func (d *NullDisplay) ShowText(string) {}

func (d *NullDisplay) ShowTextCentered(*image.RGBA, string, int) {}

func (d *NullDisplay) ShowTextOverlay(string, string, int) {}

func (d *NullDisplay) GetOrientation() int {
	return d.Orientation
}

func (d *NullDisplay) SetOrientation(o int) {
	d.Orientation = o
}
