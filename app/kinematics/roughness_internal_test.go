package kinematics

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// roughnessFramePeriod is the telemetry frame period used throughout these tests
// (59.94 Hz), matching the rate the envelope's fixed high-pass coefficient assumes.
const roughnessFramePeriod = 1.0 / 59.94

// stepCornersFreeFall advances a CornerSet by a fixed per-frame increment on all
// four corners, so a constant per-frame rise models a uniform (non-road) event like
// the whole car rising in free fall.
func stepCornersFreeFall(height models.CornerSet) models.CornerSet {
	const delta = 0.01

	return models.CornerSet{
		FrontLeft:  height.FrontLeft + delta,
		FrontRight: height.FrontRight + delta,
		RearLeft:   height.RearLeft + delta,
		RearRight:  height.RearRight + delta,
	}
}

// TestRoughnessEnvelopeAirborne models the wheels leaving the ground: suspension
// height rises at a constant rate (constant velocity, no road input), which the
// high-pass should reject so roughness settles near zero within a few frames of the
// window filling.
func TestRoughnessEnvelopeAirborne(t *testing.T) {
	t.Parallel()

	var env roughnessEnvelope

	height := models.CornerSet{}

	var settled float64

	for frame := range roughnessWindowFrames + 3 {
		next := stepCornersFreeFall(height)

		roughness, valid := env.update(next, height, roughnessFramePeriod, true)
		height = next

		if valid && frame >= roughnessWindowFrames {
			settled = roughness
		}
	}

	roadReference := sinusoidRoughness(t, 20.0, 0.020)

	require.Less(t, settled, roadReference/20, "constant-velocity (airborne) input should settle at least 20x below genuine road input")
}

// TestRoughnessEnvelopeBrakingDiveVsRoadInput models the difference between a slow
// body-attitude motion (braking dive, ~1 Hz) that must be rejected and genuine
// higher-frequency road input (~20 Hz) that must pass.
func TestRoughnessEnvelopeBrakingDiveVsRoadInput(t *testing.T) {
	t.Parallel()

	settledDive := sinusoidRoughness(t, 1.0, 0.020)
	settledRoad := sinusoidRoughness(t, 20.0, 0.020)

	require.Greater(t, settledRoad, settledDive*15, "20 Hz road input should read at least 15x above 1 Hz braking dive")
}

// sinusoidRoughness drives the envelope with a sinusoidal suspension height of the
// given frequency and amplitude and returns the settled roughness after several
// window lengths, once the sliding RMS has stabilised.
func sinusoidRoughness(t *testing.T, freqHz, amplitude float64) float64 {
	t.Helper()

	var env roughnessEnvelope

	height := models.CornerSet{}

	settled := 0.0
	total := roughnessWindowFrames * 8

	for frame := range total {
		time := float64(frame+1) * roughnessFramePeriod
		value := float32(amplitude * math.Sin(2*math.Pi*freqHz*time))

		next := models.CornerSet{
			FrontLeft: value, FrontRight: value,
			RearLeft: value, RearRight: value,
		}

		roughness, valid := env.update(next, height, roughnessFramePeriod, true)
		height = next

		if valid {
			settled = roughness
		}
	}

	return settled
}

// TestRoughnessEnvelopeGap models a telemetry sequence gap: the window is filled,
// then a single non-contiguous call must zero the state and report invalid, and
// re-warming afterwards must take roughnessMinFillFrames contiguous frames again.
func TestRoughnessEnvelopeGap(t *testing.T) {
	t.Parallel()

	var env roughnessEnvelope

	height := models.CornerSet{}
	for range roughnessWindowFrames + 5 {
		next := stepCornersFreeFall(height)
		_, _ = env.update(next, height, roughnessFramePeriod, true)
		height = next
	}

	next := stepCornersFreeFall(height)
	roughness, valid := env.update(next, height, roughnessFramePeriod, false)
	require.Zero(t, roughness)
	require.False(t, valid)

	height = next

	for frame := range roughnessMinFillFrames {
		next := stepCornersFreeFall(height)
		_, valid := env.update(next, height, roughnessFramePeriod, true)
		height = next

		if frame < roughnessMinFillFrames-1 {
			require.False(t, valid, "should still be invalid before roughnessMinFillFrames contiguous frames")
		} else {
			require.True(t, valid, "should be valid on the roughnessMinFillFrames'th contiguous frame")
		}
	}
}

// TestRoughnessEnvelopeZeroWindow models a degenerate frame period (e.g. a
// duplicate-sequence frame resolving to zero elapsed time), which must return
// invalid without producing NaN or Inf.
func TestRoughnessEnvelopeZeroWindow(t *testing.T) {
	t.Parallel()

	var env roughnessEnvelope

	height := models.CornerSet{FrontLeft: 1, FrontRight: 1, RearLeft: 1, RearRight: 1}
	roughness, valid := env.update(height, models.CornerSet{}, 0, true)

	require.Zero(t, roughness)
	require.False(t, valid)
	require.False(t, math.IsNaN(roughness))
	require.False(t, math.IsInf(roughness, 0))
}

// TestRoughnessEnvelopeNoSuspensionData models a telemetry format that carries no
// suspension data, where every corner reports a permanent zero height.
func TestRoughnessEnvelopeNoSuspensionData(t *testing.T) {
	t.Parallel()

	var env roughnessEnvelope

	roughness, valid := env.update(models.CornerSet{}, models.CornerSet{}, roughnessFramePeriod, true)

	require.Zero(t, roughness)
	require.False(t, valid)
}

// TestRoughnessEnvelopeWarmUp models the ring buffer filling from cold: the first
// roughnessMinFillFrames-1 calls must report invalid, and the roughnessMinFillFrames'th
// must report valid.
func TestRoughnessEnvelopeWarmUp(t *testing.T) {
	t.Parallel()

	var env roughnessEnvelope

	height := models.CornerSet{}

	for frame := 1; frame <= roughnessMinFillFrames; frame++ {
		next := stepCornersFreeFall(height)
		_, valid := env.update(next, height, roughnessFramePeriod, true)
		height = next

		if frame < roughnessMinFillFrames {
			require.Falsef(t, valid, "frame %d should still be invalid", frame)
		} else {
			require.Truef(t, valid, "frame %d (roughnessMinFillFrames) should be valid", frame)
		}
	}
}
