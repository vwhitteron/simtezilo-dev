package waveshare

import (
	"fmt"
	"image"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
)

const volumeFontSize = 20

var pages = []string{
	"volume",
	// "jerkProfile",
	"jerkExp",
	"jerkMax",
	// "snapProfile",
	"snapExp",
	"snapMax",
	"snapMinHz",
	"snapMaxHz",
	"gearExp",
	"gearMax",
	"chassis",
	"gear",
	"mixAlgo",
}
var currentPage = 0

func SetupWaveshareButtons(lcdDevice hardware.LCD, synth *synth.Synthesizer, config *config.Config, lastActive *time.Time, log zerolog.Logger) func() {
	return func() {
		hardware.OnButtonUpPressed(func() {
			displayString := ""

			switch pages[currentPage] {
			case "volume":
				synth.IncreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "A").
					Str("action", "increase master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerkProfile":
				profile := config.NextJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "next profile").
					Str("type", "jerk").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "jerkExp":
				profile := config.IncreaseJerkExponent()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "jerk exp").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "jerkMax":
				jerkMax := config.IncreaseJerkMax()

				displayString = fmt.Sprintf("JMax %d", jerkMax)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "jerk max").
					Str("value", fmt.Sprintf("%d", jerkMax)).
					Msg("button press")
			case "snapProfile":
				profile := config.NextSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "next profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snapExp":
				profile := config.IncreaseSnapExponent()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "snap exp").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snapMax":
				snapMax := config.IncreaseSnapMax()

				displayString = fmt.Sprintf("SMax %d", snapMax)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "snap max").
					Str("value", fmt.Sprintf("%d", snapMax)).
					Msg("button press")
			case "snapMinHz":
				minHz := config.IncreaseMinHz()

				displayString = fmt.Sprintf("fMin %d", minHz)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "min frequency").
					Str("value", fmt.Sprintf("%d", minHz)).
					Msg("button press")
			case "snapMaxHz":
				maxHz := config.IncreaseMaxHz()

				displayString = fmt.Sprintf("fMax %d", maxHz)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "max frequency").
					Str("value", fmt.Sprintf("%d", maxHz)).
					Msg("button press")
			case "gearExp":
				value := config.IncreaseGearExp()

				displayString = fmt.Sprintf("gExp %d", value)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "gear exp").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "gearMax":
				value := config.IncreaseGearMax()

				displayString = fmt.Sprintf("gMax %0.1f", value)

				log.Debug().
					Str("button", "A").
					Str("action", "increase").
					Str("type", "gear max").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "chassis":
				volume, _ := synth.IncreaseChannelVolume("chassis")

				displayString = fmt.Sprintf("Chassis %d", volume)

				log.Debug().
					Str("button", "A").
					Str("action", "increase chassis haptics volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "gear":
				volume, _ := synth.IncreaseChannelVolume("gearchange")

				displayString = fmt.Sprintf("Gear %d", volume)

				log.Debug().
					Str("button", "A").
					Str("action", "increase race gear volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "mixAlgo":
				algo := synth.Mixer.NextAlgorithm()

				displayString = fmt.Sprintf("Mix %s", algo)

				log.Debug().
					Str("button", "A").
					Str("action", "next mix algorithm").
					Str("algorithm", algo).
					Msg("button press")
			default:
				log.Debug().
					Str("button", "A").
					Str("action", "none").
					Str("mode", pages[currentPage]).
					Msg("button press")

				return
			}

			*lastActive = time.Now()
			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonDownPressed(func() {
			displayString := ""

			switch pages[currentPage] {
			case "volume":
				synth.DecreaseMasterGain()

				masterGain := synth.GetMasterGain()
				displayString = fmt.Sprintf("%0.2f dB", masterGain)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease master gain").
					Float64("master_gain", masterGain).
					Msg("button press")
			case "jerkProfile":
				profile := config.PreviousJerkProfile()

				displayString = fmt.Sprintf("Jerk %d", profile)

				log.Debug().
					Str("button", "B").
					Str("action", "previous profile").
					Str("type", "jerk").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "jerkExp":
				value := config.DecreaseJerkExponent()

				displayString = fmt.Sprintf("Jerk %d", value)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "jerk exp").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "jerkMax":
				value := config.DecreaseJerkMax()

				displayString = fmt.Sprintf("JMax %d", value)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "jerk max").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "snapProfile":
				profile := config.PreviousSnapProfile()

				displayString = fmt.Sprintf("Snap %d", profile)

				log.Debug().
					Str("button", "B").
					Str("action", "previous profile").
					Str("type", "snap").
					Str("profile", fmt.Sprintf("%d", profile)).
					Msg("button press")
			case "snapExp":
				value := config.DecreaseSnapExponent()

				displayString = fmt.Sprintf("Snap %d", value)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "snap exp").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "snapMax":
				snapMax := config.DecreaseSnapMax()

				displayString = fmt.Sprintf("SMax %d", snapMax)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "snap max").
					Str("value", fmt.Sprintf("%d", snapMax)).
					Msg("button press")
			case "snapMinHz":
				minHz := config.DecreaseMinHz()

				displayString = fmt.Sprintf("FMin %d", minHz)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "min frequency").
					Str("value", fmt.Sprintf("%d", minHz)).
					Msg("button press")
			case "snapMaxHz":
				maxHz := config.DecreaseMaxHz()

				displayString = fmt.Sprintf("FMax %d", maxHz)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease").
					Str("type", "max frequency").
					Str("value", fmt.Sprintf("%d", maxHz)).
					Msg("button press")
			case "gearExp":
				value := config.DecreaseGearExp()

				displayString = fmt.Sprintf("gExp %d", value)

				log.Debug().
					Str("button", "A").
					Str("action", "decrease").
					Str("type", "gear exp").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "gearMax":
				value := config.DecreaseGearMax()

				displayString = fmt.Sprintf("gMax %0.1f", value)

				log.Debug().
					Str("button", "A").
					Str("action", "decrease").
					Str("type", "gear max").
					Str("value", fmt.Sprintf("%d", value)).
					Msg("button press")
			case "chassis":
				volume, _ := synth.DecreaseChannelVolume("chassis")

				displayString = fmt.Sprintf("Chassis %d", volume)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease chassis haptics volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "gear":
				volume, _ := synth.DecreaseChannelVolume("gearchange")

				displayString = fmt.Sprintf("Gear %d", volume)

				log.Debug().
					Str("button", "B").
					Str("action", "decrease race gear volume").
					Str("profile", fmt.Sprintf("%d", volume)).
					Msg("button press")
			case "mixAlgo":
				algo := synth.Mixer.PreviousAlgorithm()

				displayString = fmt.Sprintf("Mix %s", algo)

				log.Debug().
					Str("button", "B").
					Str("action", "previous mix algorithm").
					Str("algorithm", algo).
					Msg("button press")
			default:
				log.Debug().
					Str("button", "B").
					Str("action", "none").
					Str("mode", pages[currentPage]).
					Msg("button press")

				return
			}

			*lastActive = time.Now()
			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonLeftPressed(func() {
			if currentPage <= 0 {
				currentPage = len(pages) - 1
			} else {
				currentPage--
			}

			displayString := ""

			switch pages[currentPage] {
			case "volume":
				displayString = fmt.Sprintf("%0.2f dB", synth.GetMasterGain())
			case "jerkProfile":
				displayString = fmt.Sprintf("Jerk %d", config.GetJerkProfile())
			case "jerkExp":
				displayString = fmt.Sprintf("Jerk %d", int(config.GetJerkExponent()*1000.0))
			case "jerkMax":
				displayString = fmt.Sprintf("JMax %d", config.GetJerkMax())
			case "snapProfile":
				displayString = fmt.Sprintf("Snap %d", config.GetSnapProfile())
			case "snapExp":
				displayString = fmt.Sprintf("Snap %d", int(config.GetSnapExponent()*1000.0))
			case "snapMax":
				displayString = fmt.Sprintf("SMax %d", config.GetSnapMax())
			case "minHz":
				displayString = fmt.Sprintf("FMin %d", int(config.GetMinHz()))
			case "maxHz":
				displayString = fmt.Sprintf("FMax %d", int(config.GetMaxHz()))
			case "chassis":
				volume, _ := synth.GetChannelVolume("chassis")
				displayString = fmt.Sprintf("Chassis %d", volume)
			case "gear":
				volume, _ := synth.GetChannelVolume("gearchange")
				displayString = fmt.Sprintf("Gear %d", volume)
			case "mixAlgo":
				displayString = fmt.Sprintf("Mix %s", synth.Mixer.GetAlgorithm())
			}

			log.Debug().
				Str("button", "X").
				Str("action", "next mode").
				Str("mode", pages[currentPage]).
				Msg("button press")

			*lastActive = time.Now()
			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonRightPressed(func() {
			if currentPage >= len(pages)-1 {
				currentPage = 0
			} else {
				currentPage++
			}

			displayString := ""

			switch pages[currentPage] {
			case "volume":
				displayString = fmt.Sprintf("%0.2f dB", synth.GetMasterGain())
			case "jerkProfile":
				displayString = fmt.Sprintf("Jerk %d", config.GetJerkProfile())
			case "jerkExp":
				displayString = fmt.Sprintf("Jerk %d", int(config.GetJerkExponent()*1000.0))
			case "jerkMax":
				displayString = fmt.Sprintf("JMax %d", config.GetJerkMax())
			case "snapProfile":
				displayString = fmt.Sprintf("Snap %d", config.GetSnapProfile())
			case "snapExp":
				displayString = fmt.Sprintf("Snap %d", int(config.GetSnapExponent()*1000.0))
			case "snapMax":
				displayString = fmt.Sprintf("SMax %d", config.GetSnapMax())
			case "minHz":
				displayString = fmt.Sprintf("FMin %d", int(config.GetMinHz()))
			case "maxHz":
				displayString = fmt.Sprintf("FMax %d", int(config.GetMaxHz()))
			case "chassis":
				volume, _ := synth.GetChannelVolume("chassis")
				displayString = fmt.Sprintf("Chassis %d", volume)
			case "gear":
				volume, _ := synth.GetChannelVolume("gearchange")
				displayString = fmt.Sprintf("Gear %d", volume)
			case "mixAlgo":
				displayString = fmt.Sprintf("Mix %s", synth.Mixer.GetAlgorithm())
			}

			log.Debug().
				Str("button", "X").
				Str("action", "next mode").
				Str("mode", pages[currentPage]).
				Msg("button press")

			*lastActive = time.Now()
			lcdDevice.PowerOn()
			canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
			lcdDevice.ShowTextCentered(canvas, displayString, volumeFontSize)
		})

		hardware.OnButtonCenterPressed(func() {
			log.Debug().
				Str("button", "Center").
				Str("action", "None").
				Msg("button press")
		})

		hardware.OnButtonOnePressed(func() {
			log.Debug().
				Str("button", "One").
				Str("action", "None").
				Msg("button press")
		})

		hardware.OnButtonTwoPressed(func() {
			orientation := lcdDevice.GetOrientation() + 90
			if orientation >= 360 {
				orientation = 0
			}

			lcdDevice.SetOrientation(orientation)
			log.Debug().
				Str("button", "Two").
				Str("action", "Rotate screen").
				Str("orientation", fmt.Sprintf("%d", orientation)).
				Msg("button press")
		})

		hardware.OnButtonThreePressed(func() {
			isPoweredOn := lcdDevice.PowerToggle()

			log.Debug().
				Str("button", "Three").
				Str("action", "Display Power").
				Str("state", fmt.Sprintf("%t", isPoweredOn)).
				Msg("button press")
		})

		log.Debug().Str("component", "waveshare 14972 buttons").Str("result", "success").Msg("button setup complete")
	}
}
