package app

import (
	"strconv"
	"strings"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
)

const (
	settingStateOn            = "on"
	settingStateOff           = "off"
	telemetrySourceModeAuto   = "auto"
	telemetrySourceModeDemo   = "demo"
	telemetrySourceModeCustom = "custom"
	telemetrySourceAuto       = "udp://255.255.255.255:33739"
	telemetrySourceDemo       = "file:///opt/simtezilo/data/replays/demo.gtz"
)

func (a *App) settingAction(setting languagedb.Key, action string) string {
	handlers := map[languagedb.Key]func(string) string{
		// App handlers
		languagedb.UIMenuAppLanguage:        a.handleLanguageSetting,
		languagedb.UIMenuAppLoglevel:        a.handleLogLevelSetting,
		languagedb.UIMenuAppDevtools:        a.handleDevToolsSetting,
		languagedb.UIMenuAppExperimental:    a.handleExperimentalSetting,
		languagedb.UIMenuAppTelemetrySource: a.handleTelemetrySourceSetting,

		// System handlers
		languagedb.UIMenuSystemSetupmode:          a.handleSetupModeCountdown,
		languagedb.UIMenuSystemDisplayOrientation: a.handleDisplayOrientationSetting,

		// Bluetooth handlers
		languagedb.UIMenuBluetoothDevice: a.bluetooth.HandleDeviceSetting,
		languagedb.UIMenuBluetoothToggle: a.bluetooth.HandleToggleSetting,

		// Synthesizer handlers
		languagedb.UIMenuSynthInternalSampleRate:        a.handleInternalSampleRateSetting,
		languagedb.UIMenuSynthMasterGain:                a.handleMasterGainSetting,
		languagedb.UIMenuSynthChassisGain:               a.handleChassisGainSetting,
		languagedb.UIMenuSynthEngineGain:                a.handleEngineGainSetting,
		languagedb.UIMenuSynthTransmissionGain:          a.handleTransmissionGainSetting,
		languagedb.UIMenuSynthTransmissionGainMinRace:   a.handleTransmissionGainMinRaceSetting,
		languagedb.UIMenuSynthTransmissionGainMinStreet: a.handleTransmissionGainMinStreetSetting,
		languagedb.UIMenuSynthDrx:                       a.handleDrxSetting,

		// Mute handlers
		languagedb.UIMenuSynthMuteMaster:       a.handleMasterMuteSetting,
		languagedb.UIMenuSynthMuteChassis:      a.handleChassisMuteSetting,
		languagedb.UIMenuSynthMuteEngine:       a.handleEngineMuteSetting,
		languagedb.UIMenuSynthMuteTransmission: a.handleTransmissionMuteSetting,

		// Calibration handlers
		languagedb.UIMenuSynthCalibrationEnable:     a.handleCalibrationEnableSetting,
		languagedb.UIMenuSynthCalibrationChannel:    a.handleCalibrationChannelSetting,
		languagedb.UIMenuSynthCalibrationFrequency:  a.handleCalibrationFrequencySetting,
		languagedb.UIMenuSynthCalibrationSweep:      a.handleCalibrationSweepSetting,
		languagedb.UIMenuSynthCalibrationSweepRange: a.handleCalibrationSweepRangeSetting,

		// Haptics handlers
		languagedb.UIMenuHapticsOutputMode:              a.handleOutputModeSetting,
		languagedb.UIMenuHapticsJerkCurve:               a.handleJerkCurveSetting,
		languagedb.UIMenuHapticsJerkMax:                 a.handleJerkMaxSetting,
		languagedb.UIMenuHapticsSnapCurve:               a.handleSnapCurveSetting,
		languagedb.UIMenuHapticsSnapMax:                 a.handleSnapMaxSetting,
		languagedb.UIMenuHapticsPulseMaxAmplitude:       a.handlePulseMaxAmplitudeSetting,
		languagedb.UIMenuHapticsPulseMinFreq:            a.handlePulseMinFreqSetting,
		languagedb.UIMenuHapticsPulseMaxFreq:            a.handlePulseMaxFreqSetting,
		languagedb.UIMenuHapticsTransmissionFFBStrength: a.handletransmissionFFBStrengthSetting,
		languagedb.UIMenuHapticsTransmissionCurve:       a.handleTransmissionCurveSetting,
		languagedb.UIMenuHapticsTransmissionGforceMax:   a.handleTransmissionGforceMaxSetting,
		languagedb.UIMenuHapticsEnginePrimaryBalance:    a.handleEnginePrimaryBalanceSetting,
		languagedb.UIMenuHapticsEngineSecondaryBalance:  a.handleEngineSecondaryBalanceSetting,
		languagedb.UIMenuHapticsEnginePulseGain:         a.handleEnginePulseGainSetting,
		languagedb.UIMenuHapticsEnginePulseScale:        a.handleEnginePulseScaleSetting,

		// Wind Simulator handlers
		languagedb.UIMenuFanEnable:          a.handleFanEnableSetting,
		languagedb.UIMenuFanMode:            a.handleFanModeSetting,
		languagedb.UIMenuFanWindSimMaxSpeed: a.handleFanMaxSpeedSetting,
		languagedb.UIMenuFanCommandTimeout:  a.handleFanCommandTimeoutSetting,

		// Pit Radio handlers
		languagedb.UIMenuPitRadioEnable:               a.handlePitRadioEnableSetting,
		languagedb.UIMenuPitRadioVolume:               a.handlePitradioVolumeSetting,
		languagedb.UIMenuPitRadioLapTimesEnable:       a.handlePitradioLapTimesEnableSetting,
		languagedb.UIMenuPitRadioLapTimesMaxDelta:     a.handlePitradioLapTimesMaxDeltaSetting,
		languagedb.UIMenuPitRadioRaceLapsEnable:       a.handlePitradioRaceLapsEnableSetting,
		languagedb.UIMenuPitRadioRaceLapsCountdown:    a.handlePitradioRaceLapsCountdownSetting,
		languagedb.UIMenuPitRadioRaceLapsInterval:     a.handlePitradioRaceLapsIntervalSetting,
		languagedb.UIMenuPitRadioRaceProgressEnable:   a.handlePitradioRaceProgressEnableSetting,
		languagedb.UIMenuPitRadioRaceProgressMinLaps:  a.handlePitradioRaceProgressMinLapsSetting,
		languagedb.UIMenuPitRadioRaceProgressInterval: a.handlePitradioRaceProgressIntervalSetting,
		languagedb.UIMenuPitRadioFuelEnable:           a.handlePitradioFuelEnableSetting,
		languagedb.UIMenuPitRadioFuelPreWarn:          a.handlePitradioFuelPreWarnSetting,
		languagedb.UIMenuPitRadioFuelStrategy:         a.handlePitradioFuelStrategySetting,
		languagedb.UIMenuPitRadioFuelSafetyLaps:       a.handlePitradioFuelSafetyLapsSetting,
		languagedb.UIMenuPitRadioFuelSafetyMetres:     a.handlePitradioFuelSafetyMetresSetting,
		languagedb.UIMenuPitRadioTyreEnable:           a.handlePitRadioTyreEnableSetting,
		languagedb.UIMenuPitRadioTyreTempOptimal:      a.handlePitradioTyreTempOptimalSetting,
		languagedb.UIMenuPitRadioTyreTempWindow:       a.handlePitradioTyreTempWindowSetting,
		languagedb.UIMenuPitRadioTyreTempMargin:       a.handlePitradioTyreTempMarginSetting,

		// Dev tool handlers
		languagedb.UIMenuDevtoolsRecord: a.handleRecordToggle,

		// Info handlers
		languagedb.UIMenuInfo:           a.handleInfoScreen,
		languagedb.UIMenuInfoVersion:    a.handleVersionInfo,
		languagedb.UIMenuInfoCommitHash: a.handleCommitHashInfo,
		languagedb.UIMenuInfoBuildTime:  a.handleBuildTimeInfo,
		languagedb.UIMenuInfoPlatform:   a.handlePlatformInfo,
		languagedb.UIMenuInfoIPAddress:  a.handleIPAddressInfo,
	}

	if handler, exists := handlers[setting]; exists {
		result := handler(action)

		// Save config to file after any setting change (except "get" actions)
		if action != "get" && action != "" {
			err := a.config.SaveConfigToFile()
			if err != nil {
				a.log.Error().Err(err).Msg("failed to save configuration to file")
			}
		}

		return result
	}

	// Dynamic routing leaf keys: "ui.menu.haptics.routing.<source>.ch<n>"
	const routingPrefix = "ui.menu.haptics.routing."
	if strings.HasPrefix(string(setting), routingPrefix) {
		result := a.handleRoutingLeafSetting(string(setting), action)

		if action != "get" && action != "" {
			err := a.config.SaveConfigToFile()
			if err != nil {
				a.log.Error().Err(err).Msg("failed to save configuration to file")
			}
		}

		return result
	}

	// Dynamic synth channel gain leaf keys: "ui.menu.synth.gain.ch<n>"
	const channelGainPrefix = "ui.menu.synth.gain.ch"
	if strings.HasPrefix(string(setting), channelGainPrefix) {
		result := a.handleChannelGainLeafSetting(string(setting), action)

		if action != "get" && action != "" {
			err := a.config.SaveConfigToFile()
			if err != nil {
				a.log.Error().Err(err).Msg("failed to save configuration to file")
			}
		}

		return result
	}

	// Dynamic synth channel mute leaf keys: "ui.menu.synth.mute.ch<n>"
	const channelMutePrefix = "ui.menu.synth.mute.ch"
	if strings.HasPrefix(string(setting), channelMutePrefix) {
		result := a.handleChannelMuteLeafSetting(string(setting), action)

		if action != "get" && action != "" {
			err := a.config.SaveConfigToFile()
			if err != nil {
				a.log.Error().Err(err).Msg("failed to save configuration to file")
			}
		}

		return result
	}

	// Dynamic synth channel EQ leaf keys: "ui.menu.synth.eq.ch<n>"
	const channelEqPrefix = "ui.menu.synth.eq.ch"
	if strings.HasPrefix(string(setting), channelEqPrefix) {
		result := a.handleChannelEqLeafSetting(string(setting), action)

		if action != "get" && action != "" {
			err := a.config.SaveConfigToFile()
			if err != nil {
				a.log.Error().Err(err).Msg("failed to save configuration to file")
			}
		}

		return result
	}

	return "error"
}

