package app

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
)

func (a *App) playGearShiftHaptic() {
	magnitude := a.determineGearShiftMagnitude()

	a.synth.PlayEffect("transmission", magnitude)

	a.log.Debug().
		Int("sequence_id", int(a.state.current.seq)).
		Float64("magnitude", magnitude).
		Float64("gforce", a.kinematics.GetSurgeGforce()).
		Int("gear", a.kinematics.Current.TransmissionGear).
		Msg("gear change")
}

func (a *App) determineGearShiftMagnitude() float64 {
	synthMagnitude, _ := a.synth.GetChannelMagnitude("transmission")

	if !a.config.DynamicTransmissionFeedbackEnabled() {
		return synthMagnitude
	}

	gForce := a.kinematics.GetSurgeGforce()
	gforceMax := a.config.GetTransmissionGforceMax()
	volumeCurve := a.config.GetTransmissionCurve()

	magnitudeMin := synth.GainToPowerRatio(a.transmissionGainMin)

	magnitude := math.Pow((gForce/gforceMax), volumeCurve) * synthMagnitude
	magnitude, _ = signal.LimitWindow(magnitude, magnitudeMin, synthMagnitude)

	return magnitude
}
