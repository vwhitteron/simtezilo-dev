// Package kinematics provides structures and methods for calculating and tracking vehicle kinematics.
package kinematics

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/kinematics/translationalenvelope"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// PositionalDerivatives holds values for the 2nd to 5th derivatives of position.
type PositionalDerivatives struct {
	Acceleration float64
	Jerk         float64
	Snap         float64
	Crackle      float64
}

// CalculatedTranslationalDerivatives holds 6DOF translational derivatives calculated from the telemetry data.
type CalculatedTranslationalDerivatives struct {
	PositionalDerivatives

	Velocity     models.Vector
	Acceleration models.Vector
	AccelMag     float64
}

// CalculatedRotationalDerivatives holds 6DOF rotational derivatives provided by the telemetry data.
type CalculatedRotationalDerivatives struct {
	PositionalDerivatives

	Velocity     models.Vector
	Acceleration models.Vector
	AccelMag     float64
}

// TranslationalDerivatives holds 6DOF translational derivatives calculated from the telemetry data.
type TranslationalDerivatives struct {
	PositionalDerivatives

	Acceleration models.TranslationalEnvelope
	AccelMag     float64
}

// RotationalDerivatives holds 6DOF rotational derivatives provided by the telemetry data.
type RotationalDerivatives struct {
	PositionalDerivatives

	Velocity     models.Vector
	Acceleration models.Vector
	AccelMag     float64
}

// Kinematics holds the kinematic state of the vehicle.
type Kinematics struct {
	SequenceID  uint32 // TODO: probably should rely on a.state.current instead of tracking separately here
	ComputeTime time.Duration
	Format      string

	SixDOFTranslation     TranslationalDerivatives
	SixDOFTranslationCalc CalculatedTranslationalDerivatives
	SixDOFRotation        RotationalDerivatives
	SixDOFRotationCalc    CalculatedRotationalDerivatives

	TransmissionGear int // TODO: probably should rely on a.state.current instead of tracking separately here

	GroundSpeed     float64
	SurgeCalculated float64

	// SurgeJerk is the rate of change of the surge (longitudinal) acceleration for
	// this frame, in m/s^3. Unlike the SixDOF jerk fields it differentiates the
	// surge axis alone rather than the three-axis acceleration magnitude, so road
	// bumps in heave/sway do not contaminate it. The transmission haptic uses it to
	// gauge how violently drive torque is cut and re-applied across a gear change.
	SurgeJerk float64

	// Resolved jerk/snap are the recovery-aware values the haptic path should
	// consume. After a sequence gap the calculated chain is still re-warming, so
	// these fall back to the telemetry-native envelope (translational only, and
	// only on formats that carry it) until the calculated values are valid again.
	ResolvedTransJerk float64
	ResolvedTransSnap float64
	ResolvedRotJerk   float64
	ResolvedRotSnap   float64

	// Per-output-channel synth amplitude/frequency for telemetry charting. Sized
	// to the configured output channel count by the haptic write path; readers
	// must length-check before indexing.
	SynthChannelAmplitude []float64
	SynthChannelFrequency []float64

	// Raw per-corner suspension height (metres) for this frame, as provided by
	// telemetry. Zeroed CornerSet on formats that do not carry suspension data.
	SuspensionHeight models.CornerSet

	// SuspensionRoughness is the sliding RMS, in m/s, of the high-passed per-corner
	// suspension velocity averaged across the four corners, over roughly 200 ms. It
	// measures how rough the surface is, not individual bumps.
	SuspensionRoughness float64

	// SuspensionRoughnessValid reports whether SuspensionRoughness carries a usable
	// measurement. It is false while the window refills after a sequence gap and on
	// formats carrying no suspension data, so a consumer can tell "measured smooth"
	// from "no data".
	SuspensionRoughnessValid bool

	// SurfaceType is the per-corner road surface classification for this frame, as
	// provided by telemetry (tarmac, concrete, grass, dirt, sand, snow, or unknown).
	// It drives the road-texture layer's loudness and grain: the render layer maps
	// each corner's surface to a rumble level and coarseness. Zeroed (all Unknown) on
	// formats that do not carry surface data.
	SurfaceType models.CornerSetGeneric[models.SurfaceType]
}

