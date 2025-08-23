package hardware

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
)

func OnGPIOButtonPressed(n int, fn func()) {
	go func() {
		p := gpioreg.ByName(fmt.Sprintf("GPIO%d", n))

		if err := p.In(gpio.PullUp, gpio.BothEdges); err != nil {
			log.Fatal(err)
		}

		const stableDelay = 50 * time.Millisecond
		var lastEdgeTime time.Time
		var lastStableState gpio.Level
		var currentState gpio.Level

		// Initialize with current state
		currentState = p.Read()
		lastStableState = currentState
		lastEdgeTime = time.Now()

		// State monitoring goroutine
		go func() {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				newState := p.Read()
				now := time.Now()

				if newState != currentState {
					// State changed, reset timer
					currentState = newState
					lastEdgeTime = now
				} else if newState != lastStableState && now.Sub(lastEdgeTime) >= stableDelay {
					// State has been stable for required duration and is different from last stable state
					lastStableState = newState

					// Trigger callback only on stable transition to LOW (button pressed with pull-up)
					if newState == gpio.Low {
						fn()
					}
				}
			}
		}()

		// Keep the edge detection running to wake up from any sleep states
		for {
			p.WaitForEdge(-1)
		}
	}()
}
