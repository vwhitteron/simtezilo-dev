// transmission.go holds the gear-shift haptic generator, extracted verbatim from
// package app so that offline tooling can drive the real generator against a recorded
// replay without linking an audio backend. App now delegates to a
// TransmissionGenerator, so there is a single source of truth.
//
// The package godoc lives in generator.go.

package haptics

import (
	"math"
	"slices"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
)

// The gear-shift thump has two parts. The dog rings or synchronizers engage with a
// force that is constant per vehicle type. The gain floor (gainMin) supplies that
// force. This file calculates the dynamic part of the thump from the gearbox character
// and the change in engine braking.
//
//   - Character: how hard this gearbox shifts. The character does not change per
//     vehicle, so each shift measures the value that the next shift uses. The haptic
//     plays without delay.
//   - Event: how much engine braking this shift adds or removes. The gear ratios
//     predict the event on the shift frame. See drivelineStep.
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
	// The unit is peak jerk multiplied by the number of frames to re-engage: see impulse.
	gearShiftCharacterMax = 1800.0 //nolint:gochecknoglobals // tuning constant; var only so the probe harness can sweep it

	// gearShiftStepMax is the driveline step at which the event term equals 1.0. The
	// value is the fleet median, so race cars sit below it and wide-ratio street boxes
	// above it.
	gearShiftStepMax = 0.30 //nolint:gochecknoglobals // tuning constant; var only so the probe harness can sweep it

	// gearShiftMaxMeasureFrames caps how long a shift is sampled, in frames at 60 Hz.
	// The cap stops measurements for shifts that never settle.
	gearShiftMaxMeasureFrames = 32 //nolint:gochecknoglobals // tuning constant; var only so the probe harness can sweep it

	// gearShiftSyncTolerance is how close the ratio of rpm to speed must come to the
	// post-shift target, as a fraction of it. Five percent is above the median ratio
	// error and below the 95th percentile.
	gearShiftSyncTolerance = 0.05 //nolint:gochecknoglobals // tuning constant; var only so the probe harness can sweep it
)

// TransmissionGenerator renders the gear-shift haptic and learns how hard the current
// vehicle changes gear.
//
// One generator serves one vehicle at a time. SetVehicle re-seeds it for a new vehicle.
// It is not safe for concurrent use: the live app drives it from the main loop only.
type TransmissionGenerator struct {
	cfg   *config.Config
	synth *synthesizer.Synthesizer
	kin   *kinematics.State
	tel   TelemetryFunc
	log   zerolog.Logger

	profile gearShiftProfile

	// gainMin is the gain floor for this vehicle type, in dB relative to the
	// transmission channel. SetVehicle derives it; do not read it from config directly,
	// because the floor differs per vehicle type.
	gainMin float64

	// revLimit is the current vehicle's rev limit, which scales the driveline step.
	revLimit uint16
}

// NewTransmissionGenerator builds a generator ready to drive the transmission channel.
// Call SetVehicle before the first gear change, so the gain floor and the seeded
// character match the vehicle.
func NewTransmissionGenerator(
	cfg *config.Config,
	synth *synthesizer.Synthesizer,
	kin *kinematics.State,
	tel TelemetryFunc,
	logger zerolog.Logger,
) *TransmissionGenerator {
	return &TransmissionGenerator{cfg: cfg, synth: synth, kin: kin, tel: tel, log: logger}
}

// SetVehicle sets the transmission gain floor from the vehicle type, and seeds the
// learned gear-shift harshness for the same vehicle.
//
// The floor is relative to the transmission channel, not absolute. The channel gain
// is applied by Synthesizer.PlayEffect, so folding it in here as well would move the
// floor with any trim the user applies to the channel.
func (g *TransmissionGenerator) SetVehicle(characteristics vehicle.Characteristics) {
	switch characteristics.VehicleType {
	case vehicle.TypeRace:
		g.gainMin = g.cfg.GetSynthTransmissionGainMinRace()
	case vehicle.TypeTuned:
		g.gainMin = (g.cfg.GetSynthTransmissionGainMinStreet() +
			g.cfg.GetSynthTransmissionGainMinRace()) / 2
	case vehicle.TypeStreet:
		fallthrough
	default:
		g.gainMin = g.cfg.GetSynthTransmissionGainMinStreet()
	}

	g.revLimit = characteristics.RevLimit

	// Seeded from the floor just set above, so it must follow the switch.
	g.seedProfile()
}

// Reset clears the learned profile without changing the vehicle, so the next shift
// starts from the seed again.
func (g *TransmissionGenerator) Reset() {
	g.seedProfile()
}

