package ui

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

type Config struct {
	I18n             *i18n.Language
	HIDEvents        chan HIDInputEvent
	Display          hardware.Display
	LiveData         *LiveData
	SettingsCallback func(menuPage string, action string) string
	Done             chan bool
	Log              zerolog.Logger
}

type LiveData struct {
	Gear            int
	TelemetryActive bool
	forceRefresh    bool
}

type UserInterface struct {
	i18n       *i18n.Language
	display    hardware.Display
	Screen     *gui.Screen
	hidEvents  chan HIDInputEvent
	menuSystem *MenuSystem

	log zerolog.Logger

	alterSetting func(menuPage string, action string) string
	done         chan bool

	displayData  LiveData
	mode         ScreenMode
	startTime    time.Time
	lastActivity time.Time
}

func NewUserInterface(config *Config) *UserInterface {
	u := &UserInterface{
		i18n:         config.I18n,
		display:      config.Display,
		hidEvents:    config.HIDEvents,
		menuSystem:   NewMenuSystem(),
		log:          config.Log,
		displayData:  LiveData{Gear: kinematics.NullGear},
		mode:         ScreenModeStartup,
		startTime:    time.Now(),
		lastActivity: time.Now(),
		alterSetting: config.SettingsCallback,
		done:         config.Done,
	}

	var err error
	u.Screen, err = gui.NewScreen(&gui.Config{
		DisplayDevice: config.Display,
		I18n:          config.I18n,
	})
	if err != nil {
		u.log.Error().
			Err(err).
			Str("sub-component", "screen").
			Str("result", "failure").
			Msg("init")
	}

	return u
}

func (u *UserInterface) RegisterActivity() {
	u.lastActivity = time.Now()
}

func (u *UserInterface) DisplaySleep() {
	if int(u.mode) == int(ScreenModeSleep) {
		return
	}

	u.display.Sleep()

	u.mode = ScreenModeSleep

	u.log.Debug().Str("state", "sleep").Msg("display update")
}

func (u *UserInterface) DisplayOff() {
	if int(u.mode) == int(ScreenModeOff) {
		return
	}

	u.display.Sleep()

	u.mode = ScreenModeOff

	u.log.Debug().Str("state", "off").Msg("display update")
}

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

// TODO: move it elsewhere or get rid of it entirely
func (u *UserInterface) DrawWaitDisplay() {
	if int(u.mode) == int(ScreenModeWait) && !u.displayData.forceRefresh {
		return
	}

	_ = u.Screen.RenderSplashScreen(u.i18n.GetString("ui.waiting"))

	u.mode = ScreenModeWait

	u.log.Debug().Str("state", "wait").Msg("display update")
}

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

func (u *UserInterface) displayPowerOffTimeoutReached() bool {
	return time.Since(u.lastActivity) > 30*time.Second
}

func (u *UserInterface) displayInactiveTimeoutReached() bool {
	return time.Since(u.lastActivity) > 5*time.Second
}

func (u *UserInterface) displaySplashTimeoutReached() bool {
	return time.Since(u.lastActivity) > 2*time.Second
}

func (u *UserInterface) AlterSetting(menuPage string, action string) string {
	u.RegisterActivity()
	u.mode = ScreenModeSettings

	return u.alterSetting(menuPage, action)
}

// TODO: clean up this logic and make it easier to understand
func (u *UserInterface) UpdateDisplay(data LiveData) {
	switch u.mode {
	case ScreenModeSettings:
		if u.displayInactiveTimeoutReached() {
			if data.TelemetryActive {
				u.DrawLiveDisplay(data)
			} else {
				u.DrawWaitDisplay()
			}
		}
	case ScreenModeLive:
		if data.TelemetryActive {
			u.DrawLiveDisplay(data)
		} else {
			u.DrawWaitDisplay()
		}
	case ScreenModeStartup:
		if u.displaySplashTimeoutReached() {
			if data.TelemetryActive {
				u.DrawLiveDisplay(data)
			} else {
				u.DrawWaitDisplay()
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
