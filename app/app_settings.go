package app

import "strconv"

func (a *App) settingAction(setting string, action string) string {
	handlers := map[string]func(string) string{
		"cVol":       a.handleChassisVolSetting,
		"ePVol":      a.handleEnginePulseVolSetting,
		"ePrimary":   a.handleEnginePrimarySetting,
		"ePScale":    a.handleEnginePulseScaleSetting,
		"eSecondary": a.handleEngineSecondarySetting,
		"eVol":       a.handleEngineVolSetting,
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
		value = a.config.IncreaseChassisGain()
	case "decrease": //nolint:goconst // no value as a const
		value = a.config.DecreaseChassisGain()
	default:
		value = a.config.GetChassisGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEnginePulseVolSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseEnginePulseGain()
	case "decrease":
		value = a.config.DecreaseEnginePulseGain()
	default:
		value = a.config.GetEnginePulseGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEnginePrimarySetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseEnginePrimaryBalance()
	case "decrease":
		value = a.config.DecreaseEnginePrimaryBalance()
	default:
		value = a.config.GetEnginePrimaryBalance()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEnginePulseScaleSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseEnginePulseScale()
	case "decrease":
		value = a.config.DecreaseEnginePulseScale()
	default:
		value = a.config.GetEnginePulseScale()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEngineSecondarySetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseEngineSecondaryBalance()
	case "decrease":
		value = a.config.DecreaseEngineSecondaryBalance()
	default:
		value = a.config.GetEngineSecondaryBalance()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleEngineVolSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseEngineGain()
	case "decrease":
		value = a.config.DecreaseEngineGain()
	default:
		value = a.config.GetEngineGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleForceCurveSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseSnapCurve()
	case "decrease":
		value = a.config.DecreaseSnapCurve()
	default:
		value = int(a.config.GetSnapCurve() * 1000)
	}

	return strconv.Itoa(value)
}

func (a *App) handleForceMaxSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseMaxHz()
	case "decrease":
		value = a.config.DecreaseMaxHz()
	default:
		value = int(a.config.GetMaxHz())
	}

	return strconv.Itoa(value)
}

func (a *App) handleForceMinSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseMinHz()
	case "decrease":
		value = a.config.DecreaseMinHz()
	default:
		value = int(a.config.GetMinHz())
	}

	return strconv.Itoa(value)
}

func (a *App) handleForceSatSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseSnapMax()
	case "decrease":
		value = a.config.DecreaseSnapMax()
	default:
		value = a.config.GetSnapMax()
	}

	return strconv.Itoa(value)
}

func (a *App) handleLanguageSetting(action string) string {
	switch action {
	case "increase":
		return a.config.NextLanguage()
	case "decrease":
		return a.config.PreviousLanguage()
	default:
		return *a.config.GetAppLanguage()
	}
}

func (a *App) handleTransmissionCurveSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseTransmissionCurve()
	case "decrease":
		value = a.config.DecreaseTransmissionCurve()
	default:
		value = int(a.config.GetTransmissionCurve() * 1000)
	}

	return strconv.Itoa(value)
}

func (a *App) handleTransmissionSatSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseTransmissionGforceMax()
	case "decrease":
		value = a.config.DecreaseTransmissionGforceMax()
	default:
		value = a.config.GetTransmissionGforceMax()
	}

	return strconv.FormatFloat(value, 'f', 1, 64)
}

func (a *App) handleTransmissionVolSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseTransmissionGain()
	case "decrease":
		value = a.config.DecreaseTransmissionGain()
	default:
		value = a.config.GetTransmissionGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func (a *App) handleVibrationCurveSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseJerkCurve()
	case "decrease":
		value = a.config.DecreaseJerkCurve()
	default:
		value = int(a.config.GetJerkCurve() * 1000)
	}

	return strconv.Itoa(value)
}

func (a *App) handleMasterVolSetting(action string) string {
	var value float64

	switch action {
	case "increase":
		value = a.config.IncreaseMasterGain()
	case "decrease":
		value = a.config.DecreaseMasterGain()
	default:
		value = a.config.GetMasterGain()
	}

	return strconv.FormatFloat(value, 'f', 2, 64) + " dB"
}

func (a *App) handleVibrationSatSetting(action string) string {
	var value int

	switch action {
	case "increase":
		value = a.config.IncreaseJerkMax()
	case "decrease":
		value = a.config.DecreaseJerkMax()
	default:
		value = a.config.GetJerkMax()
	}

	return strconv.Itoa(value)
}
