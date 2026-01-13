package synthesizer_test

import (
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
)

func TestCalibrationMode_SweepStopsOnDisable(t *testing.T) {
	t.Parallel()

	// Create calibrator
	cal := calibrator.New()

	// Start sweep (which also enables calibration)
	cal.StartSweep()

	if !cal.IsSweeping() {
		t.Fatal("Expected sweep to be active")
	}

	if !cal.IsEnabled() {
		t.Fatal("Expected calibration to be enabled")
	}

	// Disable calibration mode
	cal.SetEnabled(false)

	// Give goroutine time to stop
	time.Sleep(10 * time.Millisecond)

	// Verify sweep stopped
	if cal.IsSweeping() {
		t.Error("Expected sweep to stop when calibration is disabled")
	}

	// Verify frequency reset to minimum
	freq := cal.GetSweepFrequency()

	minFreq := cal.GetSweepMin()
	if freq != minFreq {
		t.Errorf("Expected frequency to reset to minimum (%f), got %f", minFreq, freq)
	}
}
