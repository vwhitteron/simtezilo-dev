package haptics //nolint:testpackage // reaches the unexported applyTuning directly

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/vwhitteron/simtezilo-dev/app/config"
)

// TestApplyTuningPulseWindow checks that a supplied pulse frequency window replaces
// the stored one, and that a zero half keeps its stored value.
func TestApplyTuningPulseWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tuning  Tuning
		wantMin float64
		wantMax float64
	}{
		{name: "both halves", tuning: Tuning{PulseMinFrequencyHz: 12, PulseMaxFrequencyHz: 90}, wantMin: 12, wantMax: 90},
		{name: "minimum only", tuning: Tuning{PulseMinFrequencyHz: 12}, wantMin: 12, wantMax: 60},
		{name: "neither half", tuning: Tuning{}, wantMin: 16, wantMax: 60},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			cfg := config.NewFromJSON([]byte(`{"haptics": {"pulseMinFrequencyHz": 16, "pulseMaxFrequencyHz": 60}}`), zerolog.Nop())

			// Act
			applyTuning(cfg, testCase.tuning)

			// Assert
			assert.InDelta(t, testCase.wantMin, cfg.GetHapticsPulseMinHz(), 1e-9)
			assert.InDelta(t, testCase.wantMax, cfg.GetHapticsPulseMaxHz(), 1e-9)
		})
	}
}
