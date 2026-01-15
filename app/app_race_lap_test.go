package app_test

import (
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app"
)

func TestFormatDeltaTimeFormatsLapTimeDeltasCorrectly(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		delta    time.Duration
		expected string
	}{
		// Seconds range (>= 0.95s)
		{
			name:     "exactly 1 second",
			delta:    1 * time.Second,
			expected: "1 second",
		},
		{
			name:     "boundary case: 0.95s rounds to 1 second",
			delta:    950 * time.Millisecond,
			expected: "1 second",
		},
		{
			name:     "1.5 seconds",
			delta:    1500 * time.Millisecond,
			expected: "1.5 seconds",
		},
		{
			name:     "2.3 seconds",
			delta:    2300 * time.Millisecond,
			expected: "2.3 seconds",
		},
		{
			name:     "2.0 seconds exactly",
			delta:    2000 * time.Millisecond,
			expected: "2 seconds",
		},
		{
			name:     "10.5 seconds",
			delta:    10500 * time.Millisecond,
			expected: "10.5 seconds",
		},

		// Tenths range (0.095s to 0.949s)
		{
			name:     "0.948s is 0.9 seconds (avoids 10 tenths)",
			delta:    948 * time.Millisecond,
			expected: "0.9 seconds",
		},
		{
			name:     "0.5 seconds is 5 tenths",
			delta:    500 * time.Millisecond,
			expected: "5 tenths",
		},
		{
			name:     "0.1 seconds is 1 tenth",
			delta:    100 * time.Millisecond,
			expected: "1 tenth",
		},
		{
			name:     "boundary case: 0.095s rounds to 1 tenth",
			delta:    95 * time.Millisecond,
			expected: "1 tenth",
		},
		{
			name:     "0.9 seconds is 9 tenths",
			delta:    900 * time.Millisecond,
			expected: "9 tenths",
		},

		// Hundredths range (0.0095s to 0.0949s)
		{
			name:     "0.0948s is 1 tenth (avoids 10 hundredths)",
			delta:    time.Duration(94.8 * float64(time.Millisecond)),
			expected: "1 tenth",
		},
		{
			name:     "0.05 seconds is 5 hundredths",
			delta:    50 * time.Millisecond,
			expected: "5 hundredths",
		},
		{
			name:     "0.01 seconds is 1 hundredth",
			delta:    10 * time.Millisecond,
			expected: "1 hundredth",
		},
		{
			name:     "boundary case: 0.0095s rounds to 1 hundredth",
			delta:    time.Duration(9.5 * float64(time.Millisecond)),
			expected: "1 hundredth",
		},

		// Thou range (< 0.0095s)
		{
			name:     "0.0094s is 1 hundredth (avoids 10 thou)",
			delta:    time.Duration(9.4 * float64(time.Millisecond)),
			expected: "1 hundredth",
		},
		{
			name:     "0.005 seconds is 5 thou",
			delta:    5 * time.Millisecond,
			expected: "5 thou",
		},
		{
			name:     "0.001 seconds is 1 thou",
			delta:    1 * time.Millisecond,
			expected: "1 thou",
		},
		{
			name:     "0.002 seconds is 2 thou",
			delta:    2 * time.Millisecond,
			expected: "2 thou",
		},

		// Negative deltas (faster laps - use floor instead of ceil)
		{
			name:     "negative 1 second",
			delta:    -1 * time.Second,
			expected: "1 second",
		},
		{
			name:     "negative 0.5 seconds is 5 tenths",
			delta:    -500 * time.Millisecond,
			expected: "5 tenths",
		},
		{
			name:     "negative 0.05 seconds is 5 hundredths",
			delta:    -50 * time.Millisecond,
			expected: "5 hundredths",
		},
		{
			name:     "negative 0.005 seconds is 5 thou",
			delta:    -5 * time.Millisecond,
			expected: "5 thou",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := app.FormatDeltaTime(testCase.delta)
			if result != testCase.expected {
				t.Errorf("formatDeltaTime(%v) = %q, want %q", testCase.delta, result, testCase.expected)
			}
		})
	}
}

func TestRoundDeltaRoundsValuesBasedOnSign(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		value    float64
		expected float64
	}{
		{
			name:     "positive value rounds up",
			value:    5.1,
			expected: 6.0,
		},
		{
			name:     "positive exact value",
			value:    5.0,
			expected: 5.0,
		},
		{
			name:     "negative value rounds down (floor)",
			value:    -5.1,
			expected: 6.0,
		},
		{
			name:     "negative exact value",
			value:    -5.0,
			expected: 5.0,
		},
		{
			name:     "positive small fraction rounds up",
			value:    0.1,
			expected: 1.0,
		},
		{
			name:     "negative small fraction rounds down",
			value:    -0.1,
			expected: 1.0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := app.RoundDelta(testCase.value)
			if result != testCase.expected {
				t.Errorf("roundDelta(%v) = %v, want %v", testCase.value, result, testCase.expected)
			}
		})
	}
}

