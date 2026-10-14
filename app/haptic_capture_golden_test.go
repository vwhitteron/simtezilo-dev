package app //nolint:testpackage // drives the unexported capture harness directly

// Golden capture harness. This is the cross-commit A/B oracle for haptic refactors:
// it renders one layer over a fixed slice of a replay and prints a digest of the
// resulting PCM. A refactor that only moves code must leave the digest unchanged.
//
// The capture is a deterministic discrete-event simulation, so the digest is stable
// across runs on the same commit. Run it before a refactor, record the digests, then
// run it again afterwards and compare.
//
//	HAPTIC_GOLDEN_REPLAY=data/replays/demo.gtz go test ./app/ -run TestHapticGolden -v
//
// Set HAPTIC_GOLDEN_DUMP to a directory to also write each layer's raw float64 PCM
// there, for a listen or a sample-by-sample diff.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// goldenSeekSeconds skips the replay's standing start, which carries no gear shifts
// and little chassis movement. goldenDurSeconds is long enough to contain several
// shifts on a lap of Spa.
const (
	goldenSeekSeconds = 20.0
	goldenDurSeconds  = 30.0
)

func TestHapticGolden(t *testing.T) {
	t.Parallel()

	replay := os.Getenv("HAPTIC_GOLDEN_REPLAY")
	if replay == "" {
		t.Skip("set HAPTIC_GOLDEN_REPLAY to a replay path to run the golden capture")
	}

	abs, err := filepath.Abs(replay)
	if err != nil {
		t.Fatalf("resolving replay path: %v", err)
	}

	source := "file://" + abs

	layers := []struct {
		name string
		opts HapticCaptureOptions
	}{
		{"chassis", HapticCaptureOptions{Chassis: true}},
		{"transmission", HapticCaptureOptions{Transmission: true}},
		{"engine", HapticCaptureOptions{Engine: true}},
	}

	dumpDir := os.Getenv("HAPTIC_GOLDEN_DUMP")

	for _, layer := range layers {
		opts := layer.opts
		opts.Source = source
		opts.SeekSeconds = goldenSeekSeconds
		opts.DurSeconds = goldenDurSeconds

		capture, captureErr := CaptureHaptics(opts)
		if captureErr != nil {
			t.Fatalf("%s: capture failed: %v", layer.name, captureErr)
		}

		digest, peak := digestSamples(capture.Samples)

		t.Logf("%-13s samples=%d rate=%d peak=%.6f sha256=%s",
			layer.name, len(capture.Samples), capture.InternalRate, peak, digest)

		if dumpDir != "" {
			writeSampleDump(t, dumpDir, layer.name, capture.Samples)
		}
	}
}

// digestSamples returns a hex SHA-256 over the exact bit patterns of the samples,
// plus the peak magnitude. The digest uses raw bits rather than a rounded encoding,
// so it catches a change too small to hear.
func digestSamples(samples []float64) (digest string, peak float64) {
	hash := sha256.New()

	var word [8]byte

	for _, sample := range samples {
		binary.LittleEndian.PutUint64(word[:], math.Float64bits(sample))
		hash.Write(word[:])

		if magnitude := math.Abs(sample); magnitude > peak {
			peak = magnitude
		}
	}

	return hex.EncodeToString(hash.Sum(nil)), peak
}

// writeSampleDump writes the raw little-endian float64 samples for one layer, so a
// mismatched digest can be diffed sample by sample.
func writeSampleDump(t *testing.T, dir, layer string, samples []float64) {
	t.Helper()

	buf := make([]byte, 8*len(samples))
	for i, sample := range samples {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(sample))
	}

	path := filepath.Join(dir, "haptic-golden-"+layer+".f64")

	err := os.WriteFile(path, buf, 0o600)
	if err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	t.Logf("%-13s dumped to %s", layer, path)
}
