package kinematics

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// frameFactor is 1/windowSeconds for a contiguous 60 Hz frame, matching what
// State.Update passes the gate.
const frameFactor = 60.0

// feed runs a sequence of raw velocities through one gate at the given ground speed
// and returns the per-frame outputs.
func feed(g *nyquistGate, vels []models.Vector, speedMps float64) []models.Vector {
	outs := make([]models.Vector, len(vels))
	for i, v := range vels {
		outs[i] = g.filter(v, frameFactor, speedMps)
	}

	return outs
}

// rippleVelocities builds a monotonically increasing velocity along Z whose
// increments alternate big/small every frame — the fs/2 cadence artefact the gate
// exists to suppress.
func rippleVelocities(frames int) []models.Vector {
	vels := make([]models.Vector, frames)

	var z float32
	for i := range frames {
		vels[i] = models.Vector{Z: z}
		if i%2 == 0 {
			z += 0.09 // big step
		} else {
			z += 0.01 // near-repeat
		}
	}

	return vels
}

func TestNyquistGateEngagesAndSuppressesRipple(t *testing.T) {
	t.Parallel()

	g := &nyquistGate{}
	vels := rippleVelocities(16)
	outs := feed(g, vels, 0) // low speed: gate fully active

	// Early frames, before the alternation run reaches the engage threshold, pass
	// the raw velocity through untouched.
	require.Equal(t, vels[1], outs[1], "gate must not engage before the run is established")

	// Late in a sustained ripple the gate is engaged, so it returns the two-tap
	// average of the current and previous raw velocity.
	last := len(vels) - 1
	wantMean := (vels[last].Z + vels[last-1].Z) / 2
	require.InDelta(t, wantMean, outs[last].Z, 1e-6, "engaged gate must emit the two-tap average")

	// The point of that average: the acceleration it yields is near-constant, so the
	// jerk the chassis pulse consumes collapses. Compare the raw jerk against the
	// filtered jerk over the engaged tail.
	rawJerk := jerkOf(vels[last-2:], frameFactor)
	filteredJerk := jerkOf(outs[last-2:], frameFactor)
	require.Less(t, filteredJerk, rawJerk*0.1, "filtered jerk must be an order of magnitude below raw")
}

func TestNyquistGatePassesOneShotImpact(t *testing.T) {
	t.Parallel()

	g := &nyquistGate{}

	// Smooth acceleration: constant velocity increments give constant acceleration
	// magnitude, so the jerk sits in the deadzone and the gate never engages.
	vels := make([]models.Vector, 0, 8)

	var z float32
	for range 6 {
		vels = append(vels, models.Vector{Z: z})
		z += 0.05
	}

	// A single large step — a wall impact — arrives on the next frame.
	vels = append(vels, models.Vector{Z: z + 0.5})

	outs := feed(g, vels, 0) // low speed: gate fully active

	// The smooth run leaves the gate disengaged...
	require.Equal(t, vels[4], outs[4], "steady acceleration must not engage the gate")
	// ...so the impact frame is delivered at full magnitude, not halved by averaging.
	require.Equal(t, vels[len(vels)-1], outs[len(outs)-1], "a one-shot impact must pass through unfiltered")
}

func TestNyquistGateDisabledAtRacingSpeed(t *testing.T) {
	t.Parallel()

	// The same sustained fs/2 ripple that the gate suppresses at low speed must pass
	// through untouched at racing speed: above nyquistGateZeroSpeedMps the detector
	// would only trip intermittently and chatter into jerk/snap spikes, so the gate
	// is disabled and the raw velocity (and its dynamic range) is preserved.
	g := &nyquistGate{}
	vels := rippleVelocities(16)
	outs := feed(g, vels, nyquistGateZeroSpeedMps+5)

	for i, v := range vels {
		require.Equal(t, v, outs[i], "gate must pass raw velocity at racing speed (frame %d)", i)
	}
}

func TestNyquistGateSpeedInfluenceRamp(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1.0, nyquistGateSpeedInfluence(0), "full influence at a standstill")
	require.Equal(t, 1.0, nyquistGateSpeedInfluence(nyquistGateFullSpeedMps), "full influence up to the low threshold")
	require.Equal(t, 0.0, nyquistGateSpeedInfluence(nyquistGateZeroSpeedMps), "no influence at the high threshold")
	require.Equal(t, 0.0, nyquistGateSpeedInfluence(200), "no influence at racing speed")

	mid := (nyquistGateFullSpeedMps + nyquistGateZeroSpeedMps) / 2
	require.InDelta(t, 0.5, nyquistGateSpeedInfluence(mid), 1e-9, "half influence midway through the ramp")
}

// jerkOf returns the magnitude of the last per-frame change in acceleration
// magnitude across the supplied (>=3) velocities, using the given accel factor.
func jerkOf(vels []models.Vector, accelFactor float64) float64 {
	n := len(vels)
	a1 := magnitude(delta(vels[n-2], vels[n-3]), accelFactor)
	a2 := magnitude(delta(vels[n-1], vels[n-2]), accelFactor)

	j := a2 - a1
	if j < 0 {
		return -j
	}

	return j
}

func delta(a, b models.Vector) models.Vector {
	return models.Vector{X: a.X - b.X, Y: a.Y - b.Y, Z: a.Z - b.Z}
}

func magnitude(v models.Vector, factor float64) float64 {
	m := float64(v.Z) * factor // single-axis fixtures keep this simple
	if m < 0 {
		return -m
	}

	return m
}
