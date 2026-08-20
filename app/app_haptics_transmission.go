package app

import (
	"math"
	"slices"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// The gear-shift thump has two parts. The dog rings or synchronizers engage with a
// force that is constant per vehicle type. The gain floor (transmissionGainMin)
// supplies that force. This file calculates the dynamic part of the thump from the
// gearbox character and the change in engine braking.
//
//   - Character: how hard this gearbox shifts. The character does not change per
//     vehicle, so each shift measures the value that the next shift uses. The haptic
//     plays without delay.
//   - Event: how much engine braking this shift adds or removes. The gear ratios
//     predict the event on the shift frame. See gearShiftDrivelineStep.
const (
	// gearShiftLearningSamples is the number of measurements each direction collects.
	// The character does not change, so learning stops when the buffer fills.
	gearShiftLearningSamples = 16

	// gearShiftLaunchSpeedMps is the ground speed below which a gear change counts as a
	// standing start. A launch produces several times the jerk of a shift. The estimate
	// excludes it. The haptic still plays.
	gearShiftLaunchSpeedMps = 5.0

	// gearShiftMinMeasureFrames is the shortest measurement window. Engine speed passes
	// through the post-shift target during the flare, so the re-sync test needs a floor.
	gearShiftMinMeasureFrames = 4

	// gearShiftSyncFrames is the number of consecutive in-tolerance frames that end a
	// measurement. The match must persist. A flare ends the window early when that does
	// not happen.
	gearShiftSyncFrames = 2

	// gearShiftSyncTolerance is how close the ratio of rpm to speed must come to the
	// post-shift target, as a fraction of it. Five percent is above the median ratio
	// error and below the 95th percentile.
	gearShiftSyncTolerance = 0.05

	// gearShiftResyncHoldFrames is the extra measurement time after re-engagement. The
	// hold includes the torque re-application. A longer hold measures the brakes and the
	// suspension instead.
	gearShiftResyncHoldFrames = 8

	// gearShiftReferenceFrames is the re-engagement time of a modern sequential gearbox.
	// Both directions start here. A vehicle starts quickly and slows as evidence arrives.
	gearShiftReferenceFrames = 5.0

	// The pulse waveform at each end of the gearbox-speed range, and the durations that
	// map to them. A gearbox at or below gearShiftSharpFrames plays the sharp end. One
	// at or above gearShiftHeavyFrames plays the heavy end. Anything between
	// interpolates.
	//
	// Frequency and length carry the character value, because level cannot. Race cars
	// all operate near the rev limiter. A short high sound indicates a paddle shift. A
	// longer low sound indicates a clutch and a gear lever.
	gearShiftSharpFrames  = 5.0
	gearShiftHeavyFrames  = 11.0
	gearShiftSharpPulseHz = 30.0
	gearShiftHeavyPulseHz = 22.0
	gearShiftSharpSeconds = 0.100
	gearShiftHeavySeconds = 0.120

	// gearShiftPulseQuantiseHz rounds the mapped frequency before it triggers a
	// re-render. The learned duration changes as samples arrive.
	gearShiftPulseQuantiseHz = 1.0
)

// Tuning constants for the dynamic term. These are internal. A driver reaches the
// same loudness through Level, Race/Street Min Level and the Shift Compression Curve.
// They are `var` only so the probe harness can sweep them. See
// gearshift_probe_test.go. The program does not change them while it runs.
var (
	// gearShiftCharacterMax is the gearshift impulse value that maps to full character weight.
	// The unit is peak jerk multiplied by the number of frames to re-engage: see gearShiftImpulse.
	gearShiftCharacterMax = 1800.0 //nolint:gochecknoglobals // tuning constant; var only so the probe harness can sweep it

	// gearShiftStepMax is the driveline step at which the event term equals 1.0. The
	// value is the fleet median, so race cars sit below it and wide-ratio street boxes
	// above it.
	gearShiftStepMax = 0.30 //nolint:gochecknoglobals // tuning constant; var only so the probe harness can sweep it

	// gearShiftMaxMeasureFrames caps how long a shift is sampled, in frames at 60 Hz.
	// The cap stops measurements for shifts that never settle.
	gearShiftMaxMeasureFrames = 32 //nolint:gochecknoglobals // tuning constant; var only so the probe harness can sweep it
)

// gearShiftProfile tracks how hard the current vehicle changes gear.
//
// Each direction learns its own character. Sequential race boxes upshift harder than
// they downshift. Gated and clutched boxes do the reverse by a wider margin. A
// combined estimate reduces the downshift value for a fast car.
type gearShiftProfile struct {
	// Window peaks per direction, and the character taken from them. The character is
	// the median. A window long enough for a clutched gearbox also includes brake and
	// suspension spikes. A mean value absorbs those spikes.
	peaksUp       [gearShiftLearningSamples]float64
	peaksDown     [gearShiftLearningSamples]float64
	samplesUp     int     // peaks collected for the upshift character
	samplesDown   int     // peaks collected for the downshift character
	seed          float64 // starting character, mapped from the gain floor
	characterUp   float64 // settled or in-progress upshift character, m/s^3
	characterDown float64 // settled or in-progress downshift character, m/s^3

	// Frames from the shift to driveline re-engagement, medianed like the peaks above.
	// This measurement distinguishes a paddle-shift gearbox from a manual gearbox.
	// The upshift is the clean signal. The downshift durations span only 5 to 8 frames
	// for the whole fleet, because the rev-match flare sets the timing. A rev-match
	// flare releases the clutch pedal to match the engine speed before the gears engage.
	durationsUp         [gearShiftLearningSamples]float64
	durationsDown       [gearShiftLearningSamples]float64
	durationSamplesUp   int     // durations collected for the upshift estimate
	durationSamplesDown int     // durations collected for the downshift estimate
	durationSeed        float64 // starting duration, used until samples exist
	durationUp          float64 // settled or in-progress upshift duration, frames
	durationDown        float64 // settled or in-progress downshift duration, frames
	pulseHz             float64 // frequency the rendered waveform currently carries
	measuring           bool    // a measurement is in flight
	measuredDown        bool    // the in-flight measurement is of a downshift
	framesLeft          int     // frames left before the window hits the cap
	framesElapsed       int     // frames the in-flight measurement ran for
	peakJerk            float64 // largest surge jerk in this measurement

	// The re-engagement test that ends a measurement. syncTarget is the value of the
	// ratio of engine rpm to ground speed at the new gear ratio. syncFrames counts the
	// frames it stays at the value. syncTarget is 0 when the shift gives no usable
	// prediction. The frame cap ends the window when that happens.
	syncTarget float64
	syncFrames int
	resynced   bool // the driveline re-engaged and the hold period runs
	resyncAt   int  // framesElapsed at re-engagement, 0 when it did not occur

	// Driveline state of this frame and the one before it. advanceGearShiftDriveline
	// samples both at the top of the frame, so the magnitude path never reads the
	// telemetry client.
	lastRatio float64
	lastRPM   float64
	curRatio  float64
	curRPM    float64
}

// character returns the learned character for the given direction.
func (p *gearShiftProfile) character(down bool) float64 {
	if down {
		return p.characterDown
	}

	return p.characterUp
}

// duration returns the learned frames to re-engagement for the given direction.
func (p *gearShiftProfile) duration(down bool) float64 {
	if down {
		return p.durationDown
	}

	return p.durationUp
}

// samples returns how many measurements the given direction folded in.
func (p *gearShiftProfile) samples(down bool) int {
	if down {
		return p.samplesDown
	}

	return p.samplesUp
}

// settled reports whether a direction took all the samples it needs. Its character
// then stays frozen for the life of the vehicle.
func (p *gearShiftProfile) settled(down bool) bool {
	return p.samples(down) >= gearShiftLearningSamples
}

// seedGearShiftProfile resets the learned character for a newly selected vehicle. It
// seeds both directions with the jerk that maps to the gain floor, so the first shift
// lands at or above the configured minimum and can only rise from there. The seed
// tracks the floor, so it stays correct when the floor or the curve changes.
func (a *App) seedGearShiftProfile() {
	curve := a.config.GetHapticsTransmissionJerkCurve() / 1000
	characterMax := gearShiftCharacterMax

	var seed float64

	if curve > 0 {
		// Invert the magnitude formula to find the seed value at the floor magnitude.
		seed = characterMax * math.Pow(signal.GainToPowerRatio(a.transmissionGainMin), 1/curve)

		// characterMax holds impulse units. Divide by the seeded duration to return to
		// the jerk units of the peak buffers.
		seed /= gearShiftReferenceFrames
	}

	a.gearShift = gearShiftProfile{
		seed:          seed,
		characterUp:   seed,
		characterDown: seed,
		durationSeed:  gearShiftReferenceFrames,
		durationUp:    gearShiftReferenceFrames,
		durationDown:  gearShiftReferenceFrames,
	}

	// pulseHz is zero here, so this call always re-renders.
	a.refreshGearShiftPulse()
}

// playGearShiftHaptic plays a haptic effect for a gear change. It plays once from
// the character value learned on earlier shifts. The function then starts a
// measurement that refines the estimate for the next shift.
func (a *App) playGearShiftHaptic() {
	// A shift during a measurement folds in what that measurement gathered so far.
	if a.gearShift.measuring {
		a.completeGearShiftMeasurement()
	}

	surgeJerk := a.kinematics.GetSurgeJerk()
	down := a.gearShiftIsDownshift()
	magnitude := a.determineGearShiftMagnitude()

	a.synth.PlayEffect("gearShift", magnitude, synthesizer.ChannelTransmission)

	a.armGearShiftMeasurement(surgeJerk, down)

	a.log.Debug().
		Int("sequence_id", int(a.state.current.sequenceNumber)).
		Float64("magnitude", magnitude).
		Float64("gforce", a.kinematics.GetSurgeGforce()).
		Float64("jerk", surgeJerk).
		Bool("downshift", down).
		Float64("driveline_step", a.gearShiftDrivelineStep()).
		Float64("character", a.gearShift.character(down)).
		Int("samples", a.gearShift.samples(down)).
		Bool("settled", a.gearShift.settled(down)).
		Bool("learning", a.gearShift.measuring).
		Float64("speed", a.kinematics.Current.GroundSpeed).
		Int("gear", a.kinematics.Current.TransmissionGear).
		Msg("gear change")
}

// gearShiftIsDownshift reports whether the shift moved to a lower gear. Reverse and
// neutral are not downshifts, because their indices are sentinel values (0 and 15)
// that do not order against the forward gears.
func (a *App) gearShiftIsDownshift() bool {
	from, to := a.kinematics.Last.TransmissionGear, a.kinematics.Current.TransmissionGear
	if !isForwardGear(from) || !isForwardGear(to) {
		return false
	}

	return to < from
}

// armGearShiftMeasurement starts a measurement of the shift just played, which
// refines the estimate for the next shift in the same direction. It skips standing
// starts and settled directions.
func (a *App) armGearShiftMeasurement(surgeJerk float64, down bool) {
	if a.kinematics.Current.GroundSpeed < gearShiftLaunchSpeedMps {
		return
	}

	if a.gearShift.settled(down) {
		return
	}

	a.gearShift.measuring = true
	a.gearShift.measuredDown = down
	a.gearShift.framesLeft = gearShiftMaxMeasureFrames
	a.gearShift.framesElapsed = 0
	a.gearShift.peakJerk = surgeJerk
	a.gearShift.syncTarget = a.gearShiftSyncTarget()
	a.gearShift.syncFrames = 0
	a.gearShift.resynced = false
}

// gearShiftSyncTarget returns the value the ratio of engine rpm to ground speed
// settles to once the shift re-engages. It returns 0 when the shift gives no usable
// prediction. The gear ratio and a constant relate engine speed to road speed. The
// constant cancels, so the target needs no vehicle data beyond the reported ratios.
//
// The function must not use the rpmAfter prediction of gearShiftDrivelineStep. That
// function assumes a constant road speed. A downshift under heavy braking loses enough
// speed to move a fixed rpm target out of tolerance.
func (a *App) gearShiftSyncTarget() float64 {
	ratioFrom, ratioTo := a.gearShift.lastRatio, a.gearShift.curRatio
	speedBefore := a.kinematics.Last.GroundSpeed

	if ratioFrom <= 0 || ratioTo <= 0 || speedBefore <= 0 || a.gearShift.lastRPM <= 0 {
		return 0
	}

	return (a.gearShift.lastRPM / speedBefore) * (ratioTo / ratioFrom)
}

// gearShiftHasResynced reports whether the driveline settled at the new ratio. The
// gear change ends there, and later surge jerk belongs to something else.
func (a *App) gearShiftHasResynced() bool {
	if a.gearShift.syncTarget <= 0 || a.gearShift.framesElapsed < gearShiftMinMeasureFrames {
		return false
	}

	speed := a.kinematics.Current.GroundSpeed
	if speed <= 0 {
		a.gearShift.syncFrames = 0

		return false
	}

	if math.Abs(a.gearShift.curRPM/speed-a.gearShift.syncTarget) >
		gearShiftSyncTolerance*a.gearShift.syncTarget {
		a.gearShift.syncFrames = 0

		return false
	}

	a.gearShift.syncFrames++

	return a.gearShift.syncFrames >= gearShiftSyncFrames
}

// tickGearShiftMeasurement advances a measurement by one telemetry frame. The function
// updates the estimate when the window closes. It does nothing when no measurement runs.
//
// The window closes on re-engagement, not after a fixed count. A window long enough
// for a clutched gearbox also includes the braking that follows. Those spikes let the
// track define the character of the vehicle.
func (a *App) tickGearShiftMeasurement() {
	if !a.gearShift.measuring {
		return
	}

	a.gearShift.peakJerk = math.Max(a.gearShift.peakJerk, a.kinematics.GetSurgeJerk())

	a.gearShift.framesElapsed++
	a.gearShift.framesLeft--

	// Re-engagement shortens the window to the hold period. The measurement still
	// includes the torque re-application.
	if !a.gearShift.resynced && a.gearShiftHasResynced() {
		a.gearShift.resynced = true
		a.gearShift.resyncAt = a.gearShift.framesElapsed
		a.gearShift.framesLeft = min(a.gearShift.framesLeft, gearShiftResyncHoldFrames)
	}

	if a.gearShift.framesLeft > 0 {
		return
	}

	a.completeGearShiftMeasurement()
}

// completeGearShiftMeasurement records the peak jerk as one sample for the measured
// direction. The function then recomputes the character value and stops the measurement.
func (a *App) completeGearShiftMeasurement() {
	a.gearShift.measuring = false

	if a.gearShift.measuredDown {
		a.gearShift.recordPeak(&a.gearShift.peaksDown, &a.gearShift.samplesDown,
			&a.gearShift.characterDown, a.gearShift.peakJerk, a.gearShift.seed)

		if a.gearShift.resynced {
			a.gearShift.recordPeak(&a.gearShift.durationsDown, &a.gearShift.durationSamplesDown,
				&a.gearShift.durationDown, float64(a.gearShift.resyncAt), a.gearShift.durationSeed)
		}

		return
	}

	a.gearShift.recordPeak(&a.gearShift.peaksUp, &a.gearShift.samplesUp,
		&a.gearShift.characterUp, a.gearShift.peakJerk, a.gearShift.seed)

	// A window that reached the limit never re-engaged. The function discards the
	// duration when that happens.
	if a.gearShift.resynced {
		a.gearShift.recordPeak(&a.gearShift.durationsUp, &a.gearShift.durationSamplesUp,
			&a.gearShift.durationUp, float64(a.gearShift.resyncAt), a.gearShift.durationSeed)

		// Only the upshift duration shapes the waveform.
		a.refreshGearShiftPulse()
	}
}

// recordPeak appends one sample to a buffer and re-derives its median. It serves both
// the jerk peaks and the re-engagement durations.
func (p *gearShiftProfile) recordPeak(peaks *[gearShiftLearningSamples]float64,
	count *int, character *float64, peak float64, seed float64,
) {
	if *count >= gearShiftLearningSamples {
		return
	}

	peaks[*count] = peak
	*count++

	*character = medianCharacter(peaks[:*count], seed)
}

// medianCharacter returns the median of the collected peaks and the seed. The seed
// acts as a conservative pseudo-sample. It keeps the estimate low while few real
// samples exist. The buffer outvotes it as the buffer fills.
func medianCharacter(peaks []float64, seed float64) float64 {
	values := make([]float64, 0, len(peaks)+1)
	values = append(values, seed)
	values = append(values, peaks...)

	slices.Sort(values)

	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}

	return (values[mid-1] + values[mid]) / 2
}

