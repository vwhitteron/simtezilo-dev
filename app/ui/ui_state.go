package ui

// ScreenMode provides a limited set of modes for the screen.
type ScreenMode int

const (
	ScreenModeOff ScreenMode = iota
	ScreenModeSleep
	ScreenModeWait
	ScreenModeStartup
	ScreenModeLive
	ScreenModeSettings
)

// String returns a human-readable name for the mode, used in log output.
func (m ScreenMode) String() string {
	switch m {
	case ScreenModeOff:
		return "off"
	case ScreenModeSleep:
		return "sleep"
	case ScreenModeWait:
		return "wait"
	case ScreenModeStartup:
		return "startup"
	case ScreenModeLive:
		return "live"
	case ScreenModeSettings:
		return "settings"
	default:
		return "unknown"
	}
}
