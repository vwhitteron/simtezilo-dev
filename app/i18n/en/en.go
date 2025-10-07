// Package en provides English translations and font settings.
package en

import "github.com/vwhitteron/simtezilo-dev/app/i18n/translations"

const (
	// Code is an IETF BCP47 language tag.
	Code = "en"

	// Name is the name of the language expressed in the target language.
	Name = "English"

	// RegularFont is the font face file for rendering regular text.
	RegularFont = "LeagueGothic-Regular.ttf"

	// RegularFontScale is the relative size of the regular font.
	RegularFontScale = 1.0

	// ItalicFont is the font face file for rendering italic text.
	ItalicFont = "LeagueGothic-Italic.ttf"

	// ItalicFontScale is the relative size of the italic font.
	ItalicFontScale = 1.0

	// ValueFont is the font face file for rendering values.
	ValueFont = "LeagueGothic-Regular.ttf"

	// ValueFontScale is the relative size of the value font.
	ValueFontScale = 1.0
)

// Translations maps translation keys to their corresponding English strings.
var Translations = map[translations.Key]string{ //nolint:gochecknoglobals
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

	translations.RadioOnline:           "Radio check",
	translations.RadioLapRecord:        "Lap record",
	translations.RadioFuelRangeFmt:     "Fuel range %d laps. %d laps remaining",
	translations.RadioFuelPreWarnFmt:   "Refuel in %d laps",
	translations.RadioBoxForFuel:       "Box this lap for fuel",
	translations.RadioFuelCritical:     "Fuel critical, map 6",
	translations.RadioFuelCriticalBox:  "Fuel critical, map 6 boxboxbox",
	translations.RadioOutOfFuelLastLap: "Out of fuel, switch to reserve and limp to finish",
	translations.RadioOutOfFuelBox:     "Out of fuel, switch to reserve and box immediately",
	translations.RadioLapsRemainingFmt: "%d laps remaining",
	translations.RadioFinalLap:         "Final lap",
	translations.RadioRaceProgressFmt:  "Race progress %d%%",
	translations.RadioRaceFinish:       "Race complete",
}