// State tracks the current and previous kinematic states of the vehicle.
type State struct {
	Last    Kinematics
	Current Kinematics

	// contiguousFrames counts consecutive gap-free telemetry frames (capped at the
	// deepest warm-up depth). It resets to zero on any sequence gap, which is how
	// the derivative chains re-warm rather than differencing across the gap. See
	// resolveDerivatives.
	contiguousFrames int

	// Per-domain detectors for the fs/2 telemetry cadence artefact. The GT motion
	// fields refresh on alternate frames (a big step then a near-repeat), which the
	// undamped accel->jerk->snap differences amplify into a 30 Hz buzz. Each gate
	// smooths its velocity only while that sustained every-frame alternation is
	// present, so one-shot impacts and sub-Nyquist motion pass through unfiltered.
	// See nyquistGate.
	transNyquist nyquistGate
	rotNyquist   nyquistGate

	// roughness is the road-roughness envelope's ring-buffer state. It lives on
	// State, not Kinematics, so the ring is not copied every frame by k.Last =
	// k.Current.
	roughness roughnessEnvelope

	// DisableNyquistGate bypasses both fs/2 cadence gates, so the calculated
	// velocity chains consume the raw telemetry velocity unchanged. It exists for
	// offline analysis (the in-app tuning assistant's "raw" audio render) that auditions the
	// ungated signal; the live app leaves it false and always gates.
	DisableNyquistGate bool

	// Diagnostic counters for the resolveDerivatives gap gate. GapResets counts how
	// many Update calls saw a non-contiguous sequence (delta != 1) and re-warmed;
	// LastGapDelta records the sequence delta of the most recent such reset. These
	// let the caller (which has a logger) gauge how often the haptic path is being
	// suppressed and whether the gaps are real drops or single-frame jitter.
	GapResets    int
	LastGapDelta int
}

// NewKinematicsState creates and initializes a new KinematicsTracker.
func NewKinematicsState() State {
	return State{
		Last:    newKinematics(),
		Current: newKinematics(),
	}
}

// newKinematics initializes a new Kinematics struct with default values.
func newKinematics() Kinematics {
	return Kinematics{
		SixDOFTranslationCalc: CalculatedTranslationalDerivatives{},
		SixDOFTranslation:     TranslationalDerivatives{},
		SixDOFRotationCalc:    CalculatedRotationalDerivatives{},
		ComputeTime:           0,
		TransmissionGear:      -100,
		GroundSpeed:           0,
		SurgeCalculated:       0,
		SurgeJerk:             0,
		SynthChannelAmplitude: nil,
		SynthChannelFrequency: nil,
		Format:                "A",
	}
}

