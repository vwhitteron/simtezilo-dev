package languagedb

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

	RadioOnline            Key = "radio.online"
	RadioLapRecord         Key = "radio.laprecord"
	RadioFuelRangeFmt      Key = "radio.fuelrangefmt"
	RadioFuelPreWarnFmt    Key = "radio.fuelprewarnfmt"
	RadioBoxThisLap        Key = "radio.boxthislap"
	RadioFuelCritical      Key = "radio.fuelcritical"
	RadioFuelCriticalBox   Key = "radio.fuelcriticalbox"
	RadioOutOfFuelLastLap  Key = "radio.outoffuellastlap"
	RadioOutOfFuelBox      Key = "radio.outoffuel"
	RadioLapsRemainingFmt  Key = "radio.lapsremainingfmt"
	RadioRaceProgressFmt   Key = "radio.raceprogressfmt"
	RadioRaceFinish        Key = "radio.racefinish"
	RadioFinalLap          Key = "radio.finallap"
	RadioCircuitUpdatedFmt Key = "radio.circuitupdatedfmt"
	RadioTyresUnderTemp    Key = "radio.tyresundertemp"
	RadioTyresOptimalTemp  Key = "radio.tyresoptimaltemp"
	RadioTyresOverTemp     Key = "radio.tyresovertemp"
	RadioFront             Key = "radio.front"
	RadioRear              Key = "radio.rear"
	RadioFrontLeft         Key = "radio.frontleft"
	RadioFrontRight        Key = "radio.frontright"
	RadioRearLeft          Key = "radio.rearleft"
	RadioRearRight         Key = "radio.rearright"
)

// String returns the string representation of the translation key.
func (k Key) String() string {
	return string(k)
}

// ToLower returns a new translation key with all characters converted to lowercase.
func (k Key) ToLower() Key {
	return Key(strings.ToLower(string(k)))
}

// StringToKey converts a string to a Key type.
// It returns an error if the string does not correspond to a valid Key.
func StringToKey(str string) (Key, error) {
	key := Key(str)

	if key == "" {
		return "", fmt.Errorf("invalid translation key: %q", str)
	}

	return key, nil
}