// advanceGearShiftDriveline moves the previous driveline state to the last position
// and samples this frame. It must run after kinematics.Update sets the current gear
// and before the handler for this frame's gear change.
func (a *App) advanceGearShiftDriveline() {
	a.gearShift.lastRatio, a.gearShift.lastRPM = a.gearShift.curRatio, a.gearShift.curRPM
	a.gearShift.curRatio = a.currentGearRatio()
	a.gearShift.curRPM = float64(a.gtClient.Telemetry.EngineRPM())
}

// currentGearRatio returns the ratio for the selected gear, or 0 when the gear has no
// usable ratio.
//
// It must not use Transformer.CurrentGearRatio, which indexes GearRatios[gear-1]. That
// method panics on reverse (gear 0) and returns a negative sentinel for neutral (15).
func (a *App) currentGearRatio() float64 {
	gear := a.kinematics.Current.TransmissionGear

	ratios := a.gtClient.Telemetry.Transmission().GearRatios
	if gear < 1 || gear > len(ratios) {
		return 0
	}

	ratio := float64(ratios[gear-1])
	if ratio <= 0 {
		return 0
	}

	return ratio
}

// isForwardGear reports whether a gear index is a real forward ratio, and not reverse
// (0) or neutral (15).
func isForwardGear(gear int) bool {
	return gear >= 1 && gear < 15
}