func (a *App) handleChassisGainSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseSynthChassisGain()
	case "decrease":
		value = a.config.DecreaseSynthChassisGain()
	default:
		value = a.config.GetSynthChassisGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEnginePulseGainSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsEnginePulseGain()
	case "decrease":
		value = a.config.DecreaseHapticsEnginePulseGain()
	default:
		value = a.config.GetHapticsEnginePulseGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEnginePrimaryBalanceSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsEnginePrimaryBalance()
	case "decrease":
		value = a.config.DecreaseHapticsEnginePrimaryBalance()
	default:
		value = a.config.GetHapticesEnginePrimaryBalance()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEnginePulseScaleSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsEnginePulseScale()
	case "decrease":
		value = a.config.DecreasehapticsEnginePulseScale()
	default:
		value = a.config.GetHapticsEnginePulseScale()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEngineSecondaryBalanceSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsEngineSecondaryBalance()
	case "decrease":
		value = a.config.DecreaseHapticsEngineSecondaryBalance()
	default:
		value = a.config.GetHapticsEngineSecondaryBalance()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEngineGainSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseSynthEngineGain()
	case "decrease":
		value = a.config.DecreaseSynthEngineGain()
	default:
		value = a.config.GetSynthEngineGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleDrxSetting(action string) string {
	enabled := a.config.GetSynthDRXEnabled()

	switch action {
	case "increase", "decrease":
		enabled = !enabled
		a.config.SetSynthDRXEnabled(enabled)
	}

	if enabled {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleLanguageSetting(action string) string {
	switch action {
	case "increase":
		return a.config.NextAppLanguage()
	case "decrease":
		return a.config.PreviousAppLanguage()
	default:
		return *a.config.GetAppLanguage()
	}
}

func (a *App) handleTransmissionCurveSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsTransmissionCurve()
	case "decrease":
		value = a.config.DecreaseHapticsTransmissionCurve()
	default:
		value = int(a.config.GetHapticsTransmissionCurve())
	}

	return strconv.Itoa(value)
}