// Update updates the kinematic state based on the provided telemetry data and time window.
// TODO: ideally this should not be given the gt client.
func (k *State) Update(windowSeconds float64, vehicleDimensions vehicle.Dimensions, gtclient *gttelemetry.Client) {
	k.Last = k.Current

	k.Current.Format = getTelemetryFormat(gtclient)

	k.Current.SequenceID = gtclient.Telemetry.SequenceID()

	accelFactor := float32(1.0 / windowSeconds)

	// Ground speed gates the fs/2 nyquist filter (below), so read it before the
	// calculated derivative chains that consume the gated velocity.
	k.Current.GroundSpeed = float64(gtclient.Telemetry.GroundSpeedMetresPerSecond())

	// 6DOF translational envelope - provided by telemetry
	k.Current.SixDOFTranslation.Acceleration = gtclient.Telemetry.TranslationEnvelope()
	k.Current.SixDOFTranslation.AccelMag = translationalenvelope.Magnitude(k.Current.SixDOFTranslation.Acceleration)
	k.Current.SixDOFTranslation.Jerk = (k.Current.SixDOFTranslation.AccelMag - k.Last.SixDOFTranslation.AccelMag) / windowSeconds
	k.Current.SixDOFTranslation.Snap = (k.Current.SixDOFTranslation.Jerk - k.Last.SixDOFTranslation.Jerk) / windowSeconds

	// 6DOF translational envelope - calculated from velocity vector
	transCalcVel := gtclient.Telemetry.VelocityVector()
	if !k.DisableNyquistGate {
		transCalcVel = k.transNyquist.filter(transCalcVel, float64(accelFactor), k.Current.GroundSpeed)
	}

	k.Current.SixDOFTranslationCalc.Velocity = transCalcVel
	translationCalcVelocityDelta := vector.Delta(k.Current.SixDOFTranslationCalc.Velocity, k.Last.SixDOFTranslationCalc.Velocity)
	k.Current.SixDOFTranslationCalc.Acceleration = vector.Scale(translationCalcVelocityDelta, accelFactor, accelFactor, accelFactor)
	k.Current.SixDOFTranslationCalc.AccelMag = vector.Magnitude(k.Current.SixDOFTranslationCalc.Acceleration)
	k.Current.SixDOFTranslationCalc.Jerk = (k.Current.SixDOFTranslationCalc.AccelMag - k.Last.SixDOFTranslationCalc.AccelMag) / windowSeconds
	k.Current.SixDOFTranslationCalc.Snap = (k.Current.SixDOFTranslationCalc.Jerk - k.Last.SixDOFTranslationCalc.Jerk) / windowSeconds
	k.Current.SixDOFTranslationCalc.Crackle = (k.Current.SixDOFTranslationCalc.Snap - k.Last.SixDOFTranslationCalc.Snap) / windowSeconds

	// 6DOF rotational envelope - provided by telemetry
	k.Current.SixDOFRotation.Velocity = gtclient.Telemetry.AngularVelocityVector()
	k.Current.SixDOFRotation.Acceleration = vector.Delta(k.Current.SixDOFRotation.Velocity, k.Last.SixDOFRotation.Velocity)
	k.Current.SixDOFRotation.AccelMag = vector.Magnitude(k.Current.SixDOFRotation.Acceleration)

	// 6DOF rotational envelope - calculated angular velocity vector
	// Convert from radians to metres at the wheels using vehicle dimensions
	rotCalcVel := vector.Scale(
		k.Current.SixDOFRotation.Velocity,
		vehicleDimensions.LongitudinalRadius,
		vehicleDimensions.LongitudinalRadius,
		vehicleDimensions.TransverseRadius,
	)
	if !k.DisableNyquistGate {
		rotCalcVel = k.rotNyquist.filter(rotCalcVel, float64(accelFactor), k.Current.GroundSpeed)
	}

	k.Current.SixDOFRotationCalc.Velocity = rotCalcVel
	rotationVelocityDelta := vector.Delta(k.Current.SixDOFRotationCalc.Velocity, k.Last.SixDOFRotationCalc.Velocity)
	k.Current.SixDOFRotationCalc.Acceleration = vector.Scale(rotationVelocityDelta, accelFactor, accelFactor, accelFactor)
	k.Current.SixDOFRotationCalc.AccelMag = vector.Magnitude(k.Current.SixDOFRotationCalc.Acceleration)
	k.Current.SixDOFRotationCalc.Jerk = (k.Current.SixDOFRotationCalc.AccelMag - k.Last.SixDOFRotationCalc.AccelMag) / windowSeconds
	k.Current.SixDOFRotationCalc.Snap = (k.Current.SixDOFRotationCalc.Jerk - k.Last.SixDOFRotationCalc.Jerk) / windowSeconds

	k.Current.TransmissionGear = gtclient.Telemetry.CurrentGear()
	k.Current.SurgeCalculated = signal.Abs(float64(k.Current.SixDOFRotationCalc.Acceleration.X))

	// Differentiate the surge axis alone. SurgeCalculated is set immediately above,
	// so both frames' surge sources are populated by this point.
	k.Current.SurgeJerk = (surgeAccel(k.Current) - surgeAccel(k.Last)) / windowSeconds

	k.Current.SuspensionHeight = gtclient.Telemetry.SuspensionHeightMetres()
	k.Current.SurfaceType = gtclient.Telemetry.SurfaceType()

	k.resolveDerivatives(windowSeconds)
}

