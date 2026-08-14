package haptics_test

import (
	"context"
	"os"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// TestWindowedStreamEqualsFullSlice checks the windowed streaming path yields exactly
// the samples the old "render everything, then slice by frame cursor" approach did.
func TestWindowedStreamEqualsFullSlice(t *testing.T) {
	t.Parallel()

	src := os.Getenv("HAPTICCAPTURE_REPLAY")
	if src == "" {
		t.Skip("set HAPTICCAPTURE_REPLAY to a file:// replay URL to run")
	}

	full, err := haptics.CaptureChassis(context.Background(), haptics.CaptureOptions{Source: src})
	if err != nil {
		t.Fatalf("full capture: %v", err)
	}

	if len(full.Frames) == 0 {
		t.Fatal("no frames captured")
	}

	lap := full.Frames[len(full.Frames)/2].Lap
	indices := frameIndicesForLap(full.Frames, lap)
	fromFrame, toFrame := indices[len(indices)/4], indices[len(indices)*3/4]

	want := sliceByFrameRange(full, lap, fromFrame, toFrame)

	var got []float64

	streamed, err := haptics.CaptureChassis(context.Background(), haptics.CaptureOptions{
		Source: src,
		Window: &haptics.CaptureWindow{Lap: lap, FromFrame: fromFrame, ToFrame: toFrame},
		Sink:   func(s []float64) { got = append(got, s...) },
	})
	if err != nil {
		t.Fatalf("streamed capture: %v", err)
	}

	t.Run("StreamingRunRetainsNoBuffers", func(t *testing.T) {
		t.Parallel()
		assertStreamingRunRetainsNoBuffers(t, streamed)
	})

	t.Run("SamplesMatchExactly", func(t *testing.T) {
		t.Parallel()
		assertSamplesEqual(t, got, want)
	})

	t.Logf("lap %d frames %d..%d: %d samples matched", lap, fromFrame, toFrame, len(got))
}

// frameIndicesForLap returns, in capture order, the frame indices belonging to lap.
func frameIndicesForLap(frames []haptics.Frame, lap int16) []int {
	var indices []int

	for _, frame := range frames {
		if frame.Lap == lap {
			indices = append(indices, frame.FrameIndex)
		}
	}

	return indices
}

// assertStreamingRunRetainsNoBuffers checks the streaming path buffers nothing: every
// sample must reach the sink instead of being retained in the result.
func assertStreamingRunRetainsNoBuffers(t *testing.T, streamed *haptics.Capture) {
	t.Helper()

	if len(streamed.Samples) != 0 || len(streamed.Frames) != 0 {
		t.Errorf("streaming run retained %d samples and %d frames, want none",
			len(streamed.Samples), len(streamed.Frames))
	}
}

// assertSamplesEqual checks the streamed samples exactly match the full-slice
// reference, sample for sample.
func assertSamplesEqual(t *testing.T, got, want []float64) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("streamed %d samples, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// sliceByFrameRange reproduces the pre-change approach the streaming window replaces:
// render the whole replay, then slice the sample stream by the frame cursors bounding
// the section.
func sliceByFrameRange(full *haptics.Capture, lap int16, fromFrame, toFrame int) []float64 {
	start, end := -1, len(full.Samples)

	for i := range full.Frames {
		frame := &full.Frames[i]
		if frame.Lap != lap || frame.FrameIndex < fromFrame {
			continue
		}

		if frame.FrameIndex > toFrame {
			end = frame.OutCursor

			break
		}

		if start < 0 {
			start = frame.OutCursor
		}
	}

	return full.Samples[start:end]
}