func (a *App) handleTransmissionGainSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseSynthTransmissionGain()
	case "decrease":
		value = a.config.DecreaseSynthTransmissionGain()
	default:
		value = a.config.GetSynthTransmissionGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleMasterGainSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseSynthMasterGain()
	case "decrease":
		value = a.config.DecreaseSynthMasterGain()
	default:
		value = a.config.GetSynthMasterGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64) + " dB"
}

func (a *App) handleMasterMuteSetting(action string) string {
	muted := a.config.GetSynthMasterMute()

	switch action {
	case "increase", "decrease":
		muted = !muted
		a.config.SetSynthMasterMute(muted)
	}

	if muted {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleChassisMuteSetting(action string) string {
	muted := a.config.GetSynthChassisMute()

	switch action {
	case "increase", "decrease":
		muted = !muted
		a.config.SetSynthChassisMute(muted)
	}

	if muted {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleEngineMuteSetting(action string) string {
	muted := a.config.GetSynthEngineMute()

	switch action {
	case "increase", "decrease":
		muted = !muted
		a.config.SetSynthEngineMute(muted)
	}

	if muted {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleTransmissionMuteSetting(action string) string {
	muted := a.config.GetSynthTransmissionMute()

	switch action {
	case "increase", "decrease":
		muted = !muted
		a.config.SetSynthTransmissionMute(muted)
	}

	if muted {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleInfoScreen(action string) (value string) {
	switch action {
	case "increase":
		value = a.GetNextBuildInfoItem()
	case "decrease":
		value = a.GetPreviousBuildInfoItem()
	default:
		value = a.GetBuildInfoItem()
	}

	return value
}

func (a *App) handleSetupModeCountdown(action string) string {
	switch action {
	case "increase":
		value := a.ui.ResetSetupModeCountdown()

		return strconv.Itoa(value)
	case "decrease":
		value := a.ui.DecrementSetupModeCountdown()

		if value == 0 {
			a.log.Info().Msg("Setup mode countdown reached zero, triggering setup mode")
			a.switchToSetupMode()
		}

		return strconv.Itoa(value)
	default:
		countdown := a.ui.GetSetupModeCountdown()

		return strconv.Itoa(countdown)
	}
}

func (a *App) handleDisplayOrientationSetting(action string) string {
	orientations := []int{0, 90, 180, 270}
	current := a.config.GetDisplayOrientation()

	// Find current index
	currentIndex := 0

	for i, o := range orientations {
		if o == current {
			currentIndex = i

			break
		}
	}

	switch action {
	case "increase":
		currentIndex = (currentIndex + 1) % len(orientations)
		a.config.SetDisplayOrientation(orientations[currentIndex])
	case "decrease":
		currentIndex = (currentIndex - 1 + len(orientations)) % len(orientations)
		a.config.SetDisplayOrientation(orientations[currentIndex])
	}

	return strconv.Itoa(a.config.GetDisplayOrientation()) + "°"
}

func (a *App) handleRecordToggle(action string) string {
	// Both increase and decrease actions toggle recording
	switch action {
	case "increase", "decrease":
		a.toggleRecording()
	}

	// Return current recording state
	if a.gtClient.IsRecording() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleVersionInfo(_ string) string {
	return Version
}

func (a *App) handleCommitHashInfo(_ string) string {
	return CommitHash
}

func (a *App) handleBuildTimeInfo(_ string) string {
	return BuildTime
}

func (a *App) handlePlatformInfo(_ string) string {
	return Platform
}

func (a *App) handleIPAddressInfo(_ string) string {
	return a.ipAddress
}

func (a *App) handleLogLevelSetting(action string) string {
	levels := []string{"trace", "debug", "info", "warn", "error", "fatal"}
	currentLevel := a.config.GetAppLogLevel()

	// Find current index
	currentIndex := 0

	for i, level := range levels {
		if level == currentLevel {
			currentIndex = i

			break
		}
	}

	switch action {
	case "increase":
		// Move to previous level (less verbose)
		if currentIndex > 0 {
			currentIndex--
		}

		a.config.SetAppLogLevel(levels[currentIndex])
	case "decrease":
		// Move to next level (more verbose)
		if currentIndex < len(levels)-1 {
			currentIndex++
		}

		a.config.SetAppLogLevel(levels[currentIndex])
	}

	return a.config.GetAppLogLevel()
}

func (a *App) handleDevToolsSetting(action string) string {
	switch action {
	case "increase", "decrease":
		// Toggle dev tools
		current := a.config.GetDevToolsEnabled()
		a.config.SetDevToolsEnabled(!current)
	}

	if a.config.GetDevToolsEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleExperimentalSetting(action string) string {
	switch action {
	case "increase", "decrease":
		// Toggle experimental features
		current := a.config.GetExperimentalFeaturesEnabled()
		a.config.SetExperimentalFeaturesEnabled(!current)
	}

	if a.config.GetExperimentalFeaturesEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

// getTelemetrySourceMode returns the current telemetry source mode.
func (a *App) getTelemetrySourceMode() string {
	switch a.config.GetTelemetrySource() {
	case telemetrySourceAuto:
		return telemetrySourceModeAuto
	case telemetrySourceDemo:
		return telemetrySourceModeDemo
	default:
		return telemetrySourceModeCustom
	}
}

// hasCustomTelemetrySource returns true if a custom telemetry source is stored.
func (a *App) hasCustomTelemetrySource() bool {
	return a.customTelemetrySource != "" &&
		a.customTelemetrySource != telemetrySourceAuto &&
		a.customTelemetrySource != telemetrySourceDemo
}

// setTelemetrySourceWithMode sets the telemetry source and returns the new mode.
func (a *App) setTelemetrySourceWithMode(source, mode string) string {
	a.config.SetTelemetrySource(source)

	return mode
}

func (a *App) handleTelemetrySourceSetting(action string) string {
	current := a.config.GetTelemetrySource()
	currentMode := a.getTelemetrySourceMode()

	// Define mode transitions: [currentMode] -> [nextMode on increase, nextMode on decrease]
	switch action {
	case "increase":
		return a.cycleTelemetrySourceForward(current, currentMode)
	case "decrease":
		return a.cycleTelemetrySourceBackward(current, currentMode)
	}

	return currentMode
}

func (a *App) cycleTelemetrySourceForward(current, currentMode string) string {
	// Cycle: AUTO -> DEMO -> CUSTOM -> AUTO
	switch currentMode {
	case telemetrySourceModeAuto:
		return a.setTelemetrySourceWithMode(telemetrySourceDemo, telemetrySourceModeDemo)
	case telemetrySourceModeDemo:
		if a.hasCustomTelemetrySource() {
			return a.setTelemetrySourceWithMode(a.customTelemetrySource, telemetrySourceModeCustom)
		}

		return a.setTelemetrySourceWithMode(telemetrySourceAuto, telemetrySourceModeAuto)
	case telemetrySourceModeCustom:
		a.customTelemetrySource = current

		return a.setTelemetrySourceWithMode(telemetrySourceAuto, telemetrySourceModeAuto)
	}

	return currentMode
}

func (a *App) cycleTelemetrySourceBackward(current, currentMode string) string {
	// Cycle: AUTO -> CUSTOM -> DEMO -> AUTO
	switch currentMode {
	case telemetrySourceModeAuto:
		if a.hasCustomTelemetrySource() {
			return a.setTelemetrySourceWithMode(a.customTelemetrySource, telemetrySourceModeCustom)
		}

		return a.setTelemetrySourceWithMode(telemetrySourceDemo, telemetrySourceModeDemo)
	case telemetrySourceModeDemo:
		return a.setTelemetrySourceWithMode(telemetrySourceAuto, telemetrySourceModeAuto)
	case telemetrySourceModeCustom:
		a.customTelemetrySource = current

		return a.setTelemetrySourceWithMode(telemetrySourceDemo, telemetrySourceModeDemo)
	}

	return currentMode
}

// Fan handlers.

func (a *App) handleFanEnableSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.FanEnabled()
		a.config.SetFanEnabled(!current)
	}

	if a.config.FanEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleFanModeSetting(action string) string {
	var mode string

	switch action {
	case "increase":
		mode = a.config.CycleFanMode(true)
	case "decrease":
		mode = a.config.CycleFanMode(false)
	default:
		mode = a.config.GetFanMode()
	}

	return a.fanModeDisplay(mode)
}

func (a *App) fanModeDisplay(mode string) string {
	switch mode {
	case "open":
		return a.i18n.GetString(languagedb.UIMenuFanModeOpenCockpit)
	case "all":
		return a.i18n.GetString(languagedb.UIMenuFanModeAll)
	default:
		return a.i18n.GetString(languagedb.UIMenuFanModeManual)
	}
}

func (a *App) handleFanMaxSpeedSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseFanMaxSpeedKPH()
	case "decrease":
		value = a.config.DecreaseFanMaxSpeedKPH()
	default:
		value = a.config.GetFanMaxSpeedKPH()
	}

	return strconv.Itoa(value) + "kph"
}

func (a *App) handleFanCommandTimeoutSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseFanCommandTimeoutMs()
	case "decrease":
		value = a.config.DecreaseFanCommandTimeoutMs()
	default:
		value = a.config.GetFanCommandTimeoutMs()
	}

	return strconv.Itoa(value) + "ms"
}

func (a *App) handlePitRadioEnableSetting(action string) string {
	switch action {
	case "increase", "decrease":
		// Toggle pit radio
		current := a.config.PitRadioEnabled()
		a.config.SetPitRadioEnabled(!current)
	}

	if a.config.PitRadioEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handlePitradioVolumeSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseAudioPitRadioVolume()
	case "decrease":
		value = a.config.DecreaseAudioPitRadioVolume()
	default:
		value = a.config.GetAudioPitRadioVolume()
	}

	return strconv.Itoa(value) + "%"
}

// Pit Radio - Lap Times notification handlers.
func (a *App) handlePitradioLapTimesEnableSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.GetPitRadioNotifyLapTimesEnabled()
		a.config.SetPitRadioNotifyLapTimesEnabled(!current)
	}

	if a.config.GetPitRadioNotifyLapTimesEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handlePitradioLapTimesMaxDeltaSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioNotifyLapTimesMaxDeltaSeconds()
	case "decrease":
		value = a.config.DecreasePitRadioNotifyLapTimesMaxDeltaSeconds()
	default:
		value = a.config.GetPitRadioNotifyLapTimesMaxDeltaSeconds()
	}

	return strconv.FormatFloat(value, 'f', 1, 64) + "s"
}

// Pit Radio - Race Laps notification handlers.
func (a *App) handlePitradioRaceLapsEnableSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.GetPitRadioNotifyRaceLapsEnabled()
		a.config.SetPitRadioNotifyRaceLapsEnabled(!current)
	}

	if a.config.GetPitRadioNotifyRaceLapsEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handlePitradioRaceLapsCountdownSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioNotifyRaceLapsCountdownLaps()
	case "decrease":
		value = a.config.DecreasePitRadioNotifyRaceLapsCountdownLaps()
	default:
		value = a.config.GetPitRadioNotifyRaceLapsCountdownLaps()
	}

	return strconv.Itoa(value)
}

