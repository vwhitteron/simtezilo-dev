package pirateaudio

import (
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

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

func OnButtonAPressed(fn func()) {
	hardware.OnGPIOButtonPressed(5, fn)
}

func OnButtonBPressed(fn func()) {
	hardware.OnGPIOButtonPressed(6, fn)
}

func OnButtonXPressed(fn func()) {
	hardware.OnGPIOButtonPressed(16, fn)
}

func OnButtonYPressed(fn func()) {
	hardware.OnGPIOButtonPressed(24, fn)
}
