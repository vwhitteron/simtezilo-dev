package console

import (
	"fmt"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
)

type ConsoleDisplay struct {
	Orientation int
	sleeping    bool
}

func NewDisplay() *ConsoleDisplay {
	return &ConsoleDisplay{
		Orientation: 0,
		sleeping:    false,
	}
}

func (d *ConsoleDisplay) Clear() {}

func (d *ConsoleDisplay) Close() {}

func (d *ConsoleDisplay) Wakeup() {
	d.sleeping = false
}

func (d *ConsoleDisplay) Sleep() {
	d.sleeping = true
}

func (d *ConsoleDisplay) ToggleSleep() bool {
	d.sleeping = !d.sleeping

	return d.sleeping
}

func (d *ConsoleDisplay) IsSleeping() bool {
	return d.sleeping
}

func (d *ConsoleDisplay) IsAwake() bool {
	return !d.sleeping
}

func (d *ConsoleDisplay) GetResolution() (uint16, uint16) {
	return 0, 0
}

func (d *ConsoleDisplay) GetDPI() float64 {
	return 0
}

func (d *ConsoleDisplay) Write(content *display.Content) error {
	fmt.Println(content.Text)

	return nil
}

func (d *ConsoleDisplay) GetOrientation() int {
	return d.Orientation
}

func (d *ConsoleDisplay) SetOrientation(o int) {
	d.Orientation = o
}

func (d *ConsoleDisplay) RotateCW() int {
	d.Orientation += 90
	if d.Orientation >= 360 {
		d.Orientation = 0
	}
	return d.Orientation
}

func (d *ConsoleDisplay) RotateCCW() int {
	d.Orientation -= 90
	if d.Orientation < 0 {
		d.Orientation = 270
	}
	return d.Orientation
}
