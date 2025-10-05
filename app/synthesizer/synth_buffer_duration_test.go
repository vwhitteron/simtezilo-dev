package synthesizer

import (
	"testing"
	"time"
)

func TestDurationCalculationExamples(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		duration   time.Duration
		sampleRate int
		expected   int
	}{
		{"Synthesizer default: 2 seconds at 8kHz", 2 * time.Second, 8000, 16000},
		{"High quality: 100ms at 48kHz", 100 * time.Millisecond, 48000, 4800},
		{"Low latency: 10ms at 44.1kHz", 10 * time.Millisecond, 44100, 441},
		{"Microsecond precision: 1ms at 1kHz", time.Millisecond, 1000, 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Test all buffer types to ensure consistency
			adaptiveBuffer := NewAdaptiveBuffer(testCase.duration, testCase.sampleRate)
			ringBuffer := NewRingBuffer(testCase.duration, testCase.sampleRate)
			linearBuffer := NewLinearBuffer(testCase.duration, testCase.sampleRate)

			// All buffer types should produce the same size
			if adaptiveBuffer.Length() != testCase.expected {
				t.Errorf("AdaptiveBuffer: expected %d samples, got %d", testCase.expected, adaptiveBuffer.Length())
			}

			if ringBuffer.Length() != testCase.expected {
				t.Errorf("RingBuffer: expected %d samples, got %d", testCase.expected, ringBuffer.Length())
			}

			if linearBuffer.Length() != testCase.expected {
				t.Errorf("LinearBuffer: expected %d samples, got %d", testCase.expected, linearBuffer.Length())
			}

			// Verify the duration calculation is correct
			expectedFromDuration := int(testCase.duration.Seconds() * float64(testCase.sampleRate))
			if adaptiveBuffer.Length() != expectedFromDuration {
				t.Errorf("Duration calculation mismatch: expected %d, got %d", expectedFromDuration, adaptiveBuffer.Length())
			}
		})
	}
}
