package waveshare

import (
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

// SetupHID configures the HID input event mapping of the Waveshare device buttons based on the device orientation.
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
	for i := range 4 {
		rotatedDpadMapping[i] = baseDpadMapping[(i-rotationOffset+4)%4]
	}

	baseAuxMapping := []ui.HIDInputEvent{
		ui.HIDInputEscape, // Button 1
		ui.HIDInputNone,   // Button 2
		ui.HIDInputPower,  // Button 3
	}

	// Reverse auxiliary button order at 90 and 180 degree orientation
	rotatedAuxMapping := make([]ui.HIDInputEvent, 3)

	if rotationOffset == 1 || rotationOffset == 3 {
		for i := range 3 {
			rotatedAuxMapping[i] = baseAuxMapping[3-i]
		}
	}

	// OnButtonUpPressed registers a callback function to be called when the up button is pressed.
	OnButtonUpPressed(func() {
		hidEvent <- rotatedDpadMapping[0]
	})

	// OnButtonRightPressed registers a callback function to be called when the right button is pressed.
	OnButtonRightPressed(func() {
		hidEvent <- rotatedDpadMapping[1]
	})

	// OnButtonDownPressed registers a callback function to be called when the down button is pressed.
	OnButtonDownPressed(func() {
		hidEvent <- rotatedDpadMapping[2]
	})

	// OnButtonLeftPressed registers a callback function to be called when the left button is pressed.
	OnButtonLeftPressed(func() {
		hidEvent <- rotatedDpadMapping[3]
	})

	// OnButtonCenterPressed registers a callback function to be called when the center button is pressed.
	OnButtonCenterPressed(func() {
		hidEvent <- ui.HIDInputEnter
	})

	// OnButtonOnePressed registers a callback function to be called when button 1 is pressed.
	OnButtonOnePressed(func() {
		hidEvent <- rotatedAuxMapping[0]
	})

	// OnButtonTwoPressed registers a callback function to be called when button 2 is pressed.
	OnButtonTwoPressed(func() {
		hidEvent <- rotatedAuxMapping[1]
	})

	// OnButtonThreePressed registers a callback function to be called when button 3 is pressed.
	OnButtonThreePressed(func() {
		hidEvent <- rotatedAuxMapping[2]
	})
}

func OnButtonUpPressed(callback func()) {
	hardware.OnGPIOButtonPressed(6, callback)
}

func OnButtonDownPressed(callback func()) {
	hardware.OnGPIOButtonPressed(19, callback)
}

func OnButtonLeftPressed(callback func()) {
	hardware.OnGPIOButtonPressed(5, callback)
}

func OnButtonRightPressed(callback func()) {
	hardware.OnGPIOButtonPressed(26, callback)
}

func OnButtonCenterPressed(callback func()) {
	hardware.OnGPIOButtonPressed(13, callback)
}

func OnButtonOnePressed(callback func()) {
	hardware.OnGPIOButtonPressed(21, callback)
}

func OnButtonTwoPressed(callback func()) {
	hardware.OnGPIOButtonPressed(20, callback)
}

func OnButtonThreePressed(callback func()) {
	hardware.OnGPIOButtonPressed(16, callback)
}