// Contiguous gap-free frames each source needs before its snap (the deepest
// derivative the chassis pulse consumes) is free of the cross-gap finite-
// difference artefact. The calculated chain differences velocity, so its
// acceleration needs one delta and each further derivative one more frame. The
// native translational envelope provides acceleration directly, so its snap warms
// one frame sooner.
const (
	calcSnapWarmFrames   = 3
	nativeSnapWarmFrames = 2
)

// Constants governing the road-roughness envelope (roughnessEnvelope.update).
const (
	// roughnessHighpassHz is the one-pole DC-blocker corner applied to each
	// corner's suspension velocity, in Hz. Telemetry arrives at 59.94 Hz, so the
	// usable band is 0-30 Hz. Body attitude motion (braking dive, jump crest,
	// heave/pitch/roll of the sprung mass) sits at 0.8-2.5 Hz and must be
	// rejected; wheel hop and aliased road content at 10-30 Hz must pass. At 8 Hz
	// the response is -18 dB at 1 Hz and -0.5 dB at 25 Hz. Its time constant is 20
	// ms, about 1.2 frames, so a free fall's constant velocity decays to zero
	// within two frames of the wheels leaving the ground. Do not raise it above
	// ~12 Hz: the pole then eats the remaining band and the fs/2 cadence artefact
	// dominates. Do not lower it below ~5 Hz: braking dive leaks through as false
	// roughness.
	roughnessHighpassHz = 8.0

	// roughnessWindowFrames is the sliding RMS window length, 200.2 ms at 59.94
	// Hz. Its envelope cutoff is about 5 Hz, which is what makes it measure how
	// rough the surface IS rather than tracking individual bumps; the chassis
	// impact pulse keeps sole ownership of discrete bumps and kerbs. A 3-frame
	// kerb strike contributes a quarter of the window's energy and decays over
	// 200 ms, so it reads as a swell, not a hit.
	roughnessWindowFrames = 12

	// roughnessMinFillFrames is how many frames must be in the ring before the
	// envelope reports a measurement, so one post-gap spike cannot produce a
	// full-scale roughness on a single frame.
	roughnessMinFillFrames = 4

	// roughnessMaxVelocityMps is a per-corner sanity clamp on the high-passed
	// velocity, so a corrupt or extreme sample cannot dominate the window.
	roughnessMaxVelocityMps = 5.0

	// roughnessCorners is the number of suspension corners averaged into the
	// envelope: front-left, front-right, rear-left, rear-right.
	roughnessCorners = 4
)

// roughnessEnvelope estimates road-surface roughness from suspension movement.
// Telemetry's 59.94 Hz rate means this can only ever produce a level, never a
// spectrum.
type roughnessEnvelope struct {
	lowpass [roughnessCorners]float64      // per-corner one-pole low-pass state, m/s; the high-pass is the subtraction below
	ring    [roughnessWindowFrames]float64 // per-frame mean square across the corners
	index   int
	filled  int
	sum     float64
}

