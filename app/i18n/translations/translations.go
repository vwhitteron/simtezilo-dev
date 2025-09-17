package translations

import (
	"fmt"
	"strings"
)

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
	RadioFuelRange         Key = "radio.fuelrange"
	RadioFuelPreWarn       Key = "radio.fuelprewarn"
	RadioBoxForFuel        Key = "radio.boxforfuel"
	RadioFuelCritical      Key = "radio.fuelcritical"
	RadioOutOfFuel         Key = "radio.outoffuel"
	RadioLapsRemaining     Key = "radio.lapsremaining"
	RadioLapsHalfway       Key = "radio.lapshalfway"
	RadioRaceFinish        Key = "radio.racefinish"
	RadioFinalLap          Key = "radio.finallap"
	RadioLapsWithRemaining Key = "radio.lapswithremaining"
)

func (tk Key) String() string {
	return string(tk)
}

func (tk Key) ToLower() Key {
	return Key(strings.ToLower(string(tk)))
}

func StringToKey(str string) (Key, error) {
	key := Key(str)

	if key == "" {
		return "", fmt.Errorf("invalid translation key: %q", str)
	}

	return key, nil
}
