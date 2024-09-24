package buttons

import (
	"fmt"
	"log"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

func init() {
	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}
}

func OnButtonUpPressed(fn func()) {
	onButtonPressed(6, fn)
}

func OnButtonDownPressed(fn func()) {
	onButtonPressed(19, fn)
}

func OnButtonLeftPressed(fn func()) {
	onButtonPressed(5, fn)
}

func OnButtonRightPressed(fn func()) {
	onButtonPressed(26, fn)
}

func OnButtonCenterPressed(fn func()) {
	onButtonPressed(13, fn)
}

func OnButtonOnePressed(fn func()) {
	onButtonPressed(21, fn)
}

func OnButtonTwoPressed(fn func()) {
	onButtonPressed(20, fn)
}

func OnButtonThreePressed(fn func()) {
	onButtonPressed(16, fn)
}

func onButtonPressed(n int, fn func()) {
	go func() {
		p := gpioreg.ByName(fmt.Sprintf("GPIO%d", n))
		if err := p.In(gpio.PullUp, gpio.FallingEdge); err != nil {
			log.Fatal(err)
		}
		for {
			p.WaitForEdge(-1)
			fn()
		}
	}()
}
