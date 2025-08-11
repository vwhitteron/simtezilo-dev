package app

import "strconv"

func (a *App) settingAction(setting string, action string) string {
	switch setting {
	case "vol":
		value := float64(0)

		switch action {
		case "increase":
			value = a.config.IncreaseMasterGain()
		case "decrease":
			value = a.config.DecreaseMasterGain()
		default:
			value = a.config.GetMasterGain()
		}

		return strconv.FormatFloat(value, 'f', 2, 64) + " dB"
	case "vCurve":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseJerkCurve()
		case "decrease":
			value = a.config.DecreaseJerkCurve()
		default:
			value = int(a.config.GetJerkCurve() * 1000)
		}

		return strconv.Itoa(value)
	case "vSat":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseJerkMax()
		case "decrease":
			value = a.config.DecreaseJerkMax()
		default:
			value = a.config.GetJerkMax()
		}

		return strconv.Itoa(value)
	case "fCurve":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseSnapCurve()
		case "decrease":
			value = a.config.DecreaseSnapCurve()
		default:
			value = int(a.config.GetSnapCurve() * 1000)
		}

		return strconv.Itoa(value)
	case "fSat":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseSnapMax()
		case "decrease":
			value = a.config.DecreaseSnapMax()
		default:
			value = a.config.GetSnapMax()
		}

		return strconv.Itoa(value)
	case "fMin":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseMinHz()
		case "decrease":
			value = a.config.DecreaseMinHz()
		default:
			value = int(a.config.GetMinHz())
		}

		return strconv.Itoa(value)
	case "fMax":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseMaxHz()
		case "decrease":
			value = a.config.DecreaseMaxHz()
		default:
			value = int(a.config.GetMaxHz())
		}

		return strconv.Itoa(value)
	case "tCurve":
		value := 0

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
		value := float64(0)

		switch action {
		case "increase":
			value = a.config.IncreaseTransmissionGforceMax()
		case "decrease":
			value = a.config.DecreaseTransmissionGforceMax()
		default:
			value = a.config.GetTransmissionGforceMax()
		}

		return strconv.FormatFloat(value, 'f', 1, 64)
	case "cVol":
		value := float64(0)

		switch action {
		case "increase":
			value = a.config.IncreaseChassisGain()
		case "decrease":
			value = a.config.DecreaseChassisGain()
		default:
			value = a.config.GetChassisGain()
		}

		return strconv.FormatFloat(value, 'f', 2, 64)
	case "tVol":
		value := float64(0)

		switch action {
		case "increase":
			value = a.config.IncreaseTransmissionGain()
		case "decrease":
			value = a.config.DecreaseTransmissionGain()
		default:
			value = a.config.GetTransmissionGain()
		}

		return strconv.FormatFloat(value, 'f', 2, 64)
	case "lang":
		switch action {
		case "increase":
			return a.config.NextLanguage()
		case "decrease":
			return a.config.PreviousLanguage()
		default:
			return a.config.GetLanguage()
		}
	default:
		return "error"
	}
}
