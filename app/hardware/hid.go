package hardware

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/host/v3"
)

func Init() {
	if _, err := host.Init(); err != nil {
		log.Fatal(err)
	}
}

func OnGPIOButtonPressed(n int, fn func()) {
	go func() {
		p := gpioreg.ByName(fmt.Sprintf("GPIO%d", n))

		if err := p.In(gpio.PullUp, gpio.FallingEdge); err != nil {
			log.Fatal(err)
		}

		// stableState := uint8(0)
		for {
			p.WaitForEdge(-1)

			fn()
			time.Sleep(250 * time.Millisecond)

			// level := p.Read()
			// stableState = stableState << 1

			// if level {
			// 	stableState = stableState | 0x1
			// } else {
			// 	stableState = stableState | 0x1
			// }

			// if stableState == 0xf || stableState == 0x0 {
			// 	fn()
			// }
			// time.Sleep(5 * time.Millisecond)
		}
	}()
}