// update advances the envelope by one frame and returns the current roughness
// estimate (RMS, m/s) and whether it is valid. height and last are this frame's
// and the previous frame's per-corner suspension heights; windowSeconds is the
// frame period; contiguous reports whether this frame follows the last one
// without a sequence gap.
func (r *roughnessEnvelope) update(height, last models.CornerSet, windowSeconds float64, contiguous bool) (float64, bool) {
	// A gap means the height delta spans more than one frame, so it is not a
	// single-frame velocity, and the fixed high-pass coefficient assumes a fixed
	// frame rate.
	if !contiguous || windowSeconds <= 0 {
		*r = roughnessEnvelope{}

		return 0, false
	}

	// Formats that carry no suspension data report all zeros. Without this
	// guard they would report a permanently smooth road rather than no
	// measurement.
	if height == (models.CornerSet{}) {
		*r = roughnessEnvelope{}

		return 0, false
	}

	// One-pole low-pass coefficient for the given frame period; see the
	// coefficient idiom at app/haptics/texture.go:204.
	alpha := 1 - math.Exp(-2*math.Pi*roughnessHighpassHz*windowSeconds)

	current := [roughnessCorners]float64{
		float64(height.FrontLeft), float64(height.FrontRight),
		float64(height.RearLeft), float64(height.RearRight),
	}
	previous := [roughnessCorners]float64{
		float64(last.FrontLeft), float64(last.FrontRight),
		float64(last.RearLeft), float64(last.RearRight),
	}

	sumSq := 0.0

	for i := range roughnessCorners {
		velocity := (current[i] - previous[i]) / windowSeconds

		r.lowpass[i] += alpha * (velocity - r.lowpass[i])

		road := velocity - r.lowpass[i]
		road = min(max(road, -roughnessMaxVelocityMps), roughnessMaxVelocityMps)

		sumSq += road * road
	}

	r.sum -= r.ring[r.index]
	r.ring[r.index] = sumSq / roughnessCorners
	r.sum += r.ring[r.index]
	r.index = (r.index + 1) % roughnessWindowFrames

	if r.filled < roughnessWindowFrames {
		r.filled++
	}

	if r.filled < roughnessMinFillFrames {
		return 0, false
	}

	// Divide by filled, not the window size, so the estimator degrades
	// gracefully while re-warming instead of reporting an artificially low RMS.
	return math.Sqrt(r.sum / float64(r.filled)), true
}

// resolveDerivatives selects, per domain, the jerk/snap values the chassis haptic
// should consume after accounting for telemetry gaps. A frame is contiguous when
// its sequence ID is exactly one greater than the previous frame's; any gap resets
// contiguousFrames, so the chains re-warm from scratch instead of differencing
// across the gap (which produces a spurious high-power pulse). It also advances
// the suspension-roughness envelope, since the envelope shares the same gap gate.
//
// Selection is whole-source: jerk and snap always come from the same fully-warmed
// source, so the pulse never mixes a warmed jerk with an unwarmed snap. The
// translational domain prefers the calculated chain, falls back to the native
// envelope (on formats that carry it, which warms one frame sooner), and is
// otherwise suppressed. The rotational domain has no native envelope, so it is
// suppressed until the calculated chain warms — which is why a recovering pulse
// can be translational-only for the one frame between native and calculated
// readiness.
func (k *State) resolveDerivatives(windowSeconds float64) {
	contiguous := k.Current.SequenceID == k.Last.SequenceID+1

	if contiguous {
		if k.contiguousFrames < calcSnapWarmFrames {
			k.contiguousFrames++
		}
	} else {
		k.contiguousFrames = 0
		k.GapResets++
		k.LastGapDelta = int(int64(k.Current.SequenceID) - int64(k.Last.SequenceID))
	}

	k.Current.SuspensionRoughness, k.Current.SuspensionRoughnessValid = k.roughness.update(k.Current.SuspensionHeight, k.Last.SuspensionHeight, windowSeconds, contiguous)

	calcWarm := k.contiguousFrames >= calcSnapWarmFrames
	nativeWarm := formatSupportsNativeEnvelope(k.Current.Format) &&
		k.contiguousFrames >= nativeSnapWarmFrames

	// Translational: calc when warm, else native fallback, else suppressed.
	switch {
	case calcWarm:
		k.Current.ResolvedTransJerk = k.Current.SixDOFTranslationCalc.Jerk
		k.Current.ResolvedTransSnap = k.Current.SixDOFTranslationCalc.Snap
	case nativeWarm:
		k.Current.ResolvedTransJerk = k.Current.SixDOFTranslation.Jerk
		k.Current.ResolvedTransSnap = k.Current.SixDOFTranslation.Snap
	default:
		k.Current.ResolvedTransJerk = 0
		k.Current.ResolvedTransSnap = 0
	}

	// Rotational: no native envelope, so calc when warm, else suppressed.
	if calcWarm {
		k.Current.ResolvedRotJerk = k.Current.SixDOFRotationCalc.Jerk
		k.Current.ResolvedRotSnap = k.Current.SixDOFRotationCalc.Snap
	} else {
		k.Current.ResolvedRotJerk = 0
		k.Current.ResolvedRotSnap = 0
	}
}

