package tuneassist //nolint:testpackage // reaches the unexported resolveSource and render path

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// writeGTZ gzips payload into dir/name and returns the file's path.
func writeGTZ(t *testing.T, dir, name string, payload []byte) string {
	t.Helper()

	var compressed bytes.Buffer

	writer := gzip.NewWriter(&compressed)

	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, compressed.Bytes(), 0o600))

	return path
}

// TestResolveSourceInflatesGTZ checks that a .gtz resolves to an inflated copy in
// the cache, and that a second request reuses that copy.
func TestResolveSourceInflatesGTZ(t *testing.T) {
	t.Parallel()

	// Arrange
	replayDir, cacheDir := t.TempDir(), t.TempDir()
	payload := bytes.Repeat([]byte("0S7G packet payload "), 1000)
	writeGTZ(t, replayDir, "replay.gtz", payload)

	svc := New(Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return replayDir },
		CacheDir:  func() string { return cacheDir },
	})

	// Act
	first, video, err := svc.resolveSource(replayDir, "replay.gtz")
	require.NoError(t, err)

	cached := strings.TrimPrefix(first, "file://")
	before, statErr := os.Stat(cached)
	require.NoError(t, statErr)

	second, _, err := svc.resolveSource(replayDir, "replay.gtz")
	require.NoError(t, err)

	after, statErr := os.Stat(cached)
	require.NoError(t, statErr)

	// Assert
	assert.Nil(t, video)
	assert.Equal(t, cacheDir, filepath.Dir(cached))
	assert.True(t, strings.HasSuffix(cached, ".gtr"))
	assert.Equal(t, first, second)
	assert.True(t, os.SameFile(before, after), "the cached copy must be reused, not rewritten")

	inflated, err := os.ReadFile(cached)
	require.NoError(t, err)
	assert.Equal(t, payload, inflated)
}

// TestResolveSourceGTZFallsBack checks that a .gtz is read directly when there is no
// cache directory, or when it cannot be inflated, and that no partial copy is left.
func TestResolveSourceGTZFallsBack(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		useCache bool
		truncate bool
	}{
		"no cache directory": {useCache: false},
		"truncated gzip":     {useCache: true, truncate: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			replayDir, cacheDir := t.TempDir(), t.TempDir()
			path := writeGTZ(t, replayDir, "replay.gtz", bytes.Repeat([]byte("payload "), 1000))

			if test.truncate {
				compressed, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, compressed[:len(compressed)/2], 0o600))
			}

			opts := Options{Log: zerolog.Nop(), ReplayDir: func() string { return replayDir }}
			if test.useCache {
				opts.CacheDir = func() string { return cacheDir }
			}

			svc := New(opts)

			// Act
			source, _, err := svc.resolveSource(replayDir, "replay.gtz")

			// Assert
			require.NoError(t, err)
			assert.Equal(t, "file://"+filepath.ToSlash(path), source)

			entries, err := os.ReadDir(cacheDir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

// TestRenderFromInflatedCacheMatchesGTZ checks that a render from the cached copy is
// bit-identical to a render straight from the .gtz.
func TestRenderFromInflatedCacheMatchesGTZ(t *testing.T) {
	t.Parallel()

	// Arrange
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	replayDir := filepath.Join(root, "data", "replays")
	replay := "20260801.111955-circuit-de-spa-francorchamps-toyota-supra-rz-97.gtz"

	_, err := os.Stat(filepath.Join(replayDir, replay))
	if err != nil {
		t.Skipf("replay not present: %v", err)
	}

	cacheDir := t.TempDir()
	svc := New(Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return replayDir },
		CacheDir:  func() string { return cacheDir },
	})

	window := haptics.CaptureWindow{Lap: 2, FromFrame: 600, ToFrame: 1800}

	cached, _, err := svc.resolveSource(replayDir, replay)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(cached, "file://"+filepath.ToSlash(cacheDir)))

	// Act
	fromCache, err := renderAllLayersWAV(t.Context(), cached, haptics.Tuning{}, false, window)
	require.NoError(t, err)

	fromGTZ, err := renderAllLayersWAV(t.Context(),
		"file://"+filepath.ToSlash(filepath.Join(replayDir, replay)), haptics.Tuning{}, false, window)
	require.NoError(t, err)

	// Assert
	assert.Equal(t, fromGTZ, fromCache)
}
