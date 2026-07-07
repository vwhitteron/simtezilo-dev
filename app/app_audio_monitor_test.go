package app

import (
	"testing"
	"time"
)

// TestTickerPeriodExact locks in the fix for the integer-division ticker bug:
// time.Second/rate must yield the true period, not the millisecond-truncated
// value that ran 120 Hz at 8 ms (125 Hz) and 60 Hz at 16 ms (62.5 Hz).
func TestTickerPeriodExact(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		rate int
		want time.Duration
		// buggy is the old (1000/rate)*time.Millisecond value we must not equal.
		buggy time.Duration
	}{
		{hapticFrameRate, 8333333 * time.Nanosecond, 8 * time.Millisecond},
		{telemetryFrameRate, 16666666 * time.Nanosecond, 16 * time.Millisecond},
		{engineHapticFrameRate, 33333333 * time.Nanosecond, 33 * time.Millisecond},
	}

	for _, testCase := range testCases {
		got := tickerPeriod(testCase.rate)
		if got != testCase.want {
			t.Errorf("tickerPeriod(%d) = %v, want %v", testCase.rate, got, testCase.want)
		}

		if got == testCase.buggy {
			t.Errorf("tickerPeriod(%d) = %v, must not equal the truncated %v", testCase.rate, got, testCase.buggy)
		}
	}
}
