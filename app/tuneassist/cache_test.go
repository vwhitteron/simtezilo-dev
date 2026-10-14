package tuneassist //nolint:testpackage // reaches the unexported cache sweep

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAged creates dir/name with a modification time age in the past.
func writeAged(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("packets"), 0o600))

	stamp := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, stamp, stamp))

	return path
}

// TestSweepCacheExpiresIdleCopies checks that the sweep deletes idle copies and
// leftover temporary files, keeps recent copies and unrelated files, and reports
// when the next copy expires.
func TestSweepCacheExpiresIdleCopies(t *testing.T) {
	t.Parallel()

	// Arrange
	cacheDir := t.TempDir()
	svc := New(Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return t.TempDir() },
		CacheDir:  func() string { return cacheDir },
	})

	idle := writeAged(t, cacheDir, "idle.gtz.1-2.gtr", 2*time.Hour)
	temp := writeAged(t, cacheDir, "crashed.gtz.1-2.gtr.12345", 2*time.Hour)
	recent := writeAged(t, cacheDir, "recent.gtz.1-2.gtr", 10*time.Minute)
	other := writeAged(t, cacheDir, "notes.txt", 2*time.Hour)

	// Act
	next, pending := svc.sweepCache()

	// Assert
	assert.NoFileExists(t, idle)
	assert.NoFileExists(t, temp)
	assert.FileExists(t, recent)
	assert.FileExists(t, other)
	assert.True(t, pending)
	assert.InDelta(t, 50*time.Minute, next, float64(time.Minute))
}

// TestEnsureCachedRefreshesModTime checks that a cache hit marks the copy as used,
// so the sweep measures idle time rather than age.
func TestEnsureCachedRefreshesModTime(t *testing.T) {
	t.Parallel()

	// Arrange
	path := writeAged(t, t.TempDir(), "replay.gtz.1-2.gtr", 2*time.Hour)
	write := func(io.Writer) error {
		t.Fatal("a cached copy must not be written again")

		return nil
	}

	// Act
	err := ensureCached(path, write)

	// Assert
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), info.ModTime(), time.Minute)
}

// TestCacheSweepTimerExpiresUnusedCopy checks that a copy is deleted once it goes
// unused for the TTL, with no further request to trigger the sweep.
func TestCacheSweepTimerExpiresUnusedCopy(t *testing.T) {
	t.Parallel()

	// Arrange
	replayDir, cacheDir := t.TempDir(), t.TempDir()
	writeGTZ(t, replayDir, "replay.gtz", bytes.Repeat([]byte("payload "), 100))

	svc := newService(Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return replayDir },
		CacheDir:  func() string { return cacheDir },
	}, 100*time.Millisecond)

	// Act
	source, _, err := svc.resolveSource(replayDir, "replay.gtz")
	require.NoError(t, err)

	cached := strings.TrimPrefix(source, "file://")

	// Assert
	assert.FileExists(t, cached)
	assert.Eventually(t, func() bool {
		_, statErr := os.Stat(cached)

		return os.IsNotExist(statErr)
	}, 5*time.Second, 20*time.Millisecond)
}
