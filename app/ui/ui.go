// Package ui provides the user interface management for the application.
//
// The UI follows a small Model-View-Update loop: Run (in ui_loop.go) is the sole
// owner of uiState; events (HID input, telemetry ticks, commands) mutate the
// state in the "update" handlers below, and a single pure view() (in ui_scene.go)
// turns the state into a scene that is rendered on change. To answer "what is on
// screen?", read view(); to answer "how did we get here?", read the handlers.
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

// LiveData holds the dynamic telemetry that can be displayed on the UI.
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
}

// uiState is the complete mutable UI state. The Run event loop is its sole owner,
// so no field needs locking. view() is a pure function of this state.
type uiState struct {
	mode           ScreenMode
	lastData       LiveData       // most recent telemetry tick
	activeLiveView languagedb.Key // gear view or dashboard
	gearText       string         // resolved gear/status text (NULL gears keep the last value)
	flashOn        bool           // rev-limit background flash toggle
	forceRedraw    bool           // redraw next frame even if the scene is unchanged
	menuScene      scene          // scene for the current menu page, set during navigation

	startTime        time.Time
	lastActivity     time.Time
	lastMenuActivity time.Time
	hidReady         bool // HID events are discarded for the first 2s after startup
}

// UserInterface manages the user interface components and state.
type UserInterface struct {
	// Dependencies.
	i18n             *i18n.I18n
	display          hardware.Display
	Screen           *gui.Screen
	menuSystem       *MenuSystem
	settingsCallback func(setting languagedb.Key, action string) string
	log              zerolog.Logger
	now              func() time.Time // overridable in tests for deterministic timeouts

	// Event-loop channels: the only cross-goroutine surface.
	hidEvents chan HIDInputEvent
	ticks     chan LiveData
	commands  chan command
	done      chan exitcode.Code

	// State, owned exclusively by the Run loop.
	state     uiState
	lastScene scene // the scene currently on the display, for render-on-change
}

// NewUserInterface initializes and returns a new UserInterface instance.
func NewUserInterface(config *Config) *UserInterface {
	now := time.Now

	userInterface := &UserInterface{
		i18n:             config.I18n,
		display:          config.Display,
		hidEvents:        config.HIDEvents,
		menuSystem:       NewMenuSystem(),
		log:              config.Log.With().Str("package", "ui").Logger(),
		settingsCallback: config.SettingsCallback,
		done:             config.ExitCodeChan,
		now:              now,
		ticks:            make(chan LiveData, 1),
		commands:         make(chan command, 4),
		state: uiState{
			mode:           ScreenModeStartup,
			lastData:       LiveData{Gear: kinematics.NullGear},
			activeLiveView: languagedb.UIMenuLiveView,
			startTime:      now(),
			lastActivity:   now(),
		},
	}

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
	if u.state.mode != m {
		u.log.Debug().Str("from", u.state.mode.String()).Str("to", m.String()).Msg("mode transition")
	}

	u.state.mode = m
}

// registerActivity updates the last activity timestamp, resetting the power-off timeout.
func (u *UserInterface) registerActivity() {
	u.state.lastActivity = u.now()
}

// displaySleep puts the display into sleep mode.
func (u *UserInterface) displaySleep() {
	if u.state.mode == ScreenModeSleep {
		return
	}

	u.display.Sleep()
	u.setMode(ScreenModeSleep)
}

// displayToggleOff toggles the display between off and wait modes.
func (u *UserInterface) displayToggleOff() bool {
	if u.state.mode == ScreenModeOff {
		u.setMode(ScreenModeWait)
		u.state.forceRedraw = true

		return true
	}

	u.setMode(ScreenModeOff)

	return false
}

// settingAction performs a settings action and returns the resulting setting value.
func (u *UserInterface) settingAction(setting languagedb.Key, action string) string {
	u.registerActivity()
	u.setMode(ScreenModeSettings)

	return u.settingsCallback(setting, action)
}

