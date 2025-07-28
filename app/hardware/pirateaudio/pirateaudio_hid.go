package pirateaudio

import (
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/ui"
)

func SetupPirateAudioHID(hidEvent chan ui.HIDInputEvent) {
	OnButtonAPressed(func() {
		hidEvent <- ui.HIDInputUp
	})

	OnButtonBPressed(func() {
		hidEvent <- ui.HIDInputDown
	})

	OnButtonXPressed(func() {
		hidEvent <- ui.HIDInputRight
	})

	OnButtonYPressed(func() {
		hidEvent <- ui.HIDInputPower
	})
}

func init() {
	hardware.Init()
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
