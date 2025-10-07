package pirateaudio

import (
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

// SetupHID configures the HID input event mapping of the Pirate Audio device buttons based on the device orientation.
func SetupHID(orientation int, hidEvent chan ui.HIDInputEvent) {
	rotationOffset := (orientation / 90) % 4

	baseMapping := []ui.HIDInputEvent{
		ui.HIDInputUp,    // Button A
		ui.HIDInputRight, // Button X
		ui.HIDInputLeft,  // Button Y
		ui.HIDInputDown,  // Button B
	}

	rotatedMapping := make([]ui.HIDInputEvent, 4)
	for i := range 4 {
		rotatedMapping[i] = baseMapping[(i-rotationOffset+4)%4]
	}

	OnButtonAPressed(func() {
		hidEvent <- rotatedMapping[0]
	})

	OnButtonXPressed(func() {
		hidEvent <- rotatedMapping[1]
	})

	OnButtonYPressed(func() {
		hidEvent <- rotatedMapping[2]
	})

	OnButtonBPressed(func() {
		hidEvent <- rotatedMapping[3]
	})
}

// OnButtonAPressed registers a callback function to be called when the A button is pressed.
func OnButtonAPressed(callback func()) {
	hardware.OnGPIOButtonPressed(5, callback)
}

// OnButtonBPressed registers a callback function to be called when the B buttonis pressed.
func OnButtonBPressed(callback func()) {
	hardware.OnGPIOButtonPressed(6, callback)
}

// OnButtonXPressed registers a callback function to be called when the X button is pressed.
func OnButtonXPressed(callback func()) {
	hardware.OnGPIOButtonPressed(16, callback)
}

// OnButtonYPressed registers a callback function to be called when the Y button is pressed.
func OnButtonYPressed(callback func()) {
	hardware.OnGPIOButtonPressed(24, callback)
}