// handleTick advances the UI state for one telemetry tick based on the current mode.
func (u *UserInterface) handleTick(data LiveData) {
	switch u.state.mode {
	case ScreenModeSettings:
		u.tickSettings(data)
	case ScreenModeLive:
		u.showLiveOrReady(data)
	case ScreenModeStartup:
		u.tickStartup(data)
	case ScreenModeWait:
		u.tickWait(data)
	case ScreenModeSleep:
		u.tickSleep(data)
	case ScreenModeOff:
		u.display.Clear()
		u.display.Sleep()
	default:
		u.log.Warn().Str("state", "invalid").Int("mode", int(u.state.mode)).Msg("display update")
	}

	u.state.lastData = data
}

// tickSettings exits settings mode after 10s of menu inactivity, returning to the
// live view if telemetry is active or sleeping otherwise.
func (u *UserInterface) tickSettings(data LiveData) {
	if u.state.lastMenuActivity.IsZero() || u.now().Sub(u.state.lastMenuActivity) <= 10*time.Second {
		return
	}

	u.log.Debug().Msg("Menu inactivity timeout - exiting settings")
	u.state.lastMenuActivity = time.Time{}

	if data.TelemetryActive {
		u.state.forceRedraw = true
		u.enterLive(data)
	} else {
		u.displaySleep()
	}
}

// tickStartup leaves the boot splash for the live or ready view once the splash
// timeout elapses.
func (u *UserInterface) tickStartup(data LiveData) {
	if !u.displaySplashTimeoutReached() {
		return
	}

	u.showLiveOrReady(data)

	if !data.TelemetryActive {
		u.registerActivity()
	}
}

// tickWait shows the live view when telemetry returns, or sleeps after the
// power-off timeout.
func (u *UserInterface) tickWait(data LiveData) {
	if data.TelemetryActive {
		u.enterLive(data)

		return
	}

	if u.displayPowerOffTimeoutReached() {
		u.display.Sleep()
		u.setMode(ScreenModeSleep)
	}
}

// tickSleep wakes to the live view when telemetry returns.
func (u *UserInterface) tickSleep(data LiveData) {
	if data.TelemetryActive {
		u.enterLive(data)
	}
}

// showLiveOrReady shows the live view if telemetry is active, otherwise the ready view.
func (u *UserInterface) showLiveOrReady(data LiveData) {
	if data.TelemetryActive {
		u.enterLive(data)
	} else {
		u.enterWait()
	}
}

// enterLive updates the live-view model from telemetry and switches to live mode.
// The activity timestamp is refreshed by render() whenever a live frame is drawn.
func (u *UserInterface) enterLive(data LiveData) {
	u.updateLiveModel(data)
	u.setMode(ScreenModeLive)
}

// enterWait switches to the ready view, unless a menu is on screen.
func (u *UserInterface) enterWait() {
	if u.state.mode == ScreenModeSettings {
		return
	}

	u.setMode(ScreenModeWait)
}

// updateLiveModel folds telemetry into the live-view model: it advances the
// rev-limit flash (dashboard only) and resolves the gear text. A NULL gear leaves
// the last shown gear in place rather than blanking it on a transient reading.
func (u *UserInterface) updateLiveModel(data LiveData) {
	if u.state.activeLiveView == languagedb.UIMenuLiveDashboard {
		if data.RevLightMax > 0 && data.RPM >= data.RevLightMax {
			u.state.flashOn = !u.state.flashOn
		} else {
			u.state.flashOn = false
		}
	}

	switch {
	case data.Calibrating:
		u.state.gearText = u.i18n.GetString(languagedb.UICalibrating)
	case data.Gear != kinematics.NullGear:
		u.state.gearText = kinematics.GearName(data.Gear)
	}
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

// displayPowerOffTimeoutReached checks if the power-off timeout has been reached.
func (u *UserInterface) displayPowerOffTimeoutReached() bool {
	return u.now().Sub(u.state.lastActivity) > 30*time.Second
}

// displaySplashTimeoutReached checks if the splash screen timeout has been reached.
func (u *UserInterface) displaySplashTimeoutReached() bool {
	return u.now().Sub(u.state.lastActivity) > 2*time.Second
}
