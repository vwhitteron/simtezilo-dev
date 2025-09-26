package synthesizer

import (
	"testing"
	"time"
)

func TestDurationCalculationExamples(t *testing.T) {
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test all buffer types to ensure consistency
			adaptiveBuffer := NewAdaptiveBuffer(tc.duration, tc.sampleRate)
			ringBuffer := NewRingBuffer(tc.duration, tc.sampleRate)
			linearBuffer := NewLinearBuffer(tc.duration, tc.sampleRate)

			// All buffer types should produce the same size
			if adaptiveBuffer.Length() != tc.expected {
				t.Errorf("AdaptiveBuffer: expected %d samples, got %d", tc.expected, adaptiveBuffer.Length())
			}
			if ringBuffer.Length() != tc.expected {
				t.Errorf("RingBuffer: expected %d samples, got %d", tc.expected, ringBuffer.Length())
			}
			if linearBuffer.Length() != tc.expected {
				t.Errorf("LinearBuffer: expected %d samples, got %d", tc.expected, linearBuffer.Length())
			}

			// Verify the duration calculation is correct
			expectedFromDuration := int(tc.duration.Seconds() * float64(tc.sampleRate))
			if adaptiveBuffer.Length() != expectedFromDuration {
				t.Errorf("Duration calculation mismatch: expected %d, got %d", expectedFromDuration, adaptiveBuffer.Length())
			}
		})
	}
}
