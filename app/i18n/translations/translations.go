// Package translations provides type-safe translation keys and methods for internationalization.
package translations

import (
	"fmt"
	"strings"
)

// Key provides a type-safe way to reference translation keys.
type Key string

const (
	AppName        Key = "app.name"
	AppDescription Key = "app.description"
	AppVersion     Key = "app.version"

	UIError    Key = "ui.error"
	UISuccess  Key = "ui.success"
	UIQuit     Key = "ui.quit"
	UIStarting Key = "ui.starting"
	UIStopping Key = "ui.stopping"
	UILoading  Key = "ui.loading"
	UIWaiting  Key = "ui.waiting"
	UIReady    Key = "ui.ready"
	UISettings Key = "ui.settings"

	UIMenuVol        Key = "ui.menu.vol"
	UIMenuCVol       Key = "ui.menu.cvol"
	UIMenuTVol       Key = "ui.menu.tvol"
	UIMenuEVol       Key = "ui.menu.evol"
	UIMenuEPrimary   Key = "ui.menu.eprimary"
	UIMenuESecondary Key = "ui.menu.esecondary"
	UIMenuEPVol      Key = "ui.menu.epvol"
	UIMenuEPScale    Key = "ui.menu.epscale"
	UIMenuVCurve     Key = "ui.menu.vcurve"
	UIMenuVSat       Key = "ui.menu.vsat"
	UIMenuFCurve     Key = "ui.menu.fcurve"
	UIMenuFSat       Key = "ui.menu.fsat"
	UIMenuFMin       Key = "ui.menu.fmin"
	UIMenuFMax       Key = "ui.menu.fmax"
	UIMenuTCurve     Key = "ui.menu.tcurve"
	UIMenuTSat       Key = "ui.menu.tsat"
	UIMenuMix        Key = "ui.menu.mix"
	UIMenuLang       Key = "ui.menu.lang"

	RadioOnline           Key = "radio.online"
	RadioLapRecord        Key = "radio.laprecord"
	RadioFuelRangeFmt     Key = "radio.fuelrangefmt"
	RadioFuelPreWarnFmt   Key = "radio.fuelprewarnfmt"
	RadioBoxForFuel       Key = "radio.boxforfuel"
	RadioFuelCritical     Key = "radio.fuelcriticallastlap"
	RadioFuelCriticalBox  Key = "radio.fuelcritical"
	RadioOutOfFuelLastLap Key = "radio.outoffuellastlap"
	RadioOutOfFuelBox     Key = "radio.outoffuel"
	RadioLapsRemainingFmt Key = "radio.lapsremainingfmt"
	RadioRaceProgressFmt  Key = "radio.racerogessfmt"
	RadioRaceFinish       Key = "radio.racefinish"
	RadioFinalLap         Key = "radio.finallap"
)

// String returns the string representation of the translation key.
func (tk Key) String() string {
	return string(tk)
}

// ToLower returns a new Key with all characters converted to lowercase.
func (tk Key) ToLower() Key {
	return Key(strings.ToLower(string(tk)))
}

// StringToKey converts a string to a Key, returning an error if the string is empty.
func StringToKey(str string) (Key, error) {
	key := Key(str)

	if key == "" {
		return "", fmt.Errorf("invalid translation key: %q", str)
	}

	return key, nil
}
