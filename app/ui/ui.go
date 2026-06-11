// Package ui provides the user interface management for the application.
package ui

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

// Config holds the configuration for initializing the UserInterface.
type Config struct {
	I18n             *i18n.I18n
	HIDEvents        chan HIDInputEvent
	Display          hardware.Display
	SettingsCallback func(languagedb.Key, string) string
	DevToolsEnabled  func() bool
	ExitCodeChan     chan exitcode.Code
	Log              zerolog.Logger
}

// LiveData holds the dynamic data that can currently be displayed on the UI.
type LiveData struct {
	Gear            int
	TelemetryActive bool
	Calibrating     bool

	// Dashboard live-view telemetry. Throttle/brake are percentages (0..100).
	SpeedKPH    int
	RPM         int
	RevLimit    int
	RevLightMin int
	RevLightMax int
	ThrottleIn  float64
	ThrottleOut float64
	BrakeIn     float64
	BrakeOut    float64

	forceRefresh bool
}

// UserInterface manages the user interface components and state.
type UserInterface struct {
	i18n       *i18n.I18n
	display    hardware.Display
	Screen     *gui.Screen
	hidEvents  chan HIDInputEvent
	menuSystem *MenuSystem

	log zerolog.Logger

	settingsCallback func(setting languagedb.Key, action string) string
	done             chan exitcode.Code

	displayData      LiveData
	mode             ScreenMode
	startTime        time.Time
	lastMenuActivity time.Time
	lastActivity     time.Time

	// activeLiveView is the live-view leaf currently selected (gear view or
	// dashboard). It determines which screen the live display renders.
	activeLiveView languagedb.Key
	// dashFlashOn toggles each dashboard frame while at/over the rev limit so the
	// background blinks.
	dashFlashOn bool

	// Event-loop channels. These are the only cross-goroutine surface; all
	// other fields are owned exclusively by the Run loop.
	ticks    chan LiveData
	commands chan command

	// hidReady gates HID events for the first 2 seconds after startup.
	hidReady bool
	// now returns the current time; overridable in tests for deterministic timeouts.
	now func() time.Time
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
		done:             config.ExitCodeChan,
		activeLiveView:   languagedb.UIMenuLiveView,
		ticks:            make(chan LiveData, 1),
		commands:         make(chan command, 4),
		now:              time.Now,
	}

	// Set devToolsEnabled callback if provided
	if config.DevToolsEnabled != nil {
		userInterface.menuSystem.SetDevToolsEnabledCallback(config.DevToolsEnabled)
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

// setMode is the single point where the screen mode changes. Centralising it
// keeps transitions greppable and logs every change.
func (u *UserInterface) setMode(m ScreenMode) {
	if u.mode != m {
		u.log.Debug().Str("from", u.mode.String()).Str("to", m.String()).Msg("mode transition")
	}

	u.mode = m
}

// registerActivity updates the last activity timestamp to the current time.
func (u *UserInterface) registerActivity() {
	u.lastActivity = u.now()
}

// displaySleep puts the display into sleep mode.
func (u *UserInterface) displaySleep() {
	if int(u.mode) == int(ScreenModeSleep) {
		return
	}

	u.display.Sleep()

	u.setMode(ScreenModeSleep)

	u.log.Debug().Str("state", "sleep").Msg("display update")
}

// displayOff turns the display off.
func (u *UserInterface) displayOff() {
	if int(u.mode) == int(ScreenModeOff) {
		return
	}

	u.display.Sleep()

	u.setMode(ScreenModeOff)

	u.log.Debug().Str("state", "off").Msg("display update")
}

// displayToggleOff toggles the display between off and wait modes.
func (u *UserInterface) displayToggleOff() bool {
	if int(u.mode) == int(ScreenModeOff) {
		u.setMode(ScreenModeWait)
		u.displayData.forceRefresh = true

		u.log.Debug().Str("state", "live").Msg("display update")

		return true
	}

	u.setMode(ScreenModeOff)

	u.log.Debug().Str("state", "off").Msg("display update")

	return false
}

// drawReadyDisplay renders the ready screen on the display.
// TODO: move it elsewhere or get rid of it entirely.
func (u *UserInterface) drawReadyDisplay() {
	// Don't override settings mode
	if int(u.mode) == int(ScreenModeSettings) {
		return
	}

	if int(u.mode) == int(ScreenModeWait) && !u.displayData.forceRefresh {
		return
	}

	if u.activeLiveView == languagedb.UIMenuLiveDashboard {
		_ = u.Screen.RenderDashboardScreen(gui.DashboardData{
			Gear:  u.i18n.GetString(languagedb.UIReady),
			Ready: true,
		})
		u.setMode(ScreenModeWait)
		u.log.Debug().Str("state", "wait").Msg("display update")

		return
	}

	_ = u.Screen.RenderSplashScreen(u.i18n.GetString(languagedb.UIReady))

	u.setMode(ScreenModeWait)

	u.log.Debug().Str("state", "wait").Msg("display update")
}

// drawLiveDisplay renders the active live-view screen on the display.
func (u *UserInterface) drawLiveDisplay(data LiveData) {
	// The dashboard view is driven by continuously-changing telemetry, so it
	// redraws every frame rather than only on a gear change.
	if u.activeLiveView == languagedb.UIMenuLiveDashboard {
		_ = u.Screen.RenderDashboardScreen(u.dashboardData(data))

		u.displayData = data
		u.setMode(ScreenModeLive)
		u.registerActivity()

		return
	}

	if !u.displayData.forceRefresh {
		if data.Calibrating == u.displayData.Calibrating &&
			(data.Gear == u.displayData.Gear || data.Gear == kinematics.NullGear) {
			return
		}
	}

	displayValue := kinematics.GearName(data.Gear)
	if data.Calibrating {
		displayValue = u.i18n.GetString(languagedb.UICalibrating)
	}

	_ = u.Screen.RenderLiveScreen(displayValue)

	u.displayData = data
	u.setMode(ScreenModeLive)
	u.registerActivity()

	u.log.Debug().Str("state", "live").Msg("display update")
}

// renderActiveLiveView renders the currently selected live view using the last
// known telemetry. Used when entering or paging between live views.
func (u *UserInterface) renderActiveLiveView() {
	if u.activeLiveView == languagedb.UIMenuLiveDashboard {
		if u.displayData.Gear == kinematics.NullGear {
			_ = u.Screen.RenderDashboardScreen(gui.DashboardData{
				Gear:  u.i18n.GetString(languagedb.UIReady),
				Ready: true,
			})
		} else {
			_ = u.Screen.RenderDashboardScreen(u.dashboardData(u.displayData))
		}

		return
	}

	// Gear view: without telemetry show "Ready" rather than the "NULL" placeholder.
	gearValue := kinematics.GearName(u.displayData.Gear)
	if u.displayData.Gear == kinematics.NullGear {
		gearValue = u.i18n.GetString(languagedb.UIReady)
	}

	_ = u.Screen.RenderLiveScreen(gearValue)
}

// dashboardData maps the live telemetry into the gui dashboard model and advances
// the rev-limit flash toggle.
func (u *UserInterface) dashboardData(data LiveData) gui.DashboardData {
	flash := false

	if data.RevLightMax > 0 && data.RPM >= data.RevLightMax {
		u.dashFlashOn = !u.dashFlashOn
		flash = u.dashFlashOn
	} else {
		u.dashFlashOn = false
	}

	gear := kinematics.GearName(data.Gear)
	if data.Gear == kinematics.NullGear {
		gear = ""
	}

	return gui.DashboardData{
		Gear:        gear,
		SpeedKPH:    data.SpeedKPH,
		RPM:         data.RPM,
		RevLimit:    data.RevLimit,
		RevLightMin: data.RevLightMin,
		RevLightMax: data.RevLightMax,
		ThrottleIn:  data.ThrottleIn,
		ThrottleOut: data.ThrottleOut,
		BrakeIn:     data.BrakeIn,
		BrakeOut:    data.BrakeOut,
		Flash:       flash,
	}
}

// settingAction performs a settings action and returns the resulting setting value.
func (u *UserInterface) settingAction(setting languagedb.Key, action string) string {
	u.registerActivity()
	u.setMode(ScreenModeSettings)

	return u.settingsCallback(setting, action)
}

// handleTick renders the display for one telemetry tick based on the current mode.
func (u *UserInterface) handleTick(data LiveData) {
	switch u.mode {
	case ScreenModeSettings:
		u.handleSettingsMode(data)
	case ScreenModeLive:
		u.handleLiveMode(data)
	case ScreenModeStartup:
		u.handleStartupMode(data)
	case ScreenModeWait:
		u.handleWaitMode(data)
	case ScreenModeSleep:
		u.handleSleepMode(data)
	case ScreenModeOff:
		u.handleOffMode()
	default:
		u.log.Warn().Str("state", "invalid").Int("mode", int(u.mode)).Msg("display update")
	}

	u.displayData = data
}

// GetSetupModeCountdown returns the current setup mode countdown value.
func (u *UserInterface) GetSetupModeCountdown() int {
	return u.menuSystem.GetSetupModeCountdown()
}

// ResetSetupModeCountdown resets the setup mode countdown to 5.
func (u *UserInterface) ResetSetupModeCountdown() int {
	return u.menuSystem.ResetSetupModeCountdown()
}

// DecrementSetupModeCountdown decrements the setup countdown by 1.
func (u *UserInterface) DecrementSetupModeCountdown() int {
	return u.menuSystem.DecrementSetupModeCountdown()
}

// IsSetupModeCountdownZero returns true if setup countdown has reached zero.
func (u *UserInterface) IsSetupModeCountdownZero() bool {
	return u.menuSystem.IsSetupModeCountdownZero()
}

// handleSettingsMode handles display updates in settings mode.
// Uses lastMenuActivity for timeout checks to prevent gear changes from
// interrupting menu navigation immediately after button presses.
func (u *UserInterface) handleSettingsMode(data LiveData) {
	// Check for 10-second inactivity timeout in menu
	if !u.lastMenuActivity.IsZero() && u.now().Sub(u.lastMenuActivity) > 10*time.Second {
		u.log.Debug().Msg("Menu inactivity timeout - exiting settings")
		u.lastMenuActivity = time.Time{} // Reset timer

		// Show live display if telemetry is active, otherwise sleep
		if data.TelemetryActive {
			// Force refresh to ensure we transition even if gear hasn't changed
			u.displayData.forceRefresh = true
			u.drawLiveDisplay(data)
		} else {
			u.displaySleep()
		}
	}
}

// handleLiveMode handles display updates in live mode.
func (u *UserInterface) handleLiveMode(data LiveData) {
	u.showActiveOrReadyDisplay(data)
}

// handleStartupMode handles display updates in startup mode.
func (u *UserInterface) handleStartupMode(data LiveData) {
	if u.displaySplashTimeoutReached() {
		u.showActiveOrReadyDisplay(data)

		if !data.TelemetryActive {
			u.registerActivity()
		}
	}
}

// handleWaitMode handles display updates in wait mode.
func (u *UserInterface) handleWaitMode(data LiveData) {
	if data.TelemetryActive {
		u.drawLiveDisplay(data)
	} else if u.displayPowerOffTimeoutReached() {
		u.setMode(ScreenModeSleep)
		u.display.Sleep()
	}
}

// handleSleepMode handles display updates in sleep mode.
func (u *UserInterface) handleSleepMode(data LiveData) {
	if data.TelemetryActive {
		u.drawLiveDisplay(data)
	}
}

// handleOffMode handles display updates in off mode.
func (u *UserInterface) handleOffMode() {
	u.display.Clear()
	u.display.Sleep()
}

// showActiveOrReadyDisplay shows live display if telemetry is active, otherwise ready display.
func (u *UserInterface) showActiveOrReadyDisplay(data LiveData) {
	if data.TelemetryActive {
		u.drawLiveDisplay(data)
	} else {
		u.drawReadyDisplay()
	}
}

// displayPowerOffTimeoutReached checks if the power-off timeout has been reached.
func (u *UserInterface) displayPowerOffTimeoutReached() bool {
	return u.now().Sub(u.lastActivity) > 30*time.Second
}

// displaySplashTimeoutReached checks if the splash screen timeout has been reached.
func (u *UserInterface) displaySplashTimeoutReached() bool {
	return u.now().Sub(u.lastActivity) > 2*time.Second
}