// formatSupportsNativeEnvelope reports whether the telemetry format carries a
// trustworthy native translational acceleration envelope. Mirrors the format gate
// used by GetSurgeGforce.
func formatSupportsNativeEnvelope(format string) bool {
	return format == "~" || format == "B"
}

const (
	// nyquistGateEngageFrames is the alternation run length at which the gate treats
	// the signal as the fs/2 cadence artefact and starts smoothing. Each frame whose
	// accel-magnitude jerk flips sign adds one to the run; each non-alternating frame
	// decays it by one (hysteresis). Reaching three (four alternating frames, ~67 ms
	// at 60 Hz) clears one-shot impacts and single rebounds, which flip at most twice.
	nyquistGateEngageFrames = 3

	// nyquistGateMaxRun caps the alternation run so a sustained non-alternating event
	// (a real impact holding one sign) decays back below the engage threshold within
	// a few frames and releases the gate, while brief single-frame breaks in an
	// ongoing ripple do not drop it.
	nyquistGateMaxRun = nyquistGateEngageFrames + 3

	// nyquistGateJerkDeadzone is the minimum per-frame change in acceleration
	// magnitude (m/s^2) that counts as a jerk sign flip. Below it the frame is
	// treated as non-alternating so numerical noise near steady motion cannot engage
	// the gate.
	nyquistGateJerkDeadzone = 0.25

	// nyquistGateFullSpeedMps is the ground speed (m/s) at/below which the gate is
	// fully active. The fs/2 cadence artefact dominates the (small) real road signal
	// only at low speed, where its alternation is sustained so the gate stays engaged
	// and smoothly averages — reducing the coming-to-a-stop judder it exists to fix.
	nyquistGateFullSpeedMps = 5.0

	// nyquistGateZeroSpeedMps is the ground speed (m/s) at/above which the gate is
	// fully disabled (raw passthrough). Above it real broadband road content dominates
	// and only intermittently trips the fs/2 detector, so engaging would chatter
	// on/off and inject large jerk/snap spikes ("heavy impacts down straights"). The
	// influence ramps linearly between the two thresholds so the transition never
	// steps.
	nyquistGateZeroSpeedMps = 12.0
)

// nyquistGateSpeedInfluence returns how much of the two-tap average to apply at the
// given ground speed: 1 at/below nyquistGateFullSpeedMps, 0 at/above
// nyquistGateZeroSpeedMps, and a linear ramp between. Blending (rather than a hard
// speed cutoff) means the gate fades out smoothly, so neither engagement nor the
// speed boundary itself introduces a discontinuity the derivative chain would
// amplify.
func nyquistGateSpeedInfluence(speedMps float64) float64 {
	switch {
	case speedMps <= nyquistGateFullSpeedMps:
		return 1
	case speedMps >= nyquistGateZeroSpeedMps:
		return 0
	default:
		return (nyquistGateZeroSpeedMps - speedMps) / (nyquistGateZeroSpeedMps - nyquistGateFullSpeedMps)
	}
}

// nyquistGate detects and suppresses the fs/2 telemetry cadence artefact on a
// single velocity signal. The artefact makes the calculated acceleration
// magnitude alternate high/low every frame, so its jerk flips sign every frame;
// the gate counts that run and, once it is clearly sustained, replaces the
// velocity with a two-tap average whose frequency response has a null exactly at
// fs/2. Detection runs on the raw (unsmoothed) velocity it is handed, so the gate
// does not chase its own output and toggle. A genuine impact or rebound is a
// one-shot excursion rather than a sustained alternation, so it never reaches the
// engage threshold and passes through unfiltered.
type nyquistGate struct {
	lastVel      models.Vector
	lastAccelMag float64
	lastJerkSign float64
	altRun       int
	haveVel      bool
	haveAccel    bool
}

