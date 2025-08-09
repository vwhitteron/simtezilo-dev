package app

import (
	"fmt"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

type displayState int

const (
	displayOff displayState = iota
	displayWait
	displayStartup
	displayLive
	displaySettings
)

type display struct {
	device hardware.Display
	screen *gui.Screen
	state  displayState
	gear   int
}

func (a *App) updateLastActive() {
	a.state.lastActive = time.Now()
}

func (a *App) displayPowerOffTimeoutReached() bool {
	return time.Since(a.state.lastActive) > 20*time.Second
}

func (a *App) displayInactiveTimeoutReached() bool {
	return time.Since(a.state.lastActive) > 5*time.Second
}

// TODO: remove this somehow
func (a *App) powerOffDisplay() {
	if a.display.state == displayOff {
		return
	}

	a.display.device.PowerOff()

	a.display.gear = NullGear
	a.display.state = displayOff

	a.log.Debug().Str("screen", "power off").Msg("display update")
}

// TODO: remove this somehow
func (a *App) drawStartupDisplay(text string) {
	a.display.device.PowerOn()
	a.display.screen.RenderSplashScreen(text)

	a.display.gear = NullGear
	a.display.state = displayStartup

	a.log.Debug().Str("screen", "startup").Msg("display update")
}

// TODO: remove this somehow
func (a *App) drawWaitDisplay() {
	if a.display.state == displayWait {
		return
	}

	a.log.Debug().Str("display", fmt.Sprintf("%+v", a.display)).Msg("drawing wait display")
	a.display.device.PowerOn()
	a.display.screen.RenderSplashScreen(a.i18n.GetString("ui.waiting"))

	a.display.gear = NullGear
	a.display.state = displayWait

	a.log.Debug().Str("screen", "wait").Msg("display update")
}

// TODO: remove this somehow
func (a *App) drawLiveDisplay() {
	currentGear := a.kinematics.Current.TransmissionGear

	if a.display.gear == currentGear || currentGear == NullGear {
		return
	}

	a.display.device.PowerOn()

	a.display.screen.RenderLiveScreen(gearName(currentGear))

	a.display.gear = currentGear
	a.display.state = displayLive
	a.updateLastActive()

	a.log.Debug().Str("screen", "gear").Msg("display update")
}

// TODO: do the screen rendering via the screen interface
func (a *App) updateDisplay() {
	if a.telemetryIsActive() {
		a.drawLiveDisplay()
	} else if a.displayPowerOffTimeoutReached() {
		if a.display.state > displayOff {
			a.powerOffDisplay()
		}
	} else if a.displayInactiveTimeoutReached() {
		if a.display.state > displayWait {
			a.drawWaitDisplay()
		}
	}
}