// GainMin returns the gain floor currently in force, in dB relative to the transmission
// channel. The probe harness reports it alongside the magnitudes it sweeps.
func (g *TransmissionGenerator) GainMin() float64 {
	return g.gainMin
}

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

	// Driveline state of this frame and the one before it. AdvanceDriveline samples
	// both at the top of the frame, so the magnitude path never reads the telemetry
	// client.
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

// GearHasChanged reports whether the gear changed between the last two telemetry
// frames. It ignores the initial unset state, which would otherwise read as a change.
func (g *TransmissionGenerator) GearHasChanged() bool {
	if g.kin.Current.TransmissionGear == kinematics.NullGear ||
		g.kin.Last.TransmissionGear == kinematics.NullGear {
		return false
	}

	return g.kin.Current.TransmissionGear != g.kin.Last.TransmissionGear
}

// PlayGearShift plays a haptic effect for a gear change. It plays once from the
// character value learned on earlier shifts. The method then starts a measurement that
// refines the estimate for the next shift. seq is the telemetry sequence number, which
// is used for logging only.
func (g *TransmissionGenerator) PlayGearShift(seq uint32) {
	// A shift during a measurement folds in what that measurement gathered so far.
	if g.profile.measuring {
		g.completeMeasurement()
	}

	surgeJerk := g.kin.GetSurgeJerk()
	down := g.isDownshift()
	magnitude := g.determineMagnitude()

	g.synth.PlayEffect("gearShift", magnitude, synthesizer.ChannelTransmission)

	g.armMeasurement(surgeJerk, down)

	g.log.Info().
		Int("sequence_id", int(seq)).
		Float64("magnitude", magnitude).
		Float64("gforce", g.kin.GetSurgeGforce()).
		Float64("jerk", surgeJerk).
		Bool("downshift", down).
		Float64("driveline_step", g.drivelineStep()).
		Float64("character", g.profile.character(down)).
		Int("samples", g.profile.samples(down)).
		Bool("settled", g.profile.settled(down)).
		Bool("learning", g.profile.measuring).
		Float64("speed", g.kin.Current.GroundSpeed).
		Int("gear", g.kin.Current.TransmissionGear).
		Msg("gear change")
}