func (a *App) handlePitradioRaceLapsIntervalSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioNotifyRaceLapsIntervalLaps()
	case "decrease":
		value = a.config.DecreasePitRadioNotifyRaceLapsIntervalLaps()
	default:
		value = a.config.GetPitRadioNotifyRaceLapsIntervalLaps()
	}

	return strconv.Itoa(value)
}

// Pit Radio - Race Progress notification handlers.
func (a *App) handlePitradioRaceProgressEnableSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.GetPitRadioNotifyRaceProgressEnabled()
		a.config.SetPitRadioNotifyRaceProgressEnabled(!current)
	}

	if a.config.GetPitRadioNotifyRaceProgressEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handlePitradioRaceProgressMinLapsSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioNotifyRaceProgressMinLaps()
	case "decrease":
		value = a.config.DecreasePitRadioNotifyRaceProgressMinLaps()
	default:
		value = a.config.GetPitRadioNotifyRaceProgressMinLaps()
	}

	return strconv.Itoa(value)
}

func (a *App) handlePitradioRaceProgressIntervalSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioNotifyRaceProgressIntervalPc()
	case "decrease":
		value = a.config.DecreasePitRadioNotifyRaceProgressIntervalPc()
	default:
		value = a.config.GetPitRadioNotifyRaceProgressIntervalPc()
	}

	return strconv.Itoa(value) + "%"
}

