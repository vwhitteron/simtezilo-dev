package app

import (
	"strconv"
)

func (a *App) settingAction(setting string, action string) string {
	handlers := map[string]func(string) string{
		"cVol":       a.handleChassisVolSetting,
		"ePVol":      a.handleEnginePulseVolSetting,
		"ePrimary":   a.handleEnginePrimarySetting,
		"ePScale":    a.handleEnginePulseScaleSetting,
		"eSecondary": a.handleEngineSecondarySetting,
		"eVol":       a.handleEngineVolSetting,
		"eq":         a.handleEqEnableSetting,
		"fCurve":     a.handleForceCurveSetting,
		"fMax":       a.handleForceMaxSetting,
		"fMin":       a.handleForceMinSetting,
		"fSat":       a.handleForceSatSetting,
		"lang":       a.handleLanguageSetting,
		"tCurve":     a.handleTransmissionCurveSetting,
		"tSat":       a.handleTransmissionSatSetting,
		"tVol":       a.handleTransmissionVolSetting,
		"vCurve":     a.handleVibrationCurveSetting,
		"vol":        a.handleMasterVolSetting,
		"vSat":       a.handleVibrationSatSetting,
		"record":     a.handleRecordToggle,
		"setupMode":  a.handleSetupModeCountdown,
		"info":       a.handleInfoScreen,
	}

	if handler, exists := handlers[setting]; exists {
		return handler(action)
	}

	return "error"
}

func (a *App) handleChassisVolSetting(action string) string {
	var value float64

	switch action {
	case "increase": //nolint:goconst // no value as a const
		value = a.config.IncreaseSynthChassisGain()
	case "decrease": //nolint:goconst // no value as a const
		value = a.config.DecreaseSynthChassisGain()
	default:
		value = a.config.GetSynthChassisGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEnginePulseVolSetting(action string) string {
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

func (a *App) handleEnginePrimarySetting(action string) string {
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

func (a *App) handleEngineSecondarySetting(action string) string {
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

func (a *App) handleEngineVolSetting(action string) string {
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

func (a *App) handleEqEnableSetting(action string) string {
	value := "OFF"

	switch action {
	case "increase":
		a.config.SetSynthEqEnabled(true)

		value = "ON"
	case "decrease":
		a.config.SetSynthEqEnabled(false)
	default:
		enabled := a.config.GetSynthEqEnabled()
		if enabled {
			value = "ON"
		}
	}

	return value
}

func (a *App) handleForceCurveSetting(action string) string {
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

func (a *App) handleForceMaxSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsPulseMaxHz()
	case "decrease":
		value = a.config.DecreasehapticsPulseMaxHz()
	default:
		value = int(a.config.GetHapticsPulseMaxHz())
	}

	return strconv.Itoa(value)
}

func (a *App) handleForceMinSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsPulseMinHz()
	case "decrease":
		value = a.config.DecreaseHapticsPulseMinHz()
	default:
		value = int(a.config.GetHapticsPulseMinHz())
	}

	return strconv.Itoa(value)
}

func (a *App) handleForceSatSetting(action string) string {
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

func (a *App) handleTransmissionSatSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseHapticsTransmissionGforceMax()
	case "decrease":
		value = a.config.DecreasehapticsTransmissionGforceMax()
	default:
		value = a.config.GetHapticsTransmissionGforceMax()
	}

	return strconv.FormatFloat(value, 'f', 1, 64)
}

func (a *App) handleTransmissionVolSetting(action string) string {
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

func (a *App) handleVibrationCurveSetting(action string) string {
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

func (a *App) handleMasterVolSetting(action string) string {
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

func (a *App) handleVibrationSatSetting(action string) string {
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

func (a *App) handleRecordToggle(action string) string {
	// Both increase and decrease actions toggle recording
	switch action {
	case "increase", "decrease":
		a.toggleRecording()
	}

	// Return current recording state
	if a.gtClient.IsRecording() {
		return "ON"
	}

	return "OFF"
}
