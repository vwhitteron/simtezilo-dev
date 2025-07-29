package spotpear

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

	OnButtonAPressed(func() {
		hidEvent <- ui.HIDInputEnd
	})

	OnButtonBPressed(func() {
		hidEvent <- ui.HIDInputPageDown
	})

	OnButtonXPressed(func() {
		hidEvent <- ui.HIDInputPageUp
	})

	OnButtonYPressed(func() {
		hidEvent <- ui.HIDInputHome
	})

	OnButtonTriggerLeftPressed(func() {
		hidEvent <- ui.HIDInputEscape
	})

	OnButtonTriggerRightPressed(func() {
		hidEvent <- ui.HIDInputNone
	})

	OnButtonStartPressed(func() {
		hidEvent <- ui.HIDInputPower
	})

	OnButtonSelectPressed(func() {
		hidEvent <- ui.HIDInputEnter
	})
}

func OnButtonUpPressed(fn func()) {
	hardware.OnGPIOButtonPressed(5, fn)
}

func OnButtonDownPressed(fn func()) {
	hardware.OnGPIOButtonPressed(6, fn)
}

func OnButtonLeftPressed(fn func()) {
	hardware.OnGPIOButtonPressed(16, fn)
}

func OnButtonRightPressed(fn func()) {
	hardware.OnGPIOButtonPressed(13, fn)
}

func OnButtonAPressed(fn func()) {
	hardware.OnGPIOButtonPressed(21, fn)
}

func OnButtonBPressed(fn func()) {
	hardware.OnGPIOButtonPressed(20, fn)
}

func OnButtonXPressed(fn func()) {
	hardware.OnGPIOButtonPressed(15, fn)
}

func OnButtonYPressed(fn func()) {
	hardware.OnGPIOButtonPressed(12, fn)
}

func OnButtonTriggerLeftPressed(fn func()) {
	hardware.OnGPIOButtonPressed(23, fn)
}

func OnButtonTriggerRightPressed(fn func()) {
	hardware.OnGPIOButtonPressed(14, fn)
}

func OnButtonStartPressed(fn func()) {
	hardware.OnGPIOButtonPressed(26, fn)
}

func OnButtonSelectPressed(fn func()) {
	hardware.OnGPIOButtonPressed(19, fn)
}
