package kinematics_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
)

// These tests exercise the chassis haptic's real dependency chain end-to-end
// through State.Update — telemetry in, resolveDerivatives out — rather than the
// internal step() helper used by kinematics_resolve_internal_test.go, which
// installs pre-computed derivative values and always uses a +1 sequence stride.
//
// Chassis amplitude/frequency (app_haptics_chassis.go) read ResolvedTransJerk /
// ResolvedRotJerk. Engine (RPM) and transmission (g-force) do NOT go through the
// resolver, so a resolver that never warms silences chassis alone — the exact
// "engine and transmission output but not chassis" symptom. These tests guard
// that path so the regression is caught before deploy.

// runStream drives frameCount frames through the real Update path with the given
// per-frame sequence stride, feeding a quadratically increasing velocity so the
// calculated accel→jerk→snap chain is genuinely non-zero. It returns the state
// after the final frame.
func runStream(t *testing.T, stride uint32, frameCount int) *kinematics.State {
	t.Helper()

	stateValue := kinematics.NewKinematicsState()
	state := &stateValue

	gt, err := gttelemetry.New(gttelemetry.Options{})
	require.NoError(t, err)
	gt.Telemetry.SetFormatStandard()

	dims := vehicle.Dimensions{
		WheelbaseMetres:    2.5,
		TrackWidthMetres:   1.6,
		LongitudinalRadius: 1.25,
		TransverseRadius:   0.8,
	}

	const window = 0.016

	var (
		seq uint32
		vx  float64
	)

	for i := 1; i <= frameCount; i++ {
		seq += stride
		vx += float64(i) * float64(i) // quadratic → non-zero jerk and snap

		gt.Telemetry.RawTelemetry.SequenceId = seq
		gt.Telemetry.SetVelocityVector(float32(vx), 0, 0)
		gt.Telemetry.SetAngularVelocityVector(float32(vx), 0, 0)

		state.Update(window, dims, gt)

		// Mirror app_haptics.go, which reassigns Last = Current after Update.
		state.Last = state.Current
	}

	return state
}

// TestUpdateResolvesChassisDerivativesOnContiguousStream is the core chassis
// contract: a steady telemetry stream with a +1 sequence stride (what a replay
// file and normal live capture produce) must warm the resolver so the chassis
// pulse has a non-zero jerk/snap to work with. If this fails, chassis is silent
// while engine and transmission still play.
func TestUpdateResolvesChassisDerivativesOnContiguousStream(t *testing.T) {
	t.Parallel()

	// Enough frames to clear the calc warm-up depth several times over.
	state := runStream(t, 1, 8)

	require.NotZero(t, state.Current.ResolvedTransJerk,
		"contiguous telemetry must warm the chassis translational jerk")
	require.NotZero(t, state.Current.ResolvedTransSnap,
		"contiguous telemetry must warm the chassis translational snap")
	require.NotZero(t, state.Current.ResolvedRotJerk,
		"contiguous telemetry must warm the chassis rotational jerk")
	require.Zero(t, state.GapResets,
		"a contiguous stream should never trip the gap gate")
}

// TestUpdateSuppressesChassisOnNonUnitStride documents — and guards — the known
// fragility behind the field report: resolveDerivatives treats ANY sequence
// stride other than +1 as a gap, so a steady but non-unit stream (no dropped
// packets, just a cadence whose SequenceID advances by >1 per processed frame)
// re-warms every frame and leaves the chassis-driving derivatives pinned at zero
// forever, while engine and transmission are unaffected.
//
// This is deliberately asserted so the behaviour is visible and any future change
// to the gap gate (e.g. learning the steady stride instead of hardcoding +1) is a
// conscious edit that updates this test. On device, the same condition shows up as
// kin_gap_resets climbing every frame with a steady kin_last_gap_delta (see the
// haptic latency monitor log in app.go).
func TestUpdateSuppressesChassisOnNonUnitStride(t *testing.T) {
	t.Parallel()

	state := runStream(t, 2, 8)

	require.Zero(t, state.Current.ResolvedTransJerk,
		"non-unit stride currently suppresses chassis translational jerk")
	require.Zero(t, state.Current.ResolvedTransSnap,
		"non-unit stride currently suppresses chassis translational snap")
	require.Zero(t, state.Current.ResolvedRotJerk,
		"non-unit stride currently suppresses chassis rotational jerk")
	require.NotZero(t, state.GapResets,
		"every non-unit-stride frame trips the gap gate")
}
