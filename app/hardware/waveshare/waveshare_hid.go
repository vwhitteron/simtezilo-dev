package waveshare

import (
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

func SetupHID(hidEvent chan ui.HIDInputEvent) {
	OnButtonUpPressed(func() {
		hidEvent <- ui.HIDInputUp
	})

	OnButtonDownPressed(func() {
		hidEvent <- ui.HIDInputDown
	})

	OnButtonLeftPressed(func() {
		hidEvent <- ui.HIDInputLeft
	})

	OnButtonRightPressed(func() {
		hidEvent <- ui.HIDInputRight
	})

	OnButtonCenterPressed(func() {
		hidEvent <- ui.HIDInputEnter
	})

	OnButtonOnePressed(func() {
		hidEvent <- ui.HIDInputEscape
	})

	OnButtonTwoPressed(func() {
		hidEvent <- ui.HIDInputNone
	})

	OnButtonThreePressed(func() {
		hidEvent <- ui.HIDInputPower
	})
}

func OnButtonUpPressed(fn func()) {
	hardware.OnGPIOButtonPressed(6, fn)
}

func OnButtonDownPressed(fn func()) {
	hardware.OnGPIOButtonPressed(19, fn)
}

func OnButtonLeftPressed(fn func()) {
	hardware.OnGPIOButtonPressed(5, fn)
}

func OnButtonRightPressed(fn func()) {
	hardware.OnGPIOButtonPressed(26, fn)
}

func OnButtonCenterPressed(fn func()) {
	hardware.OnGPIOButtonPressed(13, fn)
}

func OnButtonOnePressed(fn func()) {
	hardware.OnGPIOButtonPressed(21, fn)
}

func OnButtonTwoPressed(fn func()) {
	hardware.OnGPIOButtonPressed(20, fn)
}

func OnButtonThreePressed(fn func()) {
	hardware.OnGPIOButtonPressed(16, fn)
}
