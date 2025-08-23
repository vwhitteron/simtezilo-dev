package waveshare

import (
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

func SetupHID(orientation int, hidEvent chan ui.HIDInputEvent) {
	rotationOffset := (orientation / 90) % 4

	baseDpadMapping := []ui.HIDInputEvent{
		ui.HIDInputUp,    // Dpad Up
		ui.HIDInputRight, // Dpad Right
		ui.HIDInputLeft,  // Dpad Down
		ui.HIDInputDown,  // Dpad Left
	}

	// Rotate Dpad mapping based on orientation
	rotatedDpadMapping := make([]ui.HIDInputEvent, 4)
	for i := 0; i < 4; i++ {
		rotatedDpadMapping[i] = baseDpadMapping[(i-rotationOffset+4)%4]
	}

	baseAuxMapping := []ui.HIDInputEvent{
		ui.HIDInputEscape, // Button 1
		ui.HIDInputNone,   // Button 2
		ui.HIDInputPower,  // Button 3
	}

	// Reverse auxilliary button order at 90 and 180 degree orientation
	rotatedAuxMapping := make([]ui.HIDInputEvent, 3)
	if rotationOffset == 1 || rotationOffset == 3 {
		for i := 0; i < 3; i++ {
			rotatedAuxMapping[i] = baseAuxMapping[3-i]
		}
	}

	OnButtonUpPressed(func() {
		hidEvent <- rotatedDpadMapping[0]
	})

	OnButtonRightPressed(func() {
		hidEvent <- rotatedDpadMapping[1]
	})

	OnButtonDownPressed(func() {
		hidEvent <- rotatedDpadMapping[2]
	})

	OnButtonLeftPressed(func() {
		hidEvent <- rotatedDpadMapping[3]
	})

	OnButtonCenterPressed(func() {
		hidEvent <- ui.HIDInputEnter
	})

	OnButtonOnePressed(func() {
		hidEvent <- rotatedAuxMapping[0]
	})

	OnButtonTwoPressed(func() {
		hidEvent <- rotatedAuxMapping[1]
	})

	OnButtonThreePressed(func() {
		hidEvent <- rotatedAuxMapping[2]
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
