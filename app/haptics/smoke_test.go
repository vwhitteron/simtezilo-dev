package haptics

import (
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

	cap, err := CaptureChassis(CaptureOptions{Source: src})
	if err != nil {
		t.Fatalf("CaptureChassis: %v", err)
	}

	if len(cap.Samples) == 0 {
		t.Fatal("no samples produced")
	}

	if len(cap.Frames) == 0 {
		t.Fatal("no frames produced")
	}

	last := cap.Frames[len(cap.Frames)-1]
	if last.OutCursor >= len(cap.Samples) {
		t.Fatalf("last frame cursor %d beyond samples %d", last.OutCursor, len(cap.Samples))
	}

	t.Logf("rate=%d samples=%d frames=%d firstLap=%d lastLap=%d",
		cap.InternalRate, len(cap.Samples), len(cap.Frames), cap.Frames[0].Lap, last.Lap)
}