// TickMeasurement advances a measurement by one telemetry frame. The method updates the
// estimate when the window closes. It does nothing when no measurement runs.
//
// The window closes on re-engagement, not after a fixed count. A window long enough
// for a clutched gearbox also includes the braking that follows. Those spikes let the
// track define the character of the vehicle.
func (g *TransmissionGenerator) TickMeasurement() {
	if !g.profile.measuring {
		return
	}

	g.profile.peakJerk = math.Max(g.profile.peakJerk, g.kin.GetSurgeJerk())

	g.profile.framesElapsed++
	g.profile.framesLeft--

	// Re-engagement shortens the window to the hold period. The measurement still
	// includes the torque re-application.
	if !g.profile.resynced && g.hasResynced() {
		g.profile.resynced = true
		g.profile.resyncAt = g.profile.framesElapsed
		g.profile.framesLeft = min(g.profile.framesLeft, gearShiftResyncHoldFrames)
	}

	if g.profile.framesLeft > 0 {
		return
	}

	g.completeMeasurement()
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

// AdvanceDriveline moves the previous driveline state to the last position and samples
// this frame. It must run after kinematics.Update sets the current gear and before the
// handler for this frame's gear change.
func (g *TransmissionGenerator) AdvanceDriveline() {
	g.profile.lastRatio, g.profile.lastRPM = g.profile.curRatio, g.profile.curRPM
	g.profile.curRatio = g.currentGearRatio()
	g.profile.curRPM = float64(g.tel().EngineRPM())
}

// seedProfile resets the learned character for a newly selected vehicle. It seeds both
// directions with the jerk that maps to the gain floor, so the first shift lands at or
// above the configured minimum and can only rise from there. The seed tracks the floor,
// so it stays correct when the floor or the curve changes.
func (g *TransmissionGenerator) seedProfile() {
	curve := g.cfg.GetHapticsTransmissionJerkCurve() / 1000
	characterMax := gearShiftCharacterMax

	var seed float64

	if curve > 0 {
		// Invert the magnitude formula to find the seed value at the floor magnitude.
		seed = characterMax * math.Pow(signal.GainToPowerRatio(g.gainMin), 1/curve)

		// characterMax holds impulse units. Divide by the seeded duration to return to
		// the jerk units of the peak buffers.
		seed /= gearShiftReferenceFrames
	}

	g.profile = gearShiftProfile{
		seed:          seed,
		characterUp:   seed,
		characterDown: seed,
		durationSeed:  gearShiftReferenceFrames,
		durationUp:    gearShiftReferenceFrames,
		durationDown:  gearShiftReferenceFrames,
	}

	// pulseHz is zero here, so this call always re-renders.
	g.refreshPulse()
}

// isDownshift reports whether the shift moved to a lower gear. Reverse and neutral are
// not downshifts, because their indices are sentinel values (0 and 15) that do not
// order against the forward gears.
func (g *TransmissionGenerator) isDownshift() bool {
	from, to := g.kin.Last.TransmissionGear, g.kin.Current.TransmissionGear
	if !isForwardGear(from) || !isForwardGear(to) {
		return false
	}

	return to < from
}

// armMeasurement starts a measurement of the shift just played, which refines the
// estimate for the next shift in the same direction. It skips standing starts and
// settled directions.
func (g *TransmissionGenerator) armMeasurement(surgeJerk float64, down bool) {
	if g.kin.Current.GroundSpeed < gearShiftLaunchSpeedMps {
		return
	}

	if g.profile.settled(down) {
		return
	}

	g.profile.measuring = true
	g.profile.measuredDown = down
	g.profile.framesLeft = gearShiftMaxMeasureFrames
	g.profile.framesElapsed = 0
	g.profile.peakJerk = surgeJerk
	g.profile.syncTarget = g.syncTarget()
	g.profile.syncFrames = 0
	g.profile.resynced = false
}

// syncTarget returns the value the ratio of engine rpm to ground speed settles to once
// the shift re-engages. It returns 0 when the shift gives no usable prediction. The
// gear ratio and a constant relate engine speed to road speed. The constant cancels, so
// the target needs no vehicle data beyond the reported ratios.
//
// The method must not use the rpmAfter prediction of drivelineStep. That method assumes
// a constant road speed. A downshift under heavy braking loses enough speed to move a
// fixed rpm target out of tolerance.
func (g *TransmissionGenerator) syncTarget() float64 {
	ratioFrom, ratioTo := g.profile.lastRatio, g.profile.curRatio
	speedBefore := g.kin.Last.GroundSpeed

	if ratioFrom <= 0 || ratioTo <= 0 || speedBefore <= 0 || g.profile.lastRPM <= 0 {
		return 0
	}

	return (g.profile.lastRPM / speedBefore) * (ratioTo / ratioFrom)
}

// hasResynced reports whether the driveline settled at the new ratio. The gear change
// ends there, and later surge jerk belongs to something else.
func (g *TransmissionGenerator) hasResynced() bool {
	if g.profile.syncTarget <= 0 || g.profile.framesElapsed < gearShiftMinMeasureFrames {
		return false
	}

	speed := g.kin.Current.GroundSpeed
	if speed <= 0 {
		g.profile.syncFrames = 0

		return false
	}

	if math.Abs(g.profile.curRPM/speed-g.profile.syncTarget) >
		gearShiftSyncTolerance*g.profile.syncTarget {
		g.profile.syncFrames = 0

		return false
	}

	g.profile.syncFrames++

	return g.profile.syncFrames >= gearShiftSyncFrames
}

// completeMeasurement records the peak jerk as one sample for the measured direction.
// The method then recomputes the character value and stops the measurement.
func (g *TransmissionGenerator) completeMeasurement() {
	g.profile.measuring = false

	if g.profile.measuredDown {
		g.profile.recordPeak(&g.profile.peaksDown, &g.profile.samplesDown,
			&g.profile.characterDown, g.profile.peakJerk, g.profile.seed)

		if g.profile.resynced {
			g.profile.recordPeak(&g.profile.durationsDown, &g.profile.durationSamplesDown,
				&g.profile.durationDown, float64(g.profile.resyncAt), g.profile.durationSeed)
		}

		return
	}

	g.profile.recordPeak(&g.profile.peaksUp, &g.profile.samplesUp,
		&g.profile.characterUp, g.profile.peakJerk, g.profile.seed)

	// A window that reached the limit never re-engaged. The method discards the
	// duration when that happens.
	if g.profile.resynced {
		g.profile.recordPeak(&g.profile.durationsUp, &g.profile.durationSamplesUp,
			&g.profile.durationUp, float64(g.profile.resyncAt), g.profile.durationSeed)

		// Only the upshift duration shapes the waveform.
		g.refreshPulse()
	}
}

// currentGearRatio returns the ratio for the selected gear, or 0 when the gear has no
// usable ratio.
//
// It must not use Transformer.CurrentGearRatio, which indexes GearRatios[gear-1]. That
// method panics on reverse (gear 0) and returns a negative sentinel for neutral (15).
func (g *TransmissionGenerator) currentGearRatio() float64 {
	gear := g.kin.Current.TransmissionGear

	ratios := g.tel().Transmission().GearRatios
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

// drivelineStep returns the event term. It is a dimensionless value that shows how much
// engine braking the shift adds or removes.
//
// The gear ratios predict the engine-speed step. The term needs no look-ahead. At a
// constant road speed the engine rpm equals the previous rpm multiplied by the ratio
// change. The final drive and the wheel radius cancel. Engine drag torque is linear in
// engine speed. It reaches the wheels through the ratio again. The step at the wheel
// scales as the square. The rev fraction shows that the same gear pair is a bigger
// event near the limiter than at low engine speed.
//
// The method returns 0 when no ratio is available. It also returns 0 when the shift
// involves reverse or neutral. The magnitude then uses the character value only.
func (g *TransmissionGenerator) drivelineStep() float64 {
	ratioFrom, ratioTo := g.profile.lastRatio, g.profile.curRatio
	if ratioFrom <= 0 || ratioTo <= 0 {
		return 0
	}

	revLimit := float64(g.revLimit)
	if revLimit <= 0 {
		return 0
	}

	ratioStep := ratioTo / ratioFrom
	revFraction := math.Min(1.0, g.profile.lastRPM/revLimit)

	return math.Abs(ratioStep*ratioStep-1) * revFraction
}

// determineMagnitude returns the magnitude of the gear shift effect. A fixed magnitude
// simulates the shift mechanism. A dynamic magnitude adds the longitudinal forces. The
// driveline step drives those forces.
func (g *TransmissionGenerator) determineMagnitude() float64 {
	if !g.cfg.GethapticsDynamicTransFeedbackEnabled() {
		return 1.0
	}

	return g.magnitudeFromDriveline()
}

// impulse returns the learned character value scaled by the number of frames the
// gearbox takes to complete the change.
//
// Surge jerk is a rate. A gearbox that spreads the same velocity change over twice the
// time feels half as hard. A clutch stretches the event. It does not reduce the
// velocity change. The bare rate underestimates the gearboxes that feel most
// mechanical. The product ranks the reference fleet as intended. A Super Formula scores
// 1207. A Supra RZ scores 282.
func (g *TransmissionGenerator) impulse(down bool) float64 {
	return g.profile.character(down) * g.profile.duration(down)
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

// refreshPulse re-renders the waveform when the learned gearbox speed changes it. The
// method does nothing otherwise. It runs on the main loop after a measurement closes.
func (g *TransmissionGenerator) refreshPulse() {
	if g.synth == nil {
		return
	}

	pulseHz, lengthSeconds := gearShiftPulseShape(g.profile.durationUp)
	if pulseHz == g.profile.pulseHz {
		return
	}

	g.profile.pulseHz = pulseHz
	g.synth.SetGearShiftPulse(pulseHz, lengthSeconds)

	g.log.Debug().
		Float64("pulse_hz", pulseHz).
		Float64("length_seconds", lengthSeconds).
		Float64("duration_frames", g.profile.durationUp).
		Int("samples", g.profile.durationSamplesUp).
		Msg("gear shift pulse reshaped")
}

// magnitudeFromDriveline scales the learned character by how much engine braking the
// shift changes.
//
// The event term multiplies the character. The method must not add the values. How
// much engine braking changes is one quantity. How hard the driveline delivers that
// change is another. Street manual boxes have the widest ratio spacing in the fleet.
// An additive blend gives the softest gearbox the largest event and inverts the
// ranking.
//
// depth sets how far the event changes the character. At 0 the shift plays the
// character without change. At 1 a typical shift plays it without change. Small and
// large steps scale it in proportion.
func (g *TransmissionGenerator) magnitudeFromDriveline() float64 {
	depth := g.cfg.GetHapticsTransmissionStepBlend()
	volumeCurve := g.cfg.GetHapticsTransmissionJerkCurve() / 1000

	character := g.impulse(g.isDownshift()) / gearShiftCharacterMax
	event := g.drivelineStep() / gearShiftStepMax

	drive := character * (1 - depth + depth*event)

	// A gear selected at a standstill moves no driveline energy. Only the floor
	// remains. A zero event value scales the character by 1-depth. The volume curve
	// then raises that result back above the floor.
	if g.kin.Current.GroundSpeed < gearShiftLaunchSpeedMps {
		drive = 0
	}

	return g.limitMagnitude(math.Pow(drive, volumeCurve))
}

// limitMagnitude clamps a normalized magnitude to the gain floor and full scale. The
// result is relative to the transmission channel. PlayEffect applies the channel gain.
// Do not apply it again in this method.
func (g *TransmissionGenerator) limitMagnitude(magnitude float64) float64 {
	magnitude, _ = signal.LimitWindow(magnitude, signal.GainToPowerRatio(g.gainMin), 1.0)

	return magnitude
}