// gearShiftDrivelineStep returns the event term. It is a dimensionless value that
// shows how much engine braking the shift adds or removes.
//
// The gear ratios predict the engine-speed step. The term needs no look-ahead. At a
// constant road speed the engine rpm equals the previous rpm multiplied by the ratio
// change. The final drive and the wheel radius cancel. Engine drag torque is linear in
// engine speed. It reaches the wheels through the ratio again. The step at the wheel
// scales as the square. The rev fraction shows that the same gear pair is a bigger
// event near the limiter than at low engine speed.
//
// The function returns 0 when no ratio is available. It also returns 0 when the shift
// involves reverse or neutral. The magnitude then uses the character value only.
func (a *App) gearShiftDrivelineStep() float64 {
	ratioFrom, ratioTo := a.gearShift.lastRatio, a.gearShift.curRatio
	if ratioFrom <= 0 || ratioTo <= 0 {
		return 0
	}

	revLimit := float64(a.vehicle.RevLimit)
	if revLimit <= 0 {
		return 0
	}

	ratioStep := ratioTo / ratioFrom
	revFraction := math.Min(1.0, a.gearShift.lastRPM/revLimit)

	return math.Abs(ratioStep*ratioStep-1) * revFraction
}

// determineGearShiftMagnitude returns the magnitude of the gear shift effect. A fixed
// magnitude simulates the shift mechanism. A dynamic magnitude adds the longitudinal
// forces. The driveline step drives those forces.
func (a *App) determineGearShiftMagnitude() float64 {
	if !a.config.GethapticsDynamicTransFeedbackEnabled() {
		return 1.0
	}

	return a.gearShiftMagnitudeFromDriveline()
}

