package hardware

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
)

const (
	debouncedLow  uint8 = 0x00
	debouncedHigh uint8 = 0xFF

	gpioReadSampleRate = 5 * time.Millisecond // Rate at which GPIO pin is sampled for debouncing

	autoRepeatInitialDelay = 500 * time.Millisecond // Initial delay before button auto-repeat starts
	autoRepeatMaxTime      = 2 * time.Second        // Time to reach max auto-repeat rate
	autoRepeatInitialRate  = 200 * time.Millisecond // Initial auto-repeat rate
	autoRepeatMaxRate      = 50 * time.Millisecond  // Fastest auto-repeat rate
)

// buttonAutoRepeat manages auto-repeat state for button presses.
type buttonAutoRepeat struct {
	sync.Mutex

	cancel context.CancelFunc
	active bool
}

// getAutoRepeatManager returns the singleton auto-repeat manager instance.
func getAutoRepeatManager() *buttonAutoRepeat {
	// Use function-level static variables to avoid globals
	type container struct {
		instance *buttonAutoRepeat
		once     sync.Once
	}

	// This static variable is scoped to the function, not global
	static := &container{}

	static.once.Do(func() {
		static.instance = &buttonAutoRepeat{}
	})

	return static.instance
}

// OnGPIOButtonPressed sets up a GPIO pin to call the provided handler function when the button is pressed.
func OnGPIOButtonPressed(n int, handler func()) {
	go func() {
		// Prepare the GPIO pin for input
		pin := gpioreg.ByName(fmt.Sprintf("GPIO%d", n))

		err := pin.In(gpio.PullUp, gpio.BothEdges)
		if err != nil {
			log.Fatal(err)
		}

		// Initialise the stable state and debounce buffer to a button up state
		lastStableLevel := gpio.High
		gpioStates := debouncedHigh

		// button edge detection and debounce
		for {
			// Wait for any edge (state change)
			pin.WaitForEdge(-1)

			// Start debounce sampling when edge is detected
			ticker := time.NewTicker(gpioReadSampleRate)

		debouncer:
			for {
				select {
				case <-ticker.C:
					updateGPIOStates(pin, &gpioStates)

					if stableLevel, isStable := getStableGPIOState(gpioStates); isStable {
						if stableLevel != lastStableLevel {
							lastStableLevel = stableLevel

							// Trigger callback on stable transition to LOW (button pressed with pull-up)
							if stableLevel == gpio.Low {
								handler()
								startAutoRepeat(pin, handler)
							}
						}

						break debouncer
					}
				default:
					// Reset the debounce buffer if another edge occurred during debouncing
					if pin.WaitForEdge(0) { // Non-blocking
						gpioStates = 0x00
					}
				}
			}

			ticker.Stop()
		}
	}()
}

// updateGPIOStates reads the pin and updates the GPIO state chronology value.
func updateGPIOStates(pin gpio.PinIO, buffer *uint8) {
	pinLevel := pin.Read()

	// Shift buffer left and add new bit
	*buffer <<= 1
	if pinLevel == gpio.High {
		*buffer |= 1
	}
}

// getStableGPIOState checks if the current GPIO state chronology represents a stable state
// Returns the stable gpio.Level and true if stable, otherwise false.
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

// stopActiveAutoRepeat stops any active auto-repeat and marks the system as available.
func stopActiveAutoRepeat() {
	manager := getAutoRepeatManager()

	manager.Lock()
	defer manager.Unlock()

	if manager.active && manager.cancel != nil {
		manager.cancel()
	}

	manager.active = false
	manager.cancel = nil
}

// startAutoRepeat starts a new auto-repeat session, stopping any existing one.
func startAutoRepeat(pin gpio.PinIO, function func()) {
	stopActiveAutoRepeat()

	// Start a new auto-repeat handler
	ctx, cancel := context.WithCancel(context.Background())

	manager := getAutoRepeatManager()
	manager.Lock()
	manager.cancel = cancel
	manager.active = true
	manager.Unlock()

	go func() {
		defer stopActiveAutoRepeat()

		// Initial delay before repeat begins or another button is pressed
		select {
		case <-time.After(autoRepeatInitialDelay):
		case <-ctx.Done():
			return
		}

		// Check if button is still pressed after delay
		if pin.Read() == gpio.High {
			return
		}

		repeatStartTime := time.Now()

		for {
			// Finish auto repeat if the button has been released
			if pin.Read() == gpio.High {
				return
			}

			function()

			elapsed := time.Since(repeatStartTime)

			// Linear acceleration of repeat rate
			acceleration := min(
				1.0,
				float64(elapsed)/float64(autoRepeatMaxTime),
			)

			// Interpolate between initial and accelerated repeat rates
			repeatRate := float64(autoRepeatInitialRate) - (float64(autoRepeatInitialRate-autoRepeatMaxRate) * acceleration)

			select {
			case <-time.After(time.Duration(repeatRate)):
			case <-ctx.Done():
				return
			}
		}
	}()
}
