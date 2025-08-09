package ui

type ScreenMode int

const (
	ScreenModeOff ScreenMode = iota
	ScreenModeSleep
	ScreenModeWait
	ScreenModeStartup
	ScreenModeLive
	ScreenModeSettings
)

type Screen interface {
	SetMode(ScreenMode)
	GetMode() ScreenMode
	RenderSplashScreen(string)
	RenderErrorScreen(string)
	RenderLiveScreen(int)
	RenderSettingScreen(string, string)
}
