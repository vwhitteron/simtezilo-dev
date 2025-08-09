package ui

type ScreenMode int

const (
	ScreenModeSleep ScreenMode = iota
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
