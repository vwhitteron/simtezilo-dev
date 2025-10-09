// Package ui provides the user interface management for the application.
package ui

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

// Config holds the configuration for initializing the UserInterface.
type Config struct {
	I18n             *i18n.I18n
	HIDEvents        chan HIDInputEvent
	Display          hardware.Display
	LiveData         *LiveData
	SettingsCallback func(string, string) string
	Done             chan bool
	Log              zerolog.Logger
}

// LiveData holds the dynamic data that can currently be displayed on the UI.
type LiveData struct {
	Gear            int
	TelemetryActive bool
	forceRefresh    bool
}

// UserInterface manages the user interface components and state.
type UserInterface struct {
	i18n       *i18n.I18n
	display    hardware.Display
	Screen     *gui.Screen
	hidEvents  chan HIDInputEvent
	menuSystem *MenuSystem

	log zerolog.Logger

	settingsCallback func(menuPage string, action string) string
	done             chan bool

	displayData  LiveData
	mode         ScreenMode
	startTime    time.Time
	lastActivity time.Time
}

// NewUserInterface initializes and returns a new UserInterface instance.
func NewUserInterface(config *Config) *UserInterface {
	userInterface := &UserInterface{
		i18n:             config.I18n,
		display:          config.Display,
		hidEvents:        config.HIDEvents,
		menuSystem:       NewMenuSystem(),
		log:              config.Log.With().Str("package", "ui").Logger(),
		displayData:      LiveData{Gear: kinematics.NullGear},
		mode:             ScreenModeStartup,
		startTime:        time.Now(),
		lastActivity:     time.Now(),
		settingsCallback: config.SettingsCallback,
		done:             config.Done,
	}

	var err error

	userInterface.Screen, err = gui.NewScreen(&gui.Config{
		DisplayDevice: config.Display,
		I18n:          config.I18n,
	})
	if err != nil {
		userInterface.log.Error().
			Err(err).
			Str("sub-component", "screen").
			Str("result", "failure").
			Msg("init")
	}

	return userInterface
}

// RegisterActivity updates the last activity timestamp to the current time.
func (u *UserInterface) RegisterActivity() {
	u.lastActivity = time.Now()
}

// DisplaySleep puts the display into sleep mode.
func (u *UserInterface) DisplaySleep() {
	if int(u.mode) == int(ScreenModeSleep) {
		return
	}

	u.display.Sleep()

	u.mode = ScreenModeSleep

	u.log.Debug().Str("state", "sleep").Msg("display update")
}

// DisplayOff turns the display off.
func (u *UserInterface) DisplayOff() {
	if int(u.mode) == int(ScreenModeOff) {
		return
	}

	u.display.Sleep()

	u.mode = ScreenModeOff

	u.log.Debug().Str("state", "off").Msg("display update")
}

// DisplayToggleOff toggles the display between off and wait modes.
func (u *UserInterface) DisplayToggleOff() bool {
	if int(u.mode) == int(ScreenModeOff) {
		u.mode = ScreenModeWait
		u.displayData.forceRefresh = true

		u.log.Debug().Str("state", "live").Msg("display update")

		return true
	}

	u.mode = ScreenModeOff

	u.log.Debug().Str("state", "off").Msg("display update")

	return false
}

// DrawReadyDisplay renders the ready screen on the display.
// TODO: move it elsewhere or get rid of it entirely.
func (u *UserInterface) DrawReadyDisplay() {
	if int(u.mode) == int(ScreenModeWait) && !u.displayData.forceRefresh {
		return
	}

	_ = u.Screen.RenderSplashScreen(u.i18n.GetString("ui.ready"))

	u.mode = ScreenModeWait

	u.log.Debug().Str("state", "wait").Msg("display update")
}

// DrawLiveDisplay renders the live data screen on the display.
func (u *UserInterface) DrawLiveDisplay(data LiveData) {
	if !u.displayData.forceRefresh {
		if data.Gear == u.displayData.Gear || data.Gear == kinematics.NullGear {
			return
		}
	}

	_ = u.Screen.RenderLiveScreen(kinematics.GearName(data.Gear))

	u.displayData = data
	u.mode = ScreenModeLive
	u.RegisterActivity()

	u.log.Debug().Str("state", "live").Msg("display update")
}

// SettingAction performs a settings action and returns the resulting setting value.
func (u *UserInterface) SettingAction(setting string, action string) string {
	u.RegisterActivity()
	u.mode = ScreenModeSettings

	return u.settingsCallback(setting, action)
}

// UpdateDisplay updates the display based on the current mode and live data.
// TODO: clean up this logic and make it easier to understand.
func (u *UserInterface) UpdateDisplay(data LiveData) {
	switch u.mode {
	case ScreenModeSettings:
		if u.displayInactiveTimeoutReached() {
			if data.TelemetryActive {
				u.DrawLiveDisplay(data)
			} else {
				u.DrawReadyDisplay()
			}
		}
	case ScreenModeLive:
		if data.TelemetryActive {
			u.DrawLiveDisplay(data)
		} else {
			u.DrawReadyDisplay()
		}
	case ScreenModeStartup:
		if u.displaySplashTimeoutReached() {
			if data.TelemetryActive {
				u.DrawLiveDisplay(data)
			} else {
				u.DrawReadyDisplay()
				u.RegisterActivity()
			}
		}
	case ScreenModeWait:
		if data.TelemetryActive {
			u.DrawLiveDisplay(data)
		} else if u.displayPowerOffTimeoutReached() {
			u.mode = ScreenModeSleep
			u.display.Sleep()
		}
	case ScreenModeSleep:
		if data.TelemetryActive {
			u.DrawLiveDisplay(data)
		}
	case ScreenModeOff:
		u.display.Clear()
		u.display.Sleep()
	default:
		u.log.Warn().Str("state", "invalid").Int("mode", int(u.mode)).Msg("display update")
	}

	u.displayData = data
}

// displayPowerOffTimeoutReached checks if the power-off timeout has been reached.
func (u *UserInterface) displayPowerOffTimeoutReached() bool {
	return time.Since(u.lastActivity) > 30*time.Second
}

// displayInactiveTimeoutReached checks if the inactivity timeout has been reached.
func (u *UserInterface) displayInactiveTimeoutReached() bool {
	return time.Since(u.lastActivity) > 5*time.Second
}

// displaySplashTimeoutReached checks if the splash screen timeout has been reached.
func (u *UserInterface) displaySplashTimeoutReached() bool {
	return time.Since(u.lastActivity) > 2*time.Second
}
