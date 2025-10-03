package app

import "strconv"

func (a *App) settingAction(setting string, action string) string {
	switch setting {
	case "cVol":
		var value float64

		switch action {
		case "increase":
			value = a.config.IncreaseChassisGain()
		case "decrease":
			value = a.config.DecreaseChassisGain()
		default:
			value = a.config.GetChassisGain()
		}

		return strconv.FormatFloat(value, 'f', 2, 64)
	case "ePVol":
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
	case "ePrimary":
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
	case "ePScale":
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
	case "eSecondary":
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
	case "eVol":
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
	case "fCurve":
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
	case "fMax":
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
	case "fMin":
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
	case "fSat":
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
	case "lang":
		switch action {
		case "increase":
			return a.config.NextLanguage()
		case "decrease":
			return a.config.PreviousLanguage()
		default:
			return *a.config.GetAppLanguage()
		}
	case "tCurve":
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
	case "tSat":
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
	case "tVol":
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
	case "vCurve":
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
	case "vol":
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
	case "vSat":
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
	default:
		return "error"
	}
}