func TestPluraliseDeltaHandlesSingularAndPluralForms(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		value    float64
		scale    string
		expected string
	}{
		{
			name:     "1 tenth singular",
			value:    1.0,
			scale:    "tenth",
			expected: "1 tenth",
		},
		{
			name:     "2 tenths plural",
			value:    2.0,
			scale:    "tenth",
			expected: "2 tenths",
		},
		{
			name:     "1 hundredth singular",
			value:    1.0,
			scale:    "hundredth",
			expected: "1 hundredth",
		},
		{
			name:     "5 hundredths plural",
			value:    5.0,
			scale:    "hundredth",
			expected: "5 hundredths",
		},
		{
			name:     "1 thou no pluralization",
			value:    1.0,
			scale:    "thou",
			expected: "1 thou",
		},
		{
			name:     "5 thou no pluralization",
			value:    5.0,
			scale:    "thou",
			expected: "5 thou",
		},
		{
			name:     "0 tenths plural",
			value:    0.0,
			scale:    "tenth",
			expected: "0 tenths",
		},
		{
			name:     "9 tenths plural",
			value:    9.0,
			scale:    "tenth",
			expected: "9 tenths",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := app.PluraliseDelta(testCase.value, testCase.scale)
			if result != testCase.expected {
				t.Errorf("pluraliseDelta(%v, %q) = %q, want %q", testCase.value, testCase.scale, result, testCase.expected)
			}
		})
	}
}

func TestFormatDurationConvertsLapTimesToSpeechFormat(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "1 minute 30.123 seconds",
			duration: 1*time.Minute + 30*time.Second + 123*time.Millisecond,
			expected: "1 30 1 2 3",
		},
		{
			name:     "30.123 seconds (no minutes)",
			duration: 30*time.Second + 123*time.Millisecond,
			expected: "30 1 2 3",
		},
		{
			name:     "1 minute 0.500 seconds",
			duration: 1*time.Minute + 500*time.Millisecond,
			expected: "1 0 5 oh oh",
		},
		{
			name:     "0.001 seconds",
			duration: 1 * time.Millisecond,
			expected: "0 oh oh 1",
		},
		{
			name:     "2 minutes 5.050 seconds",
			duration: 2*time.Minute + 5*time.Second + 50*time.Millisecond,
			expected: "2 05 oh 5 oh",
		},
		{
			name:     "exactly 1 minute",
			duration: 1 * time.Minute,
			expected: "1 0 oh oh oh",
		},
		{
			name:     "59.999 seconds",
			duration: 59*time.Second + 999*time.Millisecond,
			expected: "59 9 9 9",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := app.FormatDuration(testCase.duration)
			if result != testCase.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", testCase.duration, result, testCase.expected)
			}
		})
	}
}

func TestPronounceTimeFormatsTimeComponentsForSpeech(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		minutes      string
		seconds      string
		milliseconds string
		includeUnits bool
		expected     string
	}{
		{
			name:         "with minutes, without units",
			minutes:      "1",
			seconds:      "30",
			milliseconds: "123",
			includeUnits: false,
			expected:     "1 30 1 2 3",
		},
		{
			name:         "with minutes, with units",
			minutes:      "1",
			seconds:      "30",
			milliseconds: "123",
			includeUnits: true,
			expected:     "1 minutes 30 point 1 2 3",
		},
		{
			name:         "no minutes, without units",
			minutes:      "0",
			seconds:      "30",
			milliseconds: "123",
			includeUnits: false,
			expected:     "30 1 2 3",
		},
		{
			name:         "no minutes, with units",
			minutes:      "0",
			seconds:      "30",
			milliseconds: "123",
			includeUnits: true,
			expected:     "30 point 1 2 3",
		},
		{
			name:         "zeros become 'oh'",
			minutes:      "0",
			seconds:      "5",
			milliseconds: "050",
			includeUnits: false,
			expected:     "5 oh 5 oh",
		},
		{
			name:         "all zeros",
			minutes:      "0",
			seconds:      "0",
			milliseconds: "000",
			includeUnits: false,
			expected:     "0 oh oh oh",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := app.PronounceTime(testCase.minutes, testCase.seconds, testCase.milliseconds, testCase.includeUnits)
			if result != testCase.expected {
				t.Errorf("pronounceTime(%q, %q, %q, %v) = %q, want %q",
					testCase.minutes, testCase.seconds, testCase.milliseconds, testCase.includeUnits, result, testCase.expected)
			}
		})
	}
}
