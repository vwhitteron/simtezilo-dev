package tuneassist //nolint:testpackage // reaches the unexported validPulseWindow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// TestValidPulseWindow checks that a pulse frequency pair must keep its minimum
// below its maximum, and that a lone half is accepted.
func TestValidPulseWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tuning haptics.Tuning
		want   bool
	}{
		{name: "open window", tuning: haptics.Tuning{PulseMinFrequencyHz: 16, PulseMaxFrequencyHz: 60}, want: true},
		{name: "equal limits", tuning: haptics.Tuning{PulseMinFrequencyHz: 40, PulseMaxFrequencyHz: 40}, want: false},
		{name: "inverted limits", tuning: haptics.Tuning{PulseMinFrequencyHz: 70, PulseMaxFrequencyHz: 60}, want: false},
		{name: "minimum only", tuning: haptics.Tuning{PulseMinFrequencyHz: 70}, want: true},
		{name: "neither half", tuning: haptics.Tuning{}, want: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			tuning := testCase.tuning

			// Act
			got := validPulseWindow(tuning)

			// Assert
			assert.Equal(t, testCase.want, got)
		})
	}
}
