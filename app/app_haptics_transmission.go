package app

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// playGearShiftHaptic outputs a haptic effect simulating forces during a gear change.
func (a *App) playGearShiftHaptic() {
	magnitude := a.determineGearShiftMagnitude()

	a.synth.PlayEffect("transmission", magnitude)

	a.log.Debug().
		Int("sequence_id", int(a.state.current.sequenceNumber)).
		Float64("magnitude", magnitude).
		Float64("gforce", a.kinematics.GetSurgeGforce()).
		Int("gear", a.kinematics.Current.TransmissionGear).
		Msg("gear change")
}

// determineGearShiftMagnitude calculates the magnitude of the gear shift haptic effect.
// A fixed magnitude simulates only the forces of the gear change mechanism itself.
// A dynamic magnitude simulates the forces of the gear change mechanism combined with
// the longitudinal g-force experienced during the gear change.
func (a *App) determineGearShiftMagnitude() float64 {
	synthMagnitude, _ := a.synth.GetChannelMagnitude("transmission")

	if !a.config.DynamicTransmissionFeedbackEnabled() {
		return synthMagnitude
	}

	gForce := a.kinematics.GetSurgeGforce()
	gforceMax := a.config.GetTransmissionGforceMax()
	volumeCurve := a.config.GetTransmissionCurve()

	magnitudeMin := synthesizer.GainToPowerRatio(a.transmissionGainMin)

	magnitude := math.Pow((gForce/gforceMax), volumeCurve) * synthMagnitude
	magnitude, _ = signal.LimitWindow(magnitude, magnitudeMin, synthMagnitude)

	return magnitude
}