// Pit Radio - Fuel Management handlers.
func (a *App) handlePitradioFuelEnableSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.GetPitRadioFuelMonitoringEnabled()
		a.config.SetPitRadioFuelMonitoringEnabled(!current)
	}

	if a.config.GetPitRadioFuelMonitoringEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handlePitradioFuelPreWarnSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioFuelPreWarnNotifyLaps()
	case "decrease":
		value = a.config.DecreasePitRadioFuelPreWarnNotifyLaps()
	default:
		value = a.config.GetPitRadioFuelPreWarnNotifyLaps()
	}

	return strconv.FormatFloat(value, 'f', 1, 64)
}

func (a *App) handlePitradioFuelStrategySetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioFuelStrategyNotifyLaps()
	case "decrease":
		value = a.config.DecreasePitRadioFuelStrategyNotifyLaps()
	default:
		value = a.config.GetPitRadioFuelStrategyNotifyLaps()
	}

	return strconv.FormatFloat(value, 'f', 1, 64)
}

func (a *App) handlePitradioFuelSafetyLapsSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioFuelRangeSafetyMarginLaps()
	case "decrease":
		value = a.config.DecreasePitRadioFuelRangeSafetyMarginLaps()
	default:
		value = a.config.GetPitRadioFuelRangeSafetyMarginLaps()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handlePitradioFuelSafetyMetresSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioFuelRangeSafetyMarginMetres()
	case "decrease":
		value = a.config.DecreasePitRadioFuelRangeSafetyMarginMetres()
	default:
		value = a.config.GetPitRadioFuelRangeSafetyMarginMetres()
	}

	return strconv.FormatFloat(value, 'f', 0, 64) + "m"
}

