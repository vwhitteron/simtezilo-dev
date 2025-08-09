package terminal

import "image"

type NullDisplay struct {
	Orientation int
	sleeping    bool
}

func NewHeadlessDisplay() *NullDisplay {
	return &NullDisplay{
		Orientation: 0,
		sleeping:    false,
	}
}

func (d *NullDisplay) Clear() {}

func (d *NullDisplay) Close() {}

func (d *NullDisplay) Wakeup() {
	d.sleeping = false
}

func (d *NullDisplay) Sleep() {
	d.sleeping = true
}

func (d *NullDisplay) ToggleSleep() bool {
	d.sleeping = !d.sleeping

	return d.sleeping
}

func (d *NullDisplay) IsSleeping() bool {
	return d.sleeping
}

func (d *NullDisplay) IsAwake() bool {
	return !d.sleeping
}

func (d *NullDisplay) GetResolution() (uint16, uint16) {
	return 0, 0
}

func (d *NullDisplay) GetDPI() float64 {
	return 0
}

func (d *NullDisplay) Write(*image.RGBA) error {
	return nil
}

func (d *NullDisplay) Show(string) {}

func (d *NullDisplay) ShowTextCentered(*image.RGBA, string, float64) {}

func (d *NullDisplay) ShowTextOverlay(string, string, float64) {}

func (d *NullDisplay) ShowTextSetting(*image.RGBA, string, float64, string, float64) {}

func (d *NullDisplay) GetOrientation() int {
	return d.Orientation
}

func (d *NullDisplay) SetOrientation(o int) {
	d.Orientation = o
}

func (d *NullDisplay) RotateCW() int {
	d.Orientation += 90
	if d.Orientation >= 360 {
		d.Orientation = 0
	}
	return d.Orientation
}

func (d *NullDisplay) RotateCCW() int {
	d.Orientation -= 90
	if d.Orientation < 0 {
		d.Orientation = 270
	}
	return d.Orientation
}
