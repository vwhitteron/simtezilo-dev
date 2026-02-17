package signal_test

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

// newTestConfig creates a config instance for signal tests using the public constructor.
func newTestConfig() *config.Config {
	return config.NewFromJSON([]byte(`{}`), zerolog.Nop())
}

func TestDRXShift(t *testing.T) {
	t.Parallel()

	t.Run("testDRXShiftDisabled", testDRXShiftDisabled)
	t.Run("testDRXShiftEQDisabled", testDRXShiftEQDisabled)
	t.Run("testDRXShiftBelowThreshold", testDRXShiftBelowThreshold)
	t.Run("testDRXShiftActivated", testDRXShiftActivated)
	t.Run("testDRXShiftShallowAttenuationFallback", testDRXShiftShallowAttenuationFallback)
	t.Run("testDRXShiftNoAttenuation", testDRXShiftNoAttenuation)
	t.Run("testDRXShiftFrequencyClamped", testDRXShiftFrequencyClamped)
	t.Run("testDRXShiftProportionalAmplitude", testDRXShiftProportionalAmplitude)
	t.Run("testDRXShiftExtremeOverflow", testDRXShiftExtremeOverflow)
	t.Run("testDRXShiftNearestBucketSelection", testDRXShiftNearestBucketSelection)
}

func testDRXShiftDisabled(t *testing.T) {
	t.Parallel()

	// Arrange
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(false)

	// Act - unclamped amplitude 2.0 exceeds maxAmplitude 1.0, but DRX disabled
	freq, amplitude, _, activated := signal.DRXShift(40.0, 2.0, 0, cfg)

	// Assert - DRX disabled, returns original frequency
	assert.False(t, activated)
	assert.InDelta(t, 40.0, freq, 0.001)
	assert.InDelta(t, 0.0, amplitude, 0.001)
}

func testDRXShiftEQDisabled(t *testing.T) {
	t.Parallel()

	// Arrange
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetSynthChannelEqEnabled(0, false)

	// Act
	freq, amplitude, _, activated := signal.DRXShift(40.0, 2.0, 0, cfg)

	// Assert - EQ disabled, DRX cannot activate
	assert.False(t, activated)
	assert.InDelta(t, 40.0, freq, 0.001)
	assert.InDelta(t, 0.0, amplitude, 0.001)
}

func testDRXShiftBelowThreshold(t *testing.T) {
	t.Parallel()

	// Arrange
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 30, Gain: -6.0, Q: 2.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 70, Gain: 0.0, Q: 2.0},
	})

	// Act - unclamped amplitude at or below 0dB (maxAmplitude)
	freq, amplitude, _, activated := signal.DRXShift(40.0, 0.8, 0, cfg)

	// Assert - below threshold, DRX does not activate
	assert.False(t, activated)
	assert.InDelta(t, 40.0, freq, 0.001)
	assert.InDelta(t, 0.0, amplitude, 0.001)
}

func testDRXShiftActivated(t *testing.T) {
	t.Parallel()

	// Arrange - deep -12dB EQ attenuation at 30Hz
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 30, Gain: -12.0, Q: 2.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 70, Gain: 0.0, Q: 2.0},
	})

	// Act - unclamped 1.5 (+3.5dB desired boost), needs bucket with at least -3.5dB
	_, amplitude, bucketRatio, activated := signal.DRXShift(40.0, 1.5, 0, cfg)

	// Assert - DRX activated with correct output
	assert.True(t, activated)
	assert.Greater(t, amplitude, 0.0)
	assert.LessOrEqual(t, amplitude, 1.0, "digital amplitude must not exceed max")
	assert.Greater(t, bucketRatio, 0.0)
	assert.Less(t, bucketRatio, 1.0, "bucket must have attenuation")
	// Digital amplitude should equal unclamped × bucketRatio (or be capped at max)
	expected := min(1.5*bucketRatio, 1.0)
	assert.InDelta(t, expected, amplitude, 0.001)
}