// filter returns the velocity the derivative chain should consume for this frame:
// the raw velocity, or a two-tap average of it with the previous frame while the
// fs/2 cadence artefact is present. accelFactor is 1/windowSeconds and only scales
// the detector's magnitudes uniformly, so it does not affect the sign logic.
//
// speedMps gates the correction: the averaging is applied in full only at low ground
// speed (where the cadence artefact dominates and the gate stays engaged), fades out
// by nyquistGateZeroSpeedMps, and is off at racing speed — where the detector would
// only trip intermittently and chatter into jerk/snap spikes. Detection state is kept
// warm at all speeds so the gate is ready the moment speed drops back into range.
func (g *nyquistGate) filter(rawVel models.Vector, accelFactor, speedMps float64) models.Vector {
	if !g.haveVel {
		g.lastVel = rawVel
		g.haveVel = true

		return rawVel
	}

	scale := float32(accelFactor)
	accel := vector.Scale(vector.Delta(rawVel, g.lastVel), scale, scale, scale)
	accelMag := vector.Magnitude(accel)

	engaged := false

	if g.haveAccel {
		jerk := accelMag - g.lastAccelMag

		sign := 0.0
		if signal.Abs(jerk) > nyquistGateJerkDeadzone {
			sign = signal.Polarity(jerk)
		}

		switch {
		case sign != 0 && g.lastJerkSign != 0 && sign != g.lastJerkSign:
			if g.altRun < nyquistGateMaxRun {
				g.altRun++
			}
		case g.altRun > 0:
			g.altRun--
		}

		if sign != 0 {
			g.lastJerkSign = sign
		}

		engaged = g.altRun >= nyquistGateEngageFrames
	}

	g.lastAccelMag = accelMag
	g.haveAccel = true

	out := rawVel

	if engaged {
		if influence := float32(nyquistGateSpeedInfluence(speedMps)); influence > 0 {
			// Blend the fs/2 two-tap average toward raw by the speed influence: full
			// average at low speed, none at racing speed. influence == 1 reproduces the
			// original (raw+lastVel)/2.
			out = models.Vector{
				X: rawVel.X + influence*((rawVel.X+g.lastVel.X)/2-rawVel.X),
				Y: rawVel.Y + influence*((rawVel.Y+g.lastVel.Y)/2-rawVel.Y),
				Z: rawVel.Z + influence*((rawVel.Z+g.lastVel.Z)/2-rawVel.Z),
			}
		}
	}

	g.lastVel = rawVel

	return out
}

// surgeAccel returns the signed surge (longitudinal) acceleration in m/s^2 for a
// frame, preferring the telemetry-native envelope on the formats that carry a
// trustworthy one and falling back to the calculated surge otherwise.
func surgeAccel(k Kinematics) float64 {
	if formatSupportsNativeEnvelope(k.Format) {
		return float64(k.SixDOFTranslation.Acceleration.Surge)
	}

	return float64(k.SurgeCalculated)
}

// GetSurgeGforce calculates and returns the translational envelope surge G-force based on the current kinematic state.
func (k *State) GetSurgeGforce() float64 {
	gForce := signal.Abs(surgeAccel(k.Current) / GravityConstant)

	return gForce
}

// GetSurgeJerk returns the magnitude of the current surge jerk in m/s^3, i.e. how
// fast the longitudinal acceleration is changing. The transmission haptic uses it
// to distinguish a race car's near-instant torque cut and re-application from a
// street car's gradual clutch and throttle ramp.
func (k *State) GetSurgeJerk() float64 {
	return signal.Abs(k.Current.SurgeJerk)
}

// getTelemetryFormat determines the telemetry format based on the telemetry client's raw telemetry data.
// Currently supports formats "A", "B", and "~".
func getTelemetryFormat(gtClient *gttelemetry.Client) string {
	isAddendum2, _ := gtClient.Telemetry.RawTelemetry.Addendum2Format()
	if isAddendum2 {
		return "~"
	}

	isAddendum1, _ := gtClient.Telemetry.RawTelemetry.Addendum1Format()
	if isAddendum1 {
		return "B"
	}

	return "A"
}
