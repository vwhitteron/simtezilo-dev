package app

import (
	"fmt"
	"image"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/hardware"
)

type displayState int

const (
	off displayState = iota
	wait
	startup
	live
	settings
)

type display struct {
	lcdDevice hardware.LCD
	state     displayState
	gear      int
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

func (a *App) powerOffDisplay() {
	if a.display.state == displayState(off) {
		return
	}

	a.display.lcdDevice.PowerOff()

	a.display.gear = NullGear
	a.display.state = displayState(off)

	a.log.Debug().Str("screen", "power off").Msg("display update")
}

func (a *App) drawStartupDisplay(text string) {
	a.display.lcdDevice.PowerOn()
	a.display.lcdDevice.ShowTextOverlay("splash", text, 9)

	a.display.gear = NullGear
	a.display.state = displayState(startup)

	a.log.Debug().Str("screen", "startup").Msg("display update")
}

func (a *App) drawWaitDisplay() {
	if a.display.state == displayState(wait) {
		return
	}

	a.log.Debug().Str("display", fmt.Sprintf("%+v", a.display)).Msg("drawing wait display")
	a.display.lcdDevice.PowerOn()
	a.display.lcdDevice.ShowTextOverlay("splash", "Waiting", 9)

	a.display.gear = NullGear
	a.display.state = displayState(wait)

	a.log.Debug().Str("screen", "wait").Msg("display update")
}

func (a *App) drawLiveDisplay() {
	currentGear := a.kinematics.Current.TransmissionGear

	if a.display.gear == currentGear || currentGear == NullGear {
		return
	}

	a.display.lcdDevice.PowerOn()

	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	a.display.lcdDevice.ShowTextCentered(canvas, gearName(currentGear), gearFontSize)

	a.display.gear = currentGear
	a.display.state = displayState(live)
	a.updateLastActive()

	a.log.Debug().Str("screen", "gear").Msg("display update")
}

func (a *App) drawSettingsDisplay(displayContent string, backlightIsOn bool) {
	if !backlightIsOn {
		a.display.state = displayState(off)

		return
	}

	a.display.lcdDevice.PowerOn()

	if displayContent != "" {
		canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
		a.display.lcdDevice.ShowTextCentered(canvas, displayContent, 20)
	}

	a.display.state = displayState(settings)
	a.updateLastActive()

	a.log.Debug().Str("screen", "settings").Msg("display update")
}

func (a *App) updateDisplay() {
	if a.shouldGenerateHaptics() {
		a.drawLiveDisplay()
	} else if a.displayPowerOffTimeoutReached() {
		if a.display.state > displayState(off) {
			a.powerOffDisplay()
		}
	} else if a.displayInactiveTimeoutReached() {
		if a.display.state > displayState(wait) {
			a.drawWaitDisplay()
		}
	}
}
