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

func (a *App) buttenEventCallback() func(bool) {
	return func(backlightIsOn bool) {
		if backlightIsOn {
			a.display.state = displayState(settings)
			a.updateLastActive()
		} else {
			a.display.state = displayState(off)
		}
	}
}

func (a *App) updateLastActive() {
	a.lastActive = time.Now()
}

func (a *App) displayPowerOffTimeoutReached() bool {
	return time.Since(a.lastActive) > 20*time.Second
}

func (a *App) displayInactiveTimeoutReached() bool {
	return time.Since(a.lastActive) > 5*time.Second
}

func (a *App) powerOffDisplay() {
	if a.display.state == displayState(off) {
		return
	}

	a.display.lcdDevice.PowerOff()

	a.display.gear = NullGear
	a.display.state = displayState(off)

	duration := time.Since(a.lastActive)
	a.log.Debug().Str("screen", "power off").Str("duration", duration.String()).Msg("display update")
}

func (a *App) drawStartupDisplay(text string) {
	a.display.lcdDevice.PowerOn()
	a.display.lcdDevice.ShowTextOverlay("splash", text, 7)

	a.display.gear = NullGear
	a.display.state = displayState(startup)

	duration := time.Since(a.lastActive)
	a.log.Debug().Str("screen", "startup").Str("duration", duration.String()).Msg("display update")
}

func (a *App) drawWaitDisplay() {
	if a.display.state == displayState(wait) {
		return
	}

	a.log.Debug().Str("display", fmt.Sprintf("%+v", a.display)).Msg("drawing wait display")
	a.display.lcdDevice.PowerOn()
	a.display.lcdDevice.ShowTextOverlay("splash", "waiting", 7)

	a.display.gear = NullGear
	a.display.state = displayState(wait)

	duration := time.Since(a.lastActive)
	a.log.Debug().Str("screen", "wait").Str("duration", duration.String()).Msg("display update")
}

func (a *App) drawLiveDisplay() {
	currentGear := a.kinematics.Current.TransmissionGear

	if a.display.gear == currentGear || currentGear == NullGear {
		return
	}

	a.display.lcdDevice.PowerOn()
	a.lastActive = time.Now()

	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	a.display.lcdDevice.ShowTextCentered(canvas, gearName(currentGear), gearFontSize)

	a.log.Debug().Str("screen", "gear").Msg("display update")

	a.display.gear = currentGear
	a.display.state = displayState(live)
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