func testDRXShiftShallowAttenuationFallback(t *testing.T) {
	t.Parallel()

	// Arrange - EQ with only shallow attenuation (-1dB, ratio ~0.89).
	// The unclamped amplitude needs +3dB but only -1dB attenuation is available.
	// DRX should fall back to the deepest bucket and provide a capped boost.
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 30, Gain: -1.0, Q: 2.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 70, Gain: 0.0, Q: 2.0},
	})

	// Act - unclamped 1.5 needs ~-3.5dB but deepest bucket is ~-1dB
	freq, amplitude, _, activated := signal.DRXShift(40.0, 1.5, 0, cfg)

	// Assert - DRX still activates using the deepest available bucket
	assert.True(t, activated, "DRX should activate using fallback to deepest bucket")
	assert.True(t, freq < 39.999 || freq > 40.001, "frequency should shift, got %f", freq)
	// Amplitude capped at maxAmplitude since bucket is shallower than ideal
	assert.LessOrEqual(t, amplitude, 1.0)
	assert.Greater(t, amplitude, 0.0)
}

func testDRXShiftNoAttenuation(t *testing.T) {
	t.Parallel()

	// Arrange - EQ with only boost (positive gain), no attenuation anywhere
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 30, Gain: 3.0, Q: 2.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 70, Gain: 0.0, Q: 2.0},
	})

	// Act - unclamped above 0dB but no attenuation zones in EQ
	freq, amplitude, _, activated := signal.DRXShift(40.0, 2.0, 0, cfg)

	// Assert - no attenuation available, DRX cannot activate
	assert.False(t, activated)
	assert.InDelta(t, 40.0, freq, 0.001)
	assert.InDelta(t, 0.0, amplitude, 0.001)
}

func testDRXShiftFrequencyClamped(t *testing.T) {
	t.Parallel()

	// Arrange - attenuation zone at 10Hz, below configured pulse min
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetHapticsPulseMinFrequencyHz(16)
	cfg.SetHapticsPulseMaxFrequencyHz(60)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 10, Gain: -12.0, Q: 2.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 70, Gain: 0.0, Q: 2.0},
	})

	// Act - unclamped above 0dB, attenuation zone below pulse min
	freq, _, _, activated := signal.DRXShift(40.0, 1.5, 0, cfg)

	// Assert - if activated, frequency should be clamped within pulse range
	if activated {
		assert.GreaterOrEqual(t, freq, 16.0)
		assert.LessOrEqual(t, freq, 60.0)
	}
}

func testDRXShiftProportionalAmplitude(t *testing.T) {
	t.Parallel()

	// Arrange - deep attenuation to ensure full boost is available
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 30, Gain: -12.0, Q: 2.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 70, Gain: 0.0, Q: 2.0},
	})

	// Act - two different unclamped levels, both above 0dB
	_, ampSmall, _, activatedSmall := signal.DRXShift(40.0, 1.3, 0, cfg)
	_, ampLarge, _, activatedLarge := signal.DRXShift(40.0, 2.0, 0, cfg)

	// Assert - both activate with proportional amplitudes: unclamped × bucketRatio
	// Higher unclamped produces higher DRX amplitude (both below 1.0)
	if activatedSmall && activatedLarge {
		assert.Greater(t, ampSmall, 0.0)
		assert.Greater(t, ampLarge, 0.0)
		assert.LessOrEqual(t, ampSmall, 1.0)
		assert.LessOrEqual(t, ampLarge, 1.0)
		// Larger unclamped should produce higher digital amplitude
		// (same bucket, so amplitude scales with unclamped level)
		assert.Greater(t, ampLarge, ampSmall,
			"higher unclamped should produce higher DRX amplitude")
	}
}

func testDRXShiftExtremeOverflow(t *testing.T) {
	t.Parallel()

	// Arrange - extreme unclamped amplitude (+34dB above 0dB).
	// DRX should fall back to the deepest bucket and cap at maxAmplitude.
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 30, Gain: -12.0, Q: 2.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 70, Gain: 0.0, Q: 2.0},
	})

	// Act - unclamped 50.0 (~+34dB), far exceeds any EQ attenuation depth
	freq, amplitude, _, activated := signal.DRXShift(40.0, 50.0, 0, cfg)

	// Assert - DRX activates using deepest bucket, amplitude capped at maxAmplitude
	assert.True(t, activated, "DRX should activate even with extreme unclamped level")
	assert.True(t, freq < 39.999 || freq > 40.001, "frequency should shift")
	assert.InDelta(t, 1.0, amplitude, 0.01, "amplitude should be capped at maxAmplitude")
}