// gearShiftImpulse returns the learned character value scaled by the number of frames
// the gearbox takes to complete the change.
//
// Surge jerk is a rate. A gearbox that spreads the same velocity change over twice the
// time feels half as hard. A clutch stretches the event. It does not reduce the
// velocity change. The bare rate underestimates the gearboxes that feel most
// mechanical. The product ranks the reference fleet as intended. A Super Formula scores
// 1207. A Supra RZ scores 282.
func (a *App) gearShiftImpulse(down bool) float64 {
	return a.gearShift.character(down) * a.gearShift.duration(down)
}

// gearShiftPulseShape maps a shift duration to the pulse frequency and length that
// show the duration. It interpolates between the quick and heavy ends. It clamps the
// value to these ends. It uses the upshift duration for both directions. A vehicle has
// one waveform.
func gearShiftPulseShape(durationFrames float64) (pulseHz float64, lengthSeconds float64) {
	span := gearShiftHeavyFrames - gearShiftSharpFrames

	position := (durationFrames - gearShiftSharpFrames) / span
	position = math.Max(0, math.Min(1, position))

	pulseHz = gearShiftSharpPulseHz + position*(gearShiftHeavyPulseHz-gearShiftSharpPulseHz)
	lengthSeconds = gearShiftSharpSeconds + position*(gearShiftHeavySeconds-gearShiftSharpSeconds)

	return math.Round(pulseHz/gearShiftPulseQuantiseHz) * gearShiftPulseQuantiseHz, lengthSeconds
}