// Pit Radio - Tyre Management handlers.
func (a *App) handlePitRadioTyreEnableSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.GetPitRadioTyreMonitoringEnabled()
		a.config.SetPitRadioTyreMonitoringEnabled(!current)
	}

	if a.config.GetPitRadioTyreMonitoringEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handlePitradioTyreTempOptimalSetting(action string) string {
	var value float32

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioTyreTemperatureOptimalCelsius()
	case "decrease":
		value = a.config.DecreasePitRadioTyreTemperatureOptimalCelsius()
	default:
		value = a.config.GetPitRadioTyreTemperatureOptimalCelsius()
	}

	return strconv.FormatFloat(float64(value), 'f', 0, 64) + "°C"
}

func (a *App) handlePitradioTyreTempWindowSetting(action string) string {
	var value float32

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioTyreTemperatureOperatingWindow()
	case "decrease":
		value = a.config.DecreasePitRadioTyreTemperatureOperatingWindow()
	default:
		value = a.config.GetPitRadioTyreTemperatureOperatingWindow()
	}

	return strconv.FormatFloat(float64(value), 'f', 1, 64) + "°C"
}

func (a *App) handlePitradioTyreTempMarginSetting(action string) string {
	var value float32

	switch action {
	case "increase":
		value = a.config.IncreasePitRadioTyreTemperatureMarginCelsius()
	case "decrease":
		value = a.config.DecreasePitRadioTyreTemperatureMarginCelsius()
	default:
		value = a.config.GetPitRadioTyreTemperatureMarginCelsius()
	}

	return strconv.FormatFloat(float64(value), 'f', 1, 64) + "°C"
}

// Synthesizer - Sample Rate handlers.
func (a *App) handleInternalSampleRateSetting(action string) string {
	rates := []int{8000, 16000, 22050, 32000, 44100, 48000}
	current := a.config.GetSynthInternalSampleRateHz()

	if action == "increase" || action == "decrease" {
		newRate := cycleSampleRate(action, current, rates)
		a.config.SetSynthInternalSampleRateHz(newRate)
	}

	return formatSampleRate(a.config.GetSynthInternalSampleRateHz())
}

// Synthesizer - Transmission Min Gain handlers.
func (a *App) handleTransmissionGainMinRaceSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseSynthTransmissionGainMinRace()
	case "decrease":
		value = a.config.DecreaseSynthTransmissionGainMinRace()
	default:
		value = a.config.GetSynthTransmissionGainMinRace()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleTransmissionGainMinStreetSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseSynthTransmissionGainMinStreet()
	case "decrease":
		value = a.config.DecreaseSynthTransmissionGainMinStreet()
	default:
		value = a.config.GetSynthTransmissionGainMinStreet()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

