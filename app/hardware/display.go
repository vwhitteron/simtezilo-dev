package hardware

import (
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
)

type Display interface {
	Clear()
	Close()
	Wakeup()
	Sleep()
	ToggleSleep() bool
	IsAwake() bool
	IsSleeping() bool
	GetResolution() (uint16, uint16)
	GetDPI() float64
	Write(*display.Content) error
	GetOrientation() int
	SetOrientation(int)
	RotateCW() int
	RotateCCW() int
}
