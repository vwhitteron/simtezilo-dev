package haptics //nolint:testpackage // reaches the unexported window gate and router directly

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCaptureWindowGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		window     *CaptureWindow
		lap        int16
		idx        int
		replayIdx  int
		wantOpens  bool
		wantCloses bool
	}{
		{name: "nil window opens", window: nil, lap: 3, idx: 0, replayIdx: 0, wantOpens: true},
		{name: "other lap stays shut", window: &CaptureWindow{Lap: 2, ToFrame: -1}, lap: 3, idx: 5, replayIdx: 50, wantOpens: false},
		{name: "lap frame in range opens", window: &CaptureWindow{Lap: 2, FromFrame: 4, ToFrame: 10}, lap: 2, idx: 5, replayIdx: 50, wantOpens: true},
		{name: "lap frame past range closes", window: &CaptureWindow{Lap: 2, FromFrame: 4, ToFrame: 10}, lap: 2, idx: 11, replayIdx: 5, wantCloses: true},
		{name: "all laps ignores lap", window: &CaptureWindow{AllLaps: true, FromFrame: 40, ToFrame: 60}, lap: 7, idx: 0, replayIdx: 50, wantOpens: true},
		{name: "all laps before range stays shut", window: &CaptureWindow{AllLaps: true, FromFrame: 40, ToFrame: 60}, lap: 1, idx: 50, replayIdx: 39, wantOpens: false},
		{name: "all laps past range closes", window: &CaptureWindow{AllLaps: true, FromFrame: 40, ToFrame: 60}, lap: 1, idx: 0, replayIdx: 61, wantCloses: true},
		{name: "all laps open end never closes", window: &CaptureWindow{AllLaps: true, ToFrame: -1}, lap: 9, idx: 0, replayIdx: 100000, wantOpens: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Act
			opens, closes := test.window.gate(test.lap, test.idx, test.replayIdx)

			// Assert
			assert.Equal(t, test.wantOpens, opens, "opens")
			assert.Equal(t, test.wantCloses, closes, "closes")
		})
	}
}

func TestCaptureRouterAllLapsSpansLapChange(t *testing.T) {
	t.Parallel()

	// Arrange
	router := &captureRouter{
		out:    &Capture{},
		sink:   func([]float64) {},
		window: &CaptureWindow{AllLaps: true, ToFrame: -1},
	}

	// Act
	stopFirst := router.frame(1, 0, 0, Frame{})
	stopNext := router.frame(2, 0, 1, Frame{})

	// Assert
	assert.False(t, stopFirst)
	assert.False(t, stopNext, "a whole-replay window must not stop at a lap change")
	assert.True(t, router.emitting)
}

func TestCaptureRouterLapWindowStopsAtLapChange(t *testing.T) {
	t.Parallel()

	// Arrange
	router := &captureRouter{
		out:    &Capture{},
		sink:   func([]float64) {},
		window: &CaptureWindow{Lap: 1, ToFrame: -1},
	}

	// Act
	stopFirst := router.frame(1, 0, 0, Frame{})
	stopNext := router.frame(2, 0, 1, Frame{})

	// Assert
	assert.False(t, stopFirst)
	assert.True(t, stopNext)
}
