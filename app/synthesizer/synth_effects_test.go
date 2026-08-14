package synthesizer_test

import (
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// softKneeThreshold mirrors the unexported constant of the same name in
// synth_utils.go (0.7): the level below which softKnee is bit-exact
// transparent. It cannot be imported from this external test package, so it
// is pinned here and the two must be kept in step.
const softKneeThreshold = 0.7

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

// TestGenerateGearShiftSample_PeakOverdrivesTheKnee guards the prominence the
// effect depends on, and the regression that established it.
//
// PlayEffect scales this sample by a magnitude in [gain floor, 1.0] and the result
// is summed into the buffer through softKnee, so overdriving the knee is what makes
// a shift dominate whatever the chassis and road channels are already playing: the
// pulse sets the operating point and softCombine leaves everything overlapping it as
// the locally-reduced slope.
//
// Reducing the peak to 0.9 to keep the magnitude range out of compression was tried
// and reverted. It did recover the range on paper, but multiple Group 1 cars and a
// Super Formula were reported as having essentially no shift feedback in either
// direction, because the pulse no longer stood out from the mix. The loss of
// prominence dwarfed the dynamic range it bought, so up/down contrast has to come
// from somewhere that does not trade prominence away.
func TestGenerateGearShiftSample_PeakOverdrivesTheKnee(t *testing.T) {
	t.Parallel()

	bank := synthesizer.NewEffectsSampleBank()
	sample := bank.GetSample("gearShift", effectsBaseSampleRateHz)

	peak := 0.0
	for _, s := range sample.Samples() {
		if abs := math.Abs(s); abs > peak {
			peak = abs
		}
	}

	if peak <= softKneeThreshold {
		t.Fatalf("gearShift sample peak %v is at or below softKneeThreshold %v: the pulse no longer overdrives the "+
			"knee, so a shift stops standing out against concurrent chassis and road content", peak, softKneeThreshold)
	}
}

// gearShiftPulseSettings is the full sharp-to-heavy range GearShiftPulseShape (in
// the app package) actually maps a learned duration to: the sharp end, the heavy
// end, and the exact midpoint a duration of 8 frames produces.
var gearShiftPulseSettings = []struct {
	name          string
	pulseHz       float64
	lengthSeconds float64
}{
	{"sharp", 40, 0.080},
	{"midpoint", 31, 0.100},
	{"heavy", 22, 0.120},
}

// TestGenerateGearShiftSample_PeakOverdrivesTheKneeAcrossRange extends
// TestGenerateGearShiftSample_PeakOverdrivesTheKnee to every waveform a vehicle can
// actually be given, not just the default: the sharp, heavy and midpoint settings
// gearShiftPulseShape produces. The super-unity peak is load-bearing at every one of
// them, not merely at the default, because SetGearShiftPulse re-renders the
// waveform per vehicle and a shift must dominate the mix regardless of which
// gearbox it came from.
func TestGenerateGearShiftSample_PeakOverdrivesTheKneeAcrossRange(t *testing.T) {
	t.Parallel()

	for _, setting := range gearShiftPulseSettings {
		t.Run(setting.name, func(t *testing.T) {
			t.Parallel()

			bank := synthesizer.NewEffectsSampleBank()
			bank.SetGearShiftPulse(setting.pulseHz, setting.lengthSeconds)

			sample := bank.GetSample(synthesizer.GearShiftEffectName, effectsBaseSampleRateHz)

			peak := 0.0
			for _, s := range sample.Samples() {
				if abs := math.Abs(s); abs > peak {
					peak = abs
				}
			}

			if peak <= softKneeThreshold {
				t.Fatalf("gearShift sample at %v Hz / %v s has peak %v, at or below softKneeThreshold %v",
					setting.pulseHz, setting.lengthSeconds, peak, softKneeThreshold)
			}
		})
	}
}

// TestGenerateGearShiftSample_StartsAndEndsNearZero checks that the rendered pulse
// has no discontinuity at either end, at every setting the pulse shape actually
// produces. There is no DC blocker downstream of this sample, so a jump at the
// boundary would click on every gear change.
func TestGenerateGearShiftSample_StartsAndEndsNearZero(t *testing.T) {
	t.Parallel()

	const nearZero = 0.05

	for _, setting := range gearShiftPulseSettings {
		t.Run(setting.name, func(t *testing.T) {
			t.Parallel()

			bank := synthesizer.NewEffectsSampleBank()
			bank.SetGearShiftPulse(setting.pulseHz, setting.lengthSeconds)

			sample := bank.GetSample(synthesizer.GearShiftEffectName, effectsBaseSampleRateHz)
			samples := sample.Samples()

			if len(samples) == 0 {
				t.Fatal("expected a non-empty gearShift sample")
			}

			if abs := math.Abs(samples[0]); abs > nearZero {
				t.Fatalf("gearShift sample at %v Hz / %v s starts at %v, not near zero",
					setting.pulseHz, setting.lengthSeconds, samples[0])
			}

			if abs := math.Abs(samples[len(samples)-1]); abs > nearZero {
				t.Fatalf("gearShift sample at %v Hz / %v s ends at %v, not near zero",
					setting.pulseHz, setting.lengthSeconds, samples[len(samples)-1])
			}
		})
	}
}

// TestSetGearShiftPulse_DropsStaleResampledEntries is the regression
// SetGearShiftPulse's comment on the per-rate map exists to prevent: the
// synthesizer runs at 8 kHz by default, so a lazily resampled 8 kHz copy of the
// *previous* waveform left in the cache after a re-render would be the one actually
// played, silently overriding the new pulse.
func TestSetGearShiftPulse_DropsStaleResampledEntries(t *testing.T) {
	t.Parallel()

	const playbackRateHz = 8000

	bank := synthesizer.NewEffectsSampleBank()

	// Populate the 8 kHz resample cache under the default waveform.
	beforeSample := bank.GetSample(synthesizer.GearShiftEffectName, playbackRateHz)
	before := slices.Clone(beforeSample.Samples())

	// Re-render at a clearly different frequency and length.
	bank.SetGearShiftPulse(22, 0.120)

	afterSample := bank.GetSample(synthesizer.GearShiftEffectName, playbackRateHz)
	after := afterSample.Samples()

	if slices.Equal(before, after) {
		t.Fatal("GetSample at 8 kHz returned the stale pre-SetGearShiftPulse waveform")
	}
}

// TestGearShiftPulse_ConcurrentAccessIsRaceFree exercises GetSample and
// SetGearShiftPulse from multiple goroutines together, since both the app main
// loop and the pit radio's background goroutine call GetSample while a shift
// re-render can land from the main loop at any time. Run with -race.
func TestGearShiftPulse_ConcurrentAccessIsRaceFree(t *testing.T) {
	t.Parallel()

	bank := synthesizer.NewEffectsSampleBank()

	const (
		goroutines = 8
		iterations = 50
	)

	var waitGroup sync.WaitGroup

	for range goroutines {
		waitGroup.Go(func() {
			for range iterations {
				bank.GetSample(synthesizer.GearShiftEffectName, effectsBaseSampleRateHz)
				bank.GetSample(synthesizer.GearShiftEffectName, 8000)
			}
		})
	}

	waitGroup.Go(func() {
		for i := range iterations {
			pulseHz := gearShiftPulseSettings[i%len(gearShiftPulseSettings)].pulseHz
			lengthSeconds := gearShiftPulseSettings[i%len(gearShiftPulseSettings)].lengthSeconds

			bank.SetGearShiftPulse(pulseHz, lengthSeconds)
		}
	})

	waitGroup.Wait()
}