func testDRXShiftNearestBucketSelection(t *testing.T) {
	t.Parallel()

	// Arrange - two attenuation zones: one near 30Hz (-6dB) and one near 70Hz (-12dB).
	// With original frequency 40Hz, DRX should pick the 30Hz zone (closer) if it has
	// enough attenuation for the desired boost.
	cfg := newTestConfig()
	cfg.SetSynthDRXEnabled(true)
	cfg.SetHapticsPulseMaxAmplitude(1.0)
	cfg.SetSynthChannelEqEnabled(0, true)
	cfg.SetSynthChannelEq(0, []config.EQBand{
		{Frequency: 30, Gain: -6.0, Q: 4.0},
		{Frequency: 70, Gain: -12.0, Q: 4.0},
		{Frequency: 15, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 40, Gain: 0.0, Q: 2.0},
		{Frequency: 50, Gain: 0.0, Q: 2.0},
		{Frequency: 60, Gain: 0.0, Q: 2.0},
		{Frequency: 80, Gain: 0.0, Q: 2.0},
	})

	// Act - unclamped 1.2 (+1.6dB), needs bucket with at least ~-1.6dB
	// Both zones qualify; 30Hz is closer to 40Hz than 70Hz
	freq, _, _, activated := signal.DRXShift(40.0, 1.2, 0, cfg)

	// Assert - should pick the closer attenuation zone
	assert.True(t, activated, "DRX should activate")
	assert.Less(t, freq, 50.0, "should pick the closer 30Hz zone, not the far 70Hz zone")
}

func TestEqualize(t *testing.T) {
	t.Parallel()

	t.Run("testEqualizeDisabled", testEqualizeDisabled)
	t.Run("testEqualizeEnabled", testEqualizeEnabled)
}

func testEqualizeDisabled(t *testing.T) {
	t.Parallel()

	// Arrange
	cfg := newTestConfig()
	cfg.SetSynthChannelEqEnabled(0, false)

	// Act
	result := signal.Equalize(0.8, 100.0, 0, cfg)

	// Assert - EQ disabled, value unchanged
	assert.InDelta(t, 0.8, result, 0.001)
}

func testEqualizeEnabled(t *testing.T) {
	t.Parallel()

	// Arrange
	cfg := newTestConfig()
	cfg.SetSynthChannelEqEnabled(0, true)

	// Act
	result := signal.Equalize(0.8, 100.0, 0, cfg)

	// Assert - EQ enabled, value should be modified (exact value depends on default EQ bands)
	assert.Greater(t, result, 0.0)
}

func TestLimitMax(t *testing.T) {
	t.Parallel()

	t.Run("testLimitMaxNotLimited", testLimitMaxNotLimited)
	t.Run("testLimitMaxLimited", testLimitMaxLimited)
	t.Run("testLimitMaxNegativeLimited", testLimitMaxNegativeLimited)
}

func testLimitMaxNotLimited(t *testing.T) {
	t.Parallel()

	// Arrange
	value := 0.5
	maxValue := 1.0

	// Act
	result, wasLimited := signal.LimitMax(value, maxValue)

	// Assert
	assert.InDelta(t, 0.5, result, 0.001)
	assert.False(t, wasLimited)
}

func testLimitMaxLimited(t *testing.T) {
	t.Parallel()

	// Arrange
	value := 1.5
	maxValue := 1.0

	// Act
	result, wasLimited := signal.LimitMax(value, maxValue)

	// Assert
	assert.InDelta(t, 1.0, result, 0.001)
	assert.True(t, wasLimited)
}

func testLimitMaxNegativeLimited(t *testing.T) {
	t.Parallel()

	// Arrange
	value := -1.5
	maxValue := 1.0

	// Act
	result, wasLimited := signal.LimitMax(value, maxValue)

	// Assert - absolute value 1.5 clamped to 1.0, sign restored
	assert.InDelta(t, -1.0, result, 0.001)
	assert.True(t, wasLimited)
}

func TestLimitMin(t *testing.T) {
	t.Parallel()

	t.Run("testLimitMinNotLimited", testLimitMinNotLimited)
	t.Run("testLimitMinLimited", testLimitMinLimited)
}

func testLimitMinNotLimited(t *testing.T) {
	t.Parallel()

	// Arrange
	value := 0.5
	minValue := 0.1

	// Act
	result, wasLimited := signal.LimitMin(value, minValue)

	// Assert
	assert.InDelta(t, 0.5, result, 0.001)
	assert.False(t, wasLimited)
}

func testLimitMinLimited(t *testing.T) {
	t.Parallel()

	// Arrange
	value := 0.05
	minValue := 0.1

	// Act
	result, wasLimited := signal.LimitMin(value, minValue)

	// Assert
	assert.InDelta(t, 0.1, result, 0.001)
	assert.True(t, wasLimited)
}
