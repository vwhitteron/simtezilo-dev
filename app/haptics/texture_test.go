package haptics

import (
	"math"
	"testing"

	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

func TestTextureSpeedAmplitude(t *testing.T) {
	t.Parallel()

	// Muted at and below the standstill gate.
	for _, speed := range []float64{0, textureMinSpeedMps - 0.5, textureMinSpeedMps} {
		if got := textureSpeedAmplitude(speed); got != 0 {
			t.Fatalf("textureSpeedAmplitude(%.2f) = %.4f, want 0", speed, got)
		}
	}

	// Rises monotonically with speed above the gate.
	prev := textureSpeedAmplitude(textureMinSpeedMps)
	for speed := textureMinSpeedMps + 1; speed <= textureAmplitudeSpeedRefMps*1.5; speed += 2 {
		got := textureSpeedAmplitude(speed)
		if got <= prev {
			t.Fatalf("not monotonic at %.1f m/s: %.4f <= %.4f", speed, got, prev)
		}

		prev = got
	}

	// Soft-caps near unity by the reference speed and never exceeds 1.
	if got := textureSpeedAmplitude(textureAmplitudeSpeedRefMps); got < 0.9 || got > 1 {
		t.Fatalf("at reference speed: got %.4f, want in [0.9, 1]", got)
	}

	if got := textureSpeedAmplitude(textureAmplitudeSpeedRefMps * 5); got > 1 {
		t.Fatalf("far beyond reference speed: got %.4f, want <= 1", got)
	}
}

// TestSurfaceRumbleOrdering asserts loose surfaces are louder and coarser than tarmac,
// and that unknown falls back to the tarmac profile.
func TestSurfaceRumbleOrdering(t *testing.T) {
	t.Parallel()

	tarmacLevel, tarmacCoarse := surfaceRumble(models.SurfaceTypeTarmac)
	dirtLevel, dirtCoarse := surfaceRumble(models.SurfaceTypeDirt)

	if dirtLevel <= tarmacLevel {
		t.Fatalf("dirt level %.2f should exceed tarmac %.2f", dirtLevel, tarmacLevel)
	}

	if dirtCoarse >= tarmacCoarse {
		t.Fatalf("dirt coarseness %.2f should be lower (coarser) than tarmac %.2f", dirtCoarse, tarmacCoarse)
	}

	unknownLevel, unknownCoarse := surfaceRumble(models.SurfaceTypeUnknown)
	if unknownLevel != tarmacLevel || unknownCoarse != tarmacCoarse {
		t.Fatalf("unknown %.2f/%.2f should match tarmac %.2f/%.2f",
			unknownLevel, unknownCoarse, tarmacLevel, tarmacCoarse)
	}
}

// TestAggregateSurfaceAverages confirms partial off-track (two corners) yields a level
// between all-tarmac and all-dirt, and all-same corners return that surface's values.
func TestAggregateSurfaceAverages(t *testing.T) {
	t.Parallel()

	allTarmac := models.CornerSetGeneric[models.SurfaceType]{
		FrontLeft: models.SurfaceTypeTarmac, FrontRight: models.SurfaceTypeTarmac,
		RearLeft: models.SurfaceTypeTarmac, RearRight: models.SurfaceTypeTarmac,
	}
	allDirt := models.CornerSetGeneric[models.SurfaceType]{
		FrontLeft: models.SurfaceTypeDirt, FrontRight: models.SurfaceTypeDirt,
		RearLeft: models.SurfaceTypeDirt, RearRight: models.SurfaceTypeDirt,
	}
	twoDirt := models.CornerSetGeneric[models.SurfaceType]{
		FrontLeft: models.SurfaceTypeDirt, FrontRight: models.SurfaceTypeDirt,
		RearLeft: models.SurfaceTypeTarmac, RearRight: models.SurfaceTypeTarmac,
	}

	tarmacLevel, _ := aggregateSurface(allTarmac)
	dirtLevel, _ := aggregateSurface(allDirt)
	mixLevel, _ := aggregateSurface(twoDirt)

	if want, _ := surfaceRumble(models.SurfaceTypeDirt); math.Abs(dirtLevel-want) > 1e-9 {
		t.Fatalf("all-dirt level %.4f, want %.4f", dirtLevel, want)
	}

	if mixLevel <= tarmacLevel || mixLevel >= dirtLevel {
		t.Fatalf("two-corner mix %.4f should sit between tarmac %.4f and dirt %.4f",
			mixLevel, tarmacLevel, dirtLevel)
	}

	if want := (tarmacLevel + dirtLevel) / 2; math.Abs(mixLevel-want) > 1e-9 {
		t.Fatalf("two-corner mix %.4f should be the mean %.4f", mixLevel, want)
	}
}

func TestTextureCutoffHz(t *testing.T) {
	t.Parallel()

	const minHz, maxHz = 25.0, 45.0

	if got := textureCutoffHz(0, minHz, maxHz); math.Abs(got-minHz) > 1e-9 {
		t.Fatalf("at rest: got %.4f, want %.4f", got, minHz)
	}

	if got := textureCutoffHz(textureCutoffSpeedRefMps, minHz, maxHz); math.Abs(got-maxHz) > 1e-9 {
		t.Fatalf("at reference speed: got %.4f, want %.4f", got, maxHz)
	}

	// Saturates at the top of the band beyond the reference speed.
	if got := textureCutoffHz(textureCutoffSpeedRefMps*3, minHz, maxHz); math.Abs(got-maxHz) > 1e-9 {
		t.Fatalf("beyond reference speed: got %.4f, want %.4f", got, maxHz)
	}

	// Monotonic and within band mid-range.
	mid := textureCutoffHz(textureCutoffSpeedRefMps/2, minHz, maxHz)
	if mid <= minHz || mid >= maxHz {
		t.Fatalf("midrange %.4f should sit strictly inside the band (%.1f, %.1f)", mid, minHz, maxHz)
	}
}

func TestEnsureTextureStateLenGrowsAndPreserves(t *testing.T) {
	t.Parallel()

	states := ensureTextureStateLen(nil, 2)
	if len(states) != 2 {
		t.Fatalf("len = %d, want 2", len(states))
	}

	states[0].rng = 123
	states[1].prevAmp = 0.5

	grown := ensureTextureStateLen(states, 4)
	if len(grown) != 4 {
		t.Fatalf("grown len = %d, want 4", len(grown))
	}

	if grown[0].rng != 123 || grown[1].prevAmp != 0.5 {
		t.Fatalf("existing state not preserved: %+v", grown[:2])
	}

	if grown[2] != (textureChannelState{}) || grown[3] != (textureChannelState{}) {
		t.Fatalf("new state should be zero: %+v", grown[2:])
	}
}

// TestTextureNoiseIsBroadband asserts the carrier is noise, not a tone: successive
// samples of the raw normalised noise change sign frequently (a sine would not).
func TestTextureNoiseDecorrelatesChannels(t *testing.T) {
	t.Parallel()

	// Two channels seeded independently must not produce identical PRNG streams.
	s0, s1 := textureSeed(0), textureSeed(1)
	if s0 == s1 {
		t.Fatalf("channel seeds collide: %d", s0)
	}

	same := 0

	for range 256 {
		s0 = textureNextRand(s0)

		s1 = textureNextRand(s1)
		if s0 == s1 {
			same++
		}
	}

	if same > 0 {
		t.Fatalf("channel PRNG streams overlapped %d times", same)
	}
}