// Haptics - Output Mode handler.
func (a *App) handleOutputModeSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.GetHapticsReplayEnabled()
		a.config.SetHapticsEnableReplay(!current)
	}

	if a.config.GetHapticsReplayEnabled() {
		return "Live+Replay"
	}

	return "Live"
}

// Haptics - Chassis Feedback handlers.
func (a *App) handleJerkCurveSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsJerkCurve()
	case "decrease":
		value = a.config.DecreaseHapticsJerkCurve()
	default:
		value = int(a.config.GethapticsJerkCurve())
	}

	return strconv.Itoa(value)
}

func (a *App) handleJerkMaxSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsJerkMax()
	case "decrease":
		value = a.config.DecreaseHapticsJerkMax()
	default:
		value = a.config.GetHapticsJerkMax()
	}

	return strconv.Itoa(value)
}

func (a *App) handleSnapCurveSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsSnapCurve()
	case "decrease":
		value = a.config.DecreaseHapticsSnapCurve()
	default:
		value = int(a.config.GetHapticsSnapCurve())
	}

	return strconv.Itoa(value)
}

func (a *App) handleSnapMaxSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsSnapMax()
	case "decrease":
		value = a.config.DecreaseHapticsSnapMax()
	default:
		value = a.config.GetHapticsSnapMax()
	}

	return strconv.Itoa(value)
}

func (a *App) handlePulseMaxAmplitudeSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsPulseMaxAmplitude()
	case "decrease":
		value = a.config.DecreaseHapticsPulseMaxAmplitude()
	default:
		value = a.config.GetHapticsPulseMaxAmplitude()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handlePulseMinFreqSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsPulseMinHz()
	case "decrease":
		value = a.config.DecreaseHapticsPulseMinHz()
	default:
		value = int(a.config.GetHapticsPulseMinHz())
	}

	return strconv.Itoa(value) + "Hz"
}

func (a *App) handlePulseMaxFreqSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsPulseMaxHz()
	case "decrease":
		value = a.config.DecreaseHapticsPulseMaxHz()
	default:
		value = int(a.config.GetHapticsPulseMaxHz())
	}

	return strconv.Itoa(value) + "Hz"
}

// Haptics - Transmission Feedback handlers.
func (a *App) handletransmissionFFBStrengthSetting(action string) string {
	switch action {
	case "increase", "decrease":
		current := a.config.GethapticsDynamicTransFeedbackEnabled()
		a.config.SetHapticsDynamicTransFeedbackEnabled(!current)
	}

	if a.config.GethapticsDynamicTransFeedbackEnabled() {
		return "Dynamic"
	}

	return "Fixed"
}

func (a *App) handleTransmissionGforceMaxSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsTransmissionGforceMax()
	case "decrease":
		value = a.config.DecreasehapticsTransmissionGforceMax()
	default:
		value = a.config.GetHapticsTransmissionGforceMax()
	}

	return strconv.FormatFloat(value, 'f', 1, 64) + "G"
}

// handleRoutingLeafSetting parses a dynamic routing leaf key of the form
// "ui.menu.haptics.routing.<source>.ch<n>", toggles the route on increase or
// decrease (mirroring other boolean settings), and returns "on"/"off".
func (a *App) handleRoutingLeafSetting(key, action string) string {
	// key format: "ui.menu.haptics.routing.<source>.ch<n>"
	const prefix = "ui.menu.haptics.routing."

	rest := strings.TrimPrefix(key, prefix)
	// rest is now "<source>.ch<n>"
	dotIdx := strings.LastIndex(rest, ".ch")
	if dotIdx < 0 {
		return "error"
	}

	source := rest[:dotIdx]
	chStr := rest[dotIdx+3:] // skip ".ch"

	ch, err := strconv.Atoi(chStr)
	if err != nil || ch < 0 {
		return "error"
	}

	enabled := a.config.GetSynthRouteEnabled(source, ch)

	switch action {
	case "increase", "decrease":
		enabled = !enabled
		a.config.SetSynthRoute(source, ch, enabled)
	}

	if enabled {
		return settingStateOn
	}

	return settingStateOff
}

// handleChannelGainLeafSetting parses a dynamic synth channel gain leaf key of
// the form "ui.menu.synth.gain.ch<n>" and adjusts the gain for that channel.
func (a *App) handleChannelGainLeafSetting(key, action string) string {
	// key format: "ui.menu.synth.gain.ch<n>"
	const prefix = "ui.menu.synth.gain.ch"

	chStr := strings.TrimPrefix(key, prefix)

	ch, err := strconv.Atoi(chStr)
	if err != nil || ch < 0 {
		return "error"
	}

	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseSynthChannelGain(ch)
	case "decrease":
		value = a.config.DecreaseSynthChannelGain(ch)
	default:
		value = a.config.GetSynthChannelGain(ch)
	}

	return strconv.FormatFloat(value, 'f', 2, 64) + " dB"
}

