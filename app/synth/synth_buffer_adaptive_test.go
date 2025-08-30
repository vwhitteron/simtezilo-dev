package synth

import (
	"testing"
	"time"
)

func TestAdaptiveBufferBasicOperations(t *testing.T) {
	// Create a buffer that holds approximately 96 samples (2ms at 48000 Hz)
	buffer := NewAdaptiveBuffer(2*time.Millisecond, 48000)

	// Test basic properties
	if buffer.Length() != 96 {
		t.Errorf("Expected buffer length 96, got %d", buffer.Length())
	}

	if buffer.Used() != 0 {
		t.Errorf("Expected empty buffer, got %d used", buffer.Used())
	}

	if buffer.Available() != 96 {
		t.Errorf("Expected 96 available, got %d", buffer.Available())
	}
}

func TestAdaptiveBufferWriteRead(t *testing.T) {
	// Create a buffer that holds 10 samples (1ms at 10000 Hz)
	buffer := NewAdaptiveBuffer(time.Millisecond, 10000)

	// Write some samples
	samples := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	buffer.Write(samples, true)

	if buffer.Used() != 5 {
		t.Errorf("Expected 5 samples used, got %d", buffer.Used())
	}

	// Read some samples
	read := buffer.Read(3)
	if len(read) != 3 {
		t.Errorf("Expected 3 samples read, got %d", len(read))
	}

	if read[0] != 1.0 || read[1] != 2.0 || read[2] != 3.0 {
		t.Errorf("Unexpected sample values: %v", read)
	}

	if buffer.Used() != 2 {
		t.Errorf("Expected 2 samples remaining, got %d", buffer.Used())
	}
}

func TestAdaptiveBufferOverflow(t *testing.T) {
	// Create a buffer that holds 5 samples (1ms at 5000 Hz)
	buffer := NewAdaptiveBuffer(time.Millisecond, 5000)

	// Fill buffer completely
	samples1 := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	buffer.Write(samples1, true)

	// Try to write more (should cause overflow)
	samples2 := []float64{6.0, 7.0, 8.0}
	buffer.Write(samples2, true)

	overflows, _, _ := buffer.Health()
	if overflows == 0 {
		t.Error("Expected overflow to be detected")
	}

	// Buffer should still work and contain the most recent samples
	if buffer.Used() != 5 {
		t.Errorf("Expected buffer to remain full, got %d used", buffer.Used())
	}
}

func TestAdaptiveBufferMixing(t *testing.T) {
	// Create a buffer that holds 10 samples (1ms at 10000 Hz)
	buffer := NewAdaptiveBuffer(time.Millisecond, 10000)

	// Write initial samples
	samples1 := []float64{0.5, 0.5, 0.5}
	buffer.Write(samples1, true)

	// Mix with existing samples
	samples2 := []float64{0.5, 0.5, 0.5}
	buffer.Write(samples2, false) // false = mix mode

	// Read back and check mixing occurred
	result := buffer.Read(3)
	for i, sample := range result {
		if sample < 0.9 || sample > 1.1 { // Should be approximately 1.0 after mixing
			t.Errorf("Sample %d mixing failed: expected ~1.0, got %f", i, sample)
		}
	}
}

func TestAdaptiveBufferUnderrun(t *testing.T) {
	// Create a buffer that holds 10 samples (1ms at 10000 Hz)
	buffer := NewAdaptiveBuffer(time.Millisecond, 10000)

	// Write fewer samples than we'll try to read
	samples := []float64{1.0, 2.0}
	buffer.Write(samples, true)

	// Try to read more than available
	result := buffer.Read(5)

	// Should only get what's available
	if len(result) != 2 {
		t.Errorf("Expected 2 samples (underrun), got %d", len(result))
	}

	_, underruns, _ := buffer.Health()
	if underruns == 0 {
		t.Error("Expected underrun to be detected")
	}
}

func TestAdaptiveBufferHealthMonitoring(t *testing.T) {
	// Create a buffer that holds 10 samples (1ms at 10000 Hz)
	buffer := NewAdaptiveBuffer(time.Millisecond, 10000)

	// Test fill ratio
	samples := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	buffer.Write(samples, true)

	_, _, fillRatio := buffer.Health()
	expectedRatio := 5.0 / 10.0
	if fillRatio != expectedRatio {
		t.Errorf("Expected fill ratio %f, got %f", expectedRatio, fillRatio)
	}

	// Test starvation detection
	buffer.Clear()
	buffer.Write([]float64{1.0}, true) // Very low fill
	if !buffer.IsStarved() {
		t.Error("Expected buffer to be detected as starved")
	}

	// Test overfull detection
	buffer.Clear()
	largeSamples := make([]float64, 8) // 80% full
	for i := range largeSamples {
		largeSamples[i] = float64(i)
	}
	buffer.Write(largeSamples, true)
	if !buffer.IsOverfull() {
		t.Error("Expected buffer to be detected as overfull")
	}
}

func TestAdaptiveBufferConcurrency(t *testing.T) {
	// Create a buffer that holds 96 samples (2ms at 48000 Hz)
	buffer := NewAdaptiveBuffer(2*time.Millisecond, 48000)

	// Test concurrent writes and reads
	done := make(chan bool, 2)

	// Writer goroutine
	go func() {
		for i := 0; i < 10; i++ {
			samples := []float64{float64(i), float64(i + 1)}
			buffer.Write(samples, true)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 10; i++ {
			buffer.Read(2)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Wait for both to complete
	<-done
	<-done

	// Should not panic or deadlock
	t.Log("Concurrent test completed successfully")
}