// refreshGearShiftPulse re-renders the waveform when the learned gearbox speed changes
// it. The function does nothing otherwise. It runs on the main loop after a
// measurement closes.
func (a *App) refreshGearShiftPulse() {
	if a.synth == nil {
		return
	}

	pulseHz, lengthSeconds := gearShiftPulseShape(a.gearShift.durationUp)
	if pulseHz == a.gearShift.pulseHz {
		return
	}

	a.gearShift.pulseHz = pulseHz
	a.synth.SetGearShiftPulse(pulseHz, lengthSeconds)

	a.log.Debug().
		Float64("pulse_hz", pulseHz).
		Float64("length_seconds", lengthSeconds).
		Float64("duration_frames", a.gearShift.durationUp).
		Int("samples", a.gearShift.durationSamplesUp).
		Msg("gear shift pulse reshaped")
}

// gearShiftMagnitudeFromDriveline scales the learned character by how much engine
// braking the shift changes.
//
// The event term multiplies the character. The function must not add the values. How
// much engine braking changes is one quantity. How hard the driveline delivers that
// change is another. Street manual boxes have the widest ratio spacing in the fleet.
// An additive blend gives the softest gearbox the largest event and inverts the
// ranking.
//
// depth sets how far the event changes the character. At 0 the shift plays the
// character without change. At 1 a typical shift plays it without change. Small and
// large steps scale it in proportion.
func (a *App) gearShiftMagnitudeFromDriveline() float64 {
	depth := a.config.GetHapticsTransmissionStepBlend()
	volumeCurve := a.config.GetHapticsTransmissionJerkCurve() / 1000

	character := a.gearShiftImpulse(a.gearShiftIsDownshift()) / gearShiftCharacterMax
	event := a.gearShiftDrivelineStep() / gearShiftStepMax

	drive := character * (1 - depth + depth*event)

	// A gear selected at a standstill moves no driveline energy. Only the floor
	// remains. A zero event value scales the character by 1-depth. The volume curve
	// then raises that result back above the floor.
	if a.kinematics.Current.GroundSpeed < gearShiftLaunchSpeedMps {
		drive = 0
	}

	return a.limitGearShiftMagnitude(math.Pow(drive, volumeCurve))
}

// limitGearShiftMagnitude clamps a normalized magnitude to the gain floor and full
// scale. The result is relative to the transmission channel. PlayEffect applies the
// channel gain. Do not apply it again in this function.
func (a *App) limitGearShiftMagnitude(magnitude float64) float64 {
	magnitude, _ = signal.LimitWindow(magnitude, signal.GainToPowerRatio(a.transmissionGainMin), 1.0)

	return magnitude
}