// handleChannelMuteLeafSetting parses a dynamic synth channel mute leaf key of
// the form "ui.menu.synth.mute.ch<n>", toggles the mute for that channel on
// increase or decrease, and returns "on"/"off".
func (a *App) handleChannelMuteLeafSetting(key, action string) string {
	// key format: "ui.menu.synth.mute.ch<n>"
	const prefix = "ui.menu.synth.mute.ch"

	chStr := strings.TrimPrefix(key, prefix)

	ch, err := strconv.Atoi(chStr)
	if err != nil || ch < 0 {
		return "error"
	}

	muted := a.config.GetSynthChannelMute(ch)

	switch action {
	case "increase", "decrease":
		muted = !muted
		a.config.SetSynthChannelMute(ch, muted)
	}

	if muted {
		return settingStateOn
	}

	return settingStateOff
}

// handleChannelEqLeafSetting parses a dynamic synth channel EQ leaf key of the
// form "ui.menu.synth.eq.ch<n>", toggles EQ enablement for that channel on
// increase or decrease, and returns "on"/"off".
func (a *App) handleChannelEqLeafSetting(key, action string) string {
	// key format: "ui.menu.synth.eq.ch<n>"
	const prefix = "ui.menu.synth.eq.ch"

	chStr := strings.TrimPrefix(key, prefix)

	ch, err := strconv.Atoi(chStr)
	if err != nil || ch < 0 {
		return "error"
	}

	enabled := a.config.GetSynthChannelEqEnabled(ch)

	switch action {
	case "increase", "decrease":
		enabled = !enabled
		a.config.SetSynthChannelEqEnabled(ch, enabled)
	}

	if enabled {
		return settingStateOn
	}

	return settingStateOff
}

// cycleSampleRate handles cycling through sample rates based on the action.
// Returns the new rate after the action is applied.
func cycleSampleRate(action string, current int, rates []int) int {
	var newRate int

	switch action {
	case "increase":
		for _, rate := range rates {
			if rate > current {
				newRate = rate

				break
			}
		}

		if newRate == 0 {
			newRate = rates[len(rates)-1]
		}
	case "decrease":
		for i := len(rates) - 1; i >= 0; i-- {
			if rates[i] < current {
				newRate = rates[i]

				break
			}
		}

		if newRate == 0 {
			newRate = rates[0]
		}
	default:
		newRate = current
	}

	return newRate
}

// formatSampleRate formats a sample rate in Hz to a human-readable string.
func formatSampleRate(hz int) string {
	if hz >= 1000 {
		return strconv.Itoa(hz/1000) + "kHz"
	}

	return strconv.Itoa(hz) + "Hz"
}

// Calibration handlers.
func (a *App) handleCalibrationEnableSetting(action string) string {
	switch action {
	case "increase":
		a.calibrator.SetEnabled(true)
	case "decrease":
		a.calibrator.SetEnabled(false)
	default:
		// Just return current state
	}

	if a.calibrator.IsEnabled() {
		return settingStateOn
	}

	return settingStateOff
}

// handleCalibrationChannelSetting cycles the calibration tone's target output
// channel through the sequence All (-1), Ch0, Ch1, ..., Ch(N-1), where N is
// the configured haptic output channel count.
func (a *App) handleCalibrationChannelSetting(action string) string {
	channelCount := a.config.GetAudioHapticsChannels()

	// Sequence indices 0..channelCount map to target channels -1 (all), 0, 1, ...
	current := a.calibrator.GetTargetChannel()
	currentIdx := current + 1

	switch action {
	case "increase":
		currentIdx = (currentIdx + 1) % (channelCount + 1)
		a.calibrator.SetTargetChannel(currentIdx - 1)
	case "decrease":
		currentIdx = (currentIdx - 1 + channelCount + 1) % (channelCount + 1)
		a.calibrator.SetTargetChannel(currentIdx - 1)
	}

	target := a.calibrator.GetTargetChannel()
	if target < 0 {
		return "All"
	}

	return "Ch" + strconv.Itoa(target)
}

func (a *App) handleCalibrationFrequencySetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.calibrator.IncreaseFrequency()
	case "decrease":
		value = a.calibrator.DecreaseFrequency()
	default:
		value = a.calibrator.GetFrequency()
	}

	return strconv.FormatFloat(value, 'f', 0, 64) + "Hz"
}

func (a *App) handleCalibrationSweepSetting(action string) string {
	switch action {
	case "increase":
		a.calibrator.StartSweep()
	case "decrease":
		a.calibrator.StopSweep()
	default:
		// Just return current state
	}

	if a.calibrator.IsSweeping() {
		return settingStateOn
	}

	return settingStateOff
}

func (a *App) handleCalibrationSweepRangeSetting(action string) string {
	switch action {
	case "increase", "decrease":
		// Toggle between haptic and full range
		mode := a.calibrator.ToggleSweepRangeMode()

		return string(mode)
	default:
		return string(a.calibrator.GetSweepRangeMode())
	}
}
