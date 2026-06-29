package synthesizer_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// effectsBaseSampleRateHz is the rate at which effects are pre-rendered, so
// GetSample returns the cached sample directly without resampling.
const effectsBaseSampleRateHz = 32000

// TestWriteScaled_DoesNotMutateInput verifies the non-mutating write path: a
// scaled write must not modify the caller's slice (regression for the effects
// copy removal in EffectsSampleBank.GetSample).
func TestWriteScaled_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	buffer := synthesizer.NewAdaptiveBuffer(time.Second, 48000)

	input := []float64{0.2, -0.4, 0.6, -0.8}
	original := slices.Clone(input)

	buffer.WriteScaled(input, 0.5, 0, true)

	if !slices.Equal(input, original) {
		t.Fatalf("WriteScaled mutated its input: got %v, want %v", input, original)
	}
}

// TestGetSample_StableAcrossRepeatedWrites writes a cached effect sample through
// the non-mutating scaled write path several times and confirms the cached data
// is unchanged. With the defensive copy in GetSample removed, GetSample hands
// out the shared cached slice, so a mutating write would attenuate the effect on
// every play.
func TestGetSample_StableAcrossRepeatedWrites(t *testing.T) {
	t.Parallel()

	bank := synthesizer.NewEffectsSampleBank()

	baseline := bank.GetSample("gearShift", effectsBaseSampleRateHz)

	want := slices.Clone(baseline.Samples())
	if len(want) == 0 {
		t.Fatal("expected non-empty gearShift sample")
	}

	buffer := synthesizer.NewAdaptiveBuffer(time.Second, effectsBaseSampleRateHz)

	for range 5 {
		sample := bank.GetSample("gearShift", effectsBaseSampleRateHz)
		buffer.WriteScaled(sample.Samples(), 0.5, 0, false)

		current := bank.GetSample("gearShift", effectsBaseSampleRateHz)
		if !slices.Equal(current.Samples(), want) {
			t.Fatal("cached effect sample changed after a scaled write")
		}
	}
}
