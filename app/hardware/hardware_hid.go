package hardware

import (
	"fmt"
	"log"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
)

const (
	debouncedLow  = 0x00
	debouncedHigh = 0xFF

	gpioReadSampleRate = 5 * time.Millisecond // Rate at which GPIO pin is sampled for debouncing

	autoRepeatInitialDelay = 500 * time.Millisecond // Initial delay before button auto-repeat starts
	autoRepeatMaxTime      = 2 * time.Second        // Time to reach max auto-repeat rate
	autoRepeatInitialRate  = 200 * time.Millisecond // Initial auto-repeat rate
	autoRepeatMaxRate      = 50 * time.Millisecond  // Fastest auto-repeat rate
)

func OnGPIOButtonPressed(n int, fn func()) {
	go func() {
		// Prepare the GPIO pin for input
		p := gpioreg.ByName(fmt.Sprintf("GPIO%d", n))

		if err := p.In(gpio.PullUp, gpio.BothEdges); err != nil {
			log.Fatal(err)
		}

		// Initialise the stable state and debounce buffer based on the current GPIO level
		lastStableLevel, gpioStates := initStableGPIOState(p)

		// button edge detection and debounce
		for {
			// Wait for any edge (state change)
			p.WaitForEdge(-1)

			// Start debounce sampling when edge is detected
			ticker := time.NewTicker(gpioReadSampleRate)

		debouncer:
			for {
				select {
				case <-ticker.C:
					updateGPIOStates(p, &gpioStates)

					if stableLevel, isStable := getStableGPIOState(gpioStates); isStable {
						if stableLevel != lastStableLevel {
							lastStableLevel = stableLevel

							// Trigger callback on stable transition to LOW (button pressed with pull-up)
							if stableLevel == gpio.Low {
								fn()

								// Auto-repeat when the button is held down
								go func() {
									// Initial delay before repeat begins
									time.Sleep(autoRepeatInitialDelay)

									repeatStartTime := time.Now()

									for {
										// Exit automatic repeat if button has been released
										if p.Read() == gpio.High {
											return
										}

										fn()

										elapsed := time.Since(repeatStartTime)

										// Linear acceleration of repeat rate
										acceleration := min(
											1.0,
											float64(elapsed)/float64(autoRepeatMaxTime),
										)

										// Interpolate between initial and acclerated repeat rates
										repeatRate := float64(autoRepeatInitialRate) - (float64(autoRepeatInitialRate-autoRepeatMaxRate) * acceleration)

										time.Sleep(time.Duration(repeatRate))
									}
								}()
							}
						}
						break debouncer
					}
				default:
					// Reset the debounce buffer if another edge occurred during debouncing
					if p.WaitForEdge(0) { // Non-blocking
						gpioStates = 0x00
					}
				}
			}

			ticker.Stop()
		}

	}()
}

// initStableGPIOState waits for a stable GPIO state and returns the GPIO level
// and the associated GPIO state chronology value
func initStableGPIOState(pin gpio.PinIO) (gpio.Level, uint8) {
	var gpioStates uint8 = 0x00
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		updateGPIOStates(pin, &gpioStates)

		// Check if state has stabilised
		if stableLevel, isStable := getStableGPIOState(gpioStates); isStable {
			return stableLevel, gpioStates
		}

		<-ticker.C
	}
}

// updateGPIOStates reads the pin and updates the GPIO state chronology value
func updateGPIOStates(pin gpio.PinIO, buffer *uint8) {
	pinLevel := pin.Read()

	// Shift buffer left and add new bit
	*buffer <<= 1
	if pinLevel == gpio.High {
		*buffer |= 1
	}
}

// getStableGPIOState checks if the current GPIO state chronology represents a stable state
// Returns the stable gpio.Level and true if stable, otherwise false
func getStableGPIOState(gpioStates uint8) (gpio.Level, bool) {
	switch gpioStates {
	case debouncedHigh:
		return gpio.High, true
	case debouncedLow:
		return gpio.Low, true
	default: // Not yet stable
		return gpio.Low, false
	}
}
