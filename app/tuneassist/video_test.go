package tuneassist_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/tuneassist"
)

// newVideoTestService builds a Service over a temporary replay directory holding the
// given files.
func newVideoTestService(t *testing.T, files map[string][]byte) *tuneassist.Service {
	t.Helper()

	dir := t.TempDir()

	for name, content := range files {
		err := os.WriteFile(filepath.Join(dir, name), content, 0o600)
		require.NoError(t, err)
	}

	return tuneassist.New(tuneassist.Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return dir },
	})
}

// serveVideo runs HandleVideo for one replay name, optionally with a Range header.
func serveVideo(svc *tuneassist.Service, replay, rangeHeader string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/tuneassist/video?replay="+replay, nil)
	if rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}

	recorder := httptest.NewRecorder()
	svc.HandleVideo(recorder, request)

	return recorder
}

func TestHandleReplaysListsVideosAlongsideRecordings(t *testing.T) {
	t.Parallel()

	// Arrange
	svc := newVideoTestService(t, map[string][]byte{
		"b.gtz":     []byte("x"),
		"a.mp4":     []byte("y"),
		"c.gtr":     []byte("z"),
		"notes.txt": []byte("ignored"),
		"upper.MP4": []byte("y"),
	})

	recorder := httptest.NewRecorder()

	// Act
	svc.HandleReplays(recorder, httptest.NewRequest(http.MethodGet, "/api/tuneassist/replays", nil))

	// Assert
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload map[string][]string

	err := json.NewDecoder(recorder.Body).Decode(&payload)
	require.NoError(t, err)

	assert.Equal(t, []string{"a.mp4", "b.gtz", "c.gtr", "upper.MP4"}, payload["replays"])
}

func TestHandleVideoServesWholeFile(t *testing.T) {
	t.Parallel()

	// Arrange
	body := []byte("0123456789abcdef")
	svc := newVideoTestService(t, map[string][]byte{"demo.mp4": body})

	// Act
	recorder := serveVideo(svc, "demo.mp4", "")

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "bytes", recorder.Header().Get("Accept-Ranges"))
	assert.Equal(t, body, recorder.Body.Bytes())
}

func TestHandleVideoServesRangeRequest(t *testing.T) {
	t.Parallel()

	// Arrange
	svc := newVideoTestService(t, map[string][]byte{"demo.mp4": []byte("0123456789abcdef")})

	// Act
	recorder := serveVideo(svc, "demo.mp4", "bytes=4-9")

	// Assert
	assert.Equal(t, http.StatusPartialContent, recorder.Code)
	assert.Equal(t, "bytes 4-9/16", recorder.Header().Get("Content-Range"))
	assert.Equal(t, []byte("456789"), recorder.Body.Bytes())
}

func TestHandleVideoRejectsNonVideoReplay(t *testing.T) {
	t.Parallel()

	// Arrange
	svc := newVideoTestService(t, map[string][]byte{"demo.gtz": []byte("x")})

	// Act
	recorder := serveVideo(svc, "demo.gtz", "")

	// Assert
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestHandleVideoRejectsUnknownAndTraversalNames(t *testing.T) {
	t.Parallel()

	// Arrange
	cases := map[string]struct {
		replay string
		want   int
	}{
		"unknown file":   {replay: "absent.mp4", want: http.StatusNotFound},
		"parent escape":  {replay: "../demo.mp4", want: http.StatusBadRequest},
		"absolute path":  {replay: "/etc/passwd", want: http.StatusBadRequest},
		"empty name":     {replay: "", want: http.StatusBadRequest},
		"backslash path": {replay: `..\demo.mp4`, want: http.StatusBadRequest},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc := newVideoTestService(t, map[string][]byte{"demo.mp4": []byte("x")})

			// Act
			recorder := serveVideo(svc, testCase.replay, "")

			// Assert
			assert.Equal(t, testCase.want, recorder.Code)
		})
	}
}
