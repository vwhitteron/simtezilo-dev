package en

import "github.com/vwhitteron/simtezilo-dev/app/i18n/translations"

const (
	Code             = "en"
	Name             = "English"
	RegularFont      = "LeagueGothic-Regular.ttf"
	RegularFontScale = 1.0
	ItalicFont       = "LeagueGothic-Italic.ttf"
	ItalicFontScale  = 1.0
	ValueFont        = "LeagueGothic-Regular.ttf"
	ValueFontScale   = 1.0
)

var Translations = map[translations.Key]string{
	translations.AppName:        "Simtezilo",
	translations.AppDescription: "Sim Racing Haptics Synthesizer",
	translations.AppVersion:     "version",

	translations.UIError:    "error",
	translations.UISuccess:  "success",
	translations.UIQuit:     "goodbye",
	translations.UIStarting: "starting",
	translations.UIStopping: "stopping",
	translations.UILoading:  "loading",
	translations.UIWaiting:  "waiting",
	translations.UIReady:    "ready",
	translations.UISettings: "settings",

	translations.UIMenuVol:        "Master Gain",
	translations.UIMenuCVol:       "Chassis Gain",
	translations.UIMenuTVol:       "Trans Gain",
	translations.UIMenuEVol:       "Engine Gain",
	translations.UIMenuEPrimary:   "Engine Bal 1",
	translations.UIMenuESecondary: "Engine Bal 2",
	translations.UIMenuEPVol:      "Engine Pul Gain",
	translations.UIMenuEPScale:    "Engine Pul Scale",
	translations.UIMenuVCurve:     "FFB Curve",
	translations.UIMenuVSat:       "FFB Saturation",
	translations.UIMenuFCurve:     "Freq Curve",
	translations.UIMenuFSat:       "Freq Saturation",
	translations.UIMenuFMin:       "Freq Min",
	translations.UIMenuFMax:       "Freq Max",
	translations.UIMenuTCurve:     "Trans Curve",
	translations.UIMenuTSat:       "Trans Saturation",
	translations.UIMenuMix:        "Mix Algo",
	translations.UIMenuLang:       "Language",

	translations.RadioOnline:            "Radio check",
	translations.RadioLapRecord:         "Lap record",
	translations.RadioFuelRange:         "Fuel range %d laps. %d laps remaining",
	translations.RadioFuelPreWarn:       "Refuel in %d laps",
	translations.RadioBoxForFuel:        "Box this lap for fuel",
	translations.RadioFuelCritical:      "Fuel critical, map 5 boxboxbox",
	translations.RadioOutOfFuel:         "Out of fuel, switch to reserve and box immediately",
	translations.RadioLapsRemaining:     "%d laps remaining",
	translations.RadioLapsWithRemaining: "Lap %d, %d laps remaining",
	translations.RadioLapsHalfway:       "Lap %d, halfway there",
	translations.RadioFinalLap:          "Final lap",
	translations.RadioRaceFinish:        "Race complete",
}
