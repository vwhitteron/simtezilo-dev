package haptics

import (
	"context"
	"os"
	"testing"
)

// TestChassisSmoke drives a real replay if one is provided via HAPTICCAPTURE_REPLAY
// (an absolute file:// URL) and asserts the capture is non-empty and finite. It is a
// developer smoke check, skipped by default.
func TestChassisSmoke(t *testing.T) {
	src := os.Getenv("HAPTICCAPTURE_REPLAY")
	if src == "" {
		t.Skip("set HAPTICCAPTURE_REPLAY to a file:// replay URL to run")
	}

	capture, err := CaptureChassis(context.Background(), CaptureOptions{Source: src})
	if err != nil {
		t.Fatalf("CaptureChassis: %v", err)
	}

	if len(capture.Samples) == 0 {
		t.Fatal("no samples produced")
	}

	if len(capture.Frames) == 0 {
		t.Fatal("no frames produced")
	}

	last := capture.Frames[len(capture.Frames)-1]
	if last.OutCursor >= len(capture.Samples) {
		t.Fatalf("last frame cursor %d beyond samples %d", last.OutCursor, len(capture.Samples))
	}

	t.Logf("rate=%d samples=%d frames=%d firstLap=%d lastLap=%d",
		capture.InternalRate, len(capture.Samples), len(capture.Frames), capture.Frames[0].Lap, last.Lap)
}
