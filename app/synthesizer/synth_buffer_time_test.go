package synthesizer

import (
	"testing"
	"time"
)

func TestBufferTimeCalculation(t *testing.T) {
	testCases := []struct {
		name       string
		duration   time.Duration
		sampleRate int
		expected   int
	}{
		{"1 second at 48kHz", time.Second, 48000, 48000},
		{"500ms at 48kHz", 500 * time.Millisecond, 48000, 24000},
		{"2 seconds at 8kHz", 2 * time.Second, 8000, 16000},
		{"100ms at 44.1kHz", 100 * time.Millisecond, 44100, 4410},
		{"1ms at 10kHz", time.Millisecond, 10000, 10},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buffer := NewAdaptiveBuffer(tc.duration, tc.sampleRate)
			actual := buffer.Length()

			if actual != tc.expected {
				t.Errorf("Expected %d samples, got %d samples for %s", tc.expected, actual, tc.name)
			}
		})
	}
}

func TestAllBufferTypesTimeCalculation(t *testing.T) {
	duration := 500 * time.Millisecond
	sampleRate := 8000
	expected := 4000

	adaptiveBuffer := NewAdaptiveBuffer(duration, sampleRate)
	ringBuffer := NewRingBuffer(duration, sampleRate)
	linearBuffer := NewLinearBuffer(duration, sampleRate)

	if adaptiveBuffer.Length() != expected {
		t.Errorf("AdaptiveBuffer: expected %d samples, got %d", expected, adaptiveBuffer.Length())
	}

	if ringBuffer.Length() != expected {
		t.Errorf("RingBuffer: expected %d samples, got %d", expected, ringBuffer.Length())
	}

	if linearBuffer.Length() != expected {
		t.Errorf("LinearBuffer: expected %d samples, got %d", expected, linearBuffer.Length())
	}
}
