package tuneassist_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/tuneassist"
)

// slowReadBody drains a response body, stalling once part way through so the server
// is forced to block on a write for longer than its write timeout.
func slowReadBody(t *testing.T, body io.Reader, stall time.Duration) int {
	t.Helper()

	buf := make([]byte, 32*1024)
	total := 0
	stalled := false

	for {
		read, err := body.Read(buf)
		total += read

		if !stalled && total > 0 {
			stalled = true

			time.Sleep(stall)
		}

		if err != nil {
			return total
		}
	}
}

// TestHandleVideoOutlivesServerWriteTimeout pins the reason HandleVideo clears its
// write deadline. The app's shared server sets a 30 second write timeout, which a
// large video body streamed to a scrubbing browser will exceed. The control case
// below shows the same transfer being truncated when the deadline is left in place,
// so this test fails if the clearing is ever removed.
func TestHandleVideoOutlivesServerWriteTimeout(t *testing.T) {
	t.Parallel()

	// Arrange: a body far larger than the kernel socket buffers, so the server
	// genuinely blocks on write rather than handing everything off at once.
	body := make([]byte, 8<<20)
	for i := range body {
		body[i] = byte(i)
	}

	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "demo.mp4"), body, 0o600)
	require.NoError(t, err)

	svc := tuneassist.New(tuneassist.Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return dir },
	})

	server := httptest.NewUnstartedServer(http.HandlerFunc(svc.HandleVideo))
	server.Config.WriteTimeout = 250 * time.Millisecond

	server.Start()
	defer server.Close()

	// Act
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"?replay=demo.mp4", nil)
	require.NoError(t, err)

	response, err := server.Client().Do(request)
	require.NoError(t, err)

	defer response.Body.Close()

	got := slowReadBody(t, response.Body, 750*time.Millisecond)

	// Assert
	assert.Equal(t, len(body), got, "whole body should arrive despite the write timeout")
}

// TestServerWriteTimeoutTruncatesWithoutDeadlineReset is the control for the test
// above: the identical transfer from a handler that does not clear the deadline.
func TestServerWriteTimeoutTruncatesWithoutDeadlineReset(t *testing.T) {
	t.Parallel()

	// Arrange
	body := make([]byte, 8<<20)

	handler := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "video/mp4")
		_, _ = response.Write(body)
	})

	server := httptest.NewUnstartedServer(handler)
	server.Config.WriteTimeout = 250 * time.Millisecond

	server.Start()
	defer server.Close()

	// Act
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	response, err := server.Client().Do(request)
	require.NoError(t, err)

	defer response.Body.Close()

	got := slowReadBody(t, response.Body, 750*time.Millisecond)

	// Assert
	assert.Less(t, got, len(body), "the write timeout should truncate an unguarded transfer")
}
