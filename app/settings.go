package app

import "strconv"

func (a *App) alterSetting(name string, action string) string {
	switch name {
	case "vol":
		value := float64(0)

		switch action {
		case "increase":
			value = a.synth.IncreaseMasterGain()
		case "decrease":
			value = a.synth.DecreaseMasterGain()
		default:
			value = a.synth.GetMasterGain()
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
	case "vMax":
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
	case "fMax":
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
	case "maxHz":
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
	case "minHz":
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
	case "gCurve":
		value := 0

		switch action {
		case "increase":
			value = a.config.IncreaseGearShiftCurve()
		case "decrease":
			value = a.config.DecreaseGearShiftCurve()
		default:
			value = int(a.config.GetGearShiftCurve() * 1000)
		}

		return strconv.Itoa(value)
	case "gMax":
		value := float64(0)

		switch action {
		case "increase":
			value = a.config.IncreaseGearShiftGforceMax()
		case "decrease":
			value = a.config.DecreaseGearShiftGforceMax()
		default:
			value = a.config.GetGearShiftGforceMax()
		}

		return strconv.FormatFloat(value, 'f', 1, 64)
	case "cVol":
		value := 0

		switch action {
		case "increase":
			value, _ = a.synth.IncreaseChannelVolume("chassis")
		case "decrease":
			value, _ = a.synth.DecreaseChannelVolume("chassis")
		default:
			value, _ = a.synth.GetChannelVolume("chassis")
		}

		return strconv.Itoa(value)
	case "gVol":
		value := 0

		switch action {
		case "increase":
			value, _ = a.synth.IncreaseChannelVolume("gearchange")
		case "decrease":
			value, _ = a.synth.DecreaseChannelVolume("gearchange")
		default:
			value, _ = a.synth.GetChannelVolume("gearchange")
		}

		return strconv.Itoa(value)
	case "mix":
		switch action {
		case "increase":
			return a.synth.Mixer.NextAlgorithm()
		case "decrease":
			return a.synth.Mixer.PreviousAlgorithm()
		default:
			return a.synth.Mixer.GetAlgorithm()
		}
	default:
		return "err"
	}
}
