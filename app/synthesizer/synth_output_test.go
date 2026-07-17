package synthesizer_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

func TestCalibrationModeOutputsSignal(t *testing.T) {
	t.Parallel()

	// Arrange
	mockCalibrator := calibrator.NewMockCalibrator(
		true,
		50, // 50 Hz test tone
		0,  // 0 dB (max)
		calibrator.OutputChannelBoth,
	)

	mockMixer := synthesizer.NewMockMixer(8000, mockCalibrator)

	synth, _ := synthesizer.New(&synthesizer.SynthOpts{
		Calibrator: mockCalibrator,
		Mixer:      mockMixer,
		Config:     &config.Synthesizer{InternalSampleRateHz: 8000},
	})

	streamer := synthesizer.NewStreamer(synth)

	const (
		frames   = 100
		channels = 2
	)

	buf := make([]float32, frames*channels)

	// Act
	n, ok := streamer.ReadInterleaved(buf, channels)

	// Assert
	assert.True(t, ok, "ReadInterleaved() should succeed")
	assert.Equal(t, frames, n, "ReadInterleaved() should return expected number of frames")

	anyNonZero := false

	for _, s := range buf {
		if math.Abs(float64(s)) > 1e-6 {
			anyNonZero = true

			break
		}
	}

	assert.True(t, anyNonZero, "Calibration mode should output non-zero signal")
}
