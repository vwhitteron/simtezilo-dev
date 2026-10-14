package tuneassist //nolint:testpackage // reaches the unexported upload helpers

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUploadService returns a Service over fresh replay and cache directories.
func newUploadService(t *testing.T) (svc *Service, replayDir, cacheDir string) {
	t.Helper()

	replayDir, cacheDir = t.TempDir(), t.TempDir()

	svc = New(Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return replayDir },
		CacheDir:  func() string { return cacheDir },
	})

	return svc, replayDir, cacheDir
}

// gzipBytes returns payload gzipped.
func gzipBytes(t *testing.T, payload []byte) []byte {
	t.Helper()

	var compressed bytes.Buffer

	writer := gzip.NewWriter(&compressed)

	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return compressed.Bytes()
}

// postUpload sends body to HandleUpload and returns the recorded response.
func postUpload(svc *Service, method string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/api/tuneassist/upload", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	svc.HandleUpload(recorder, request)

	return recorder
}

// decodeUploadID decodes the ID from a successful upload response.
func decodeUploadID(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var decoded struct {
		ID string `json:"id"`
	}

	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&decoded))

	return decoded.ID
}

// getData requests /data with the given query and returns the recorded response.
func getData(svc *Service, query url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/tuneassist/data?"+query.Encode(), nil)
	recorder := httptest.NewRecorder()

	svc.HandleData(recorder, request)

	return recorder
}

// testUploadPayload is a stand-in packet stream. It need not decode: /data answers
// the same for an upload and for a replay holding equal bytes.
func testUploadPayload() []byte {
	return bytes.Repeat([]byte("0S7G packet payload "), 1000)
}

func TestHandleUploadStoresInflatedBody(t *testing.T) {
	t.Parallel()

	// Arrange
	svc, _, cacheDir := newUploadService(t)
	payload := testUploadPayload()
	sum := sha256.Sum256(payload)

	// Act
	recorder := postUpload(svc, http.MethodPost, gzipBytes(t, payload))
	upload := decodeUploadID(t, recorder)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, hex.EncodeToString(sum[:]), upload)

	stored, err := os.ReadFile(filepath.Join(cacheDir, "upload-"+upload+".gtr"))
	require.NoError(t, err)
	assert.Equal(t, payload, stored)
}

func TestHandleUploadDeduplicatesSameBody(t *testing.T) {
	t.Parallel()

	// Arrange
	svc, _, cacheDir := newUploadService(t)
	body := gzipBytes(t, testUploadPayload())

	// Act
	first := postUpload(svc, http.MethodPost, body)
	second := postUpload(svc, http.MethodPost, body)

	// Assert
	assert.Equal(t, decodeUploadID(t, first), decodeUploadID(t, second))

	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "a repeated upload must not leave a second file or a temp file")
}

func TestHandleUploadRejectsBadBody(t *testing.T) {
	t.Parallel()

	tests := map[string][]byte{
		"not gzip":        []byte("this is not gzip"),
		"empty body":      {},
		"empty gzip":      nil,
		"truncated gzip":  nil,
		"corrupt payload": nil,
	}

	whole := gzipBytes(t, testUploadPayload())
	tests["empty gzip"] = gzipBytes(t, nil)
	tests["truncated gzip"] = whole[:len(whole)/2]

	corrupt := bytes.Clone(whole)
	corrupt[len(corrupt)/2] ^= 0xff
	tests["corrupt payload"] = corrupt

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			svc, _, cacheDir := newUploadService(t)

			// Act
			recorder := postUpload(svc, http.MethodPost, body)

			// Assert
			assert.Equal(t, http.StatusBadRequest, recorder.Code)

			entries, err := os.ReadDir(cacheDir)
			require.NoError(t, err)
			assert.Empty(t, entries, "a rejected upload must leave nothing in the cache")
		})
	}
}

func TestHandleUploadWithoutCacheDir(t *testing.T) {
	t.Parallel()

	// Arrange
	svc := New(Options{
		Log:       zerolog.Nop(),
		ReplayDir: func() string { return t.TempDir() },
	})

	// Act
	recorder := postUpload(svc, http.MethodPost, gzipBytes(t, testUploadPayload()))

	// Assert
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestHandleUploadRejectsOtherMethods(t *testing.T) {
	t.Parallel()

	// Arrange
	svc, _, cacheDir := newUploadService(t)

	// Act
	recorder := postUpload(svc, http.MethodGet, gzipBytes(t, testUploadPayload()))

	// Assert
	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)

	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestHandleUploadHoldsHeavySlot(t *testing.T) {
	t.Parallel()

	// Arrange
	svc, _, cacheDir := newUploadService(t)

	body := gzipBytes(t, testUploadPayload())

	release, err := svc.acquire(t.Context())
	require.NoError(t, err)

	done := make(chan *httptest.ResponseRecorder, 1)

	// Act
	go func() { done <- postUpload(svc, http.MethodPost, body) }()

	// Assert
	select {
	case <-done:
		t.Fatal("upload finished while the heavy-job slot was held")
	case <-time.After(50 * time.Millisecond):
	}

	release()

	recorder := <-done
	assert.Equal(t, http.StatusOK, recorder.Code)

	entries, readErr := os.ReadDir(cacheDir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1)
}

func TestIsCachedCopyMatchesUploadTempFile(t *testing.T) {
	t.Parallel()

	// Arrange
	dir := t.TempDir()

	temp, err := os.CreateTemp(dir, uploadPrefix+"*"+uploadSuffix+".part")
	require.NoError(t, err)
	require.NoError(t, temp.Close())

	// Act
	matches := isCachedCopy(filepath.Base(temp.Name()))

	// Assert
	assert.True(t, matches, "the sweep must remove a temp file left by an interrupted upload")
	assert.True(t, strings.HasPrefix(filepath.Base(temp.Name()), uploadPrefix))
}

func TestResolveRequestSourceServesUpload(t *testing.T) {
	t.Parallel()

	// Arrange
	svc, replayDir, _ := newUploadService(t)
	payload := testUploadPayload()
	require.NoError(t, os.WriteFile(filepath.Join(replayDir, "same.gtr"), payload, 0o600))

	upload := decodeUploadID(t, postUpload(svc, http.MethodPost, gzipBytes(t, payload)))

	// Act
	fromReplay := getData(svc, url.Values{"replay": {"same.gtr"}})
	fromUpload := getData(svc, url.Values{"upload": {upload}})

	// Assert
	assert.Equal(t, fromReplay.Code, fromUpload.Code)
	assert.Equal(t, fromReplay.Body.String(), fromUpload.Body.String())
}

func TestResolveRequestSourceRejectsBadSelectors(t *testing.T) {
	t.Parallel()

	valid := strings.Repeat("a", 64)

	tests := map[string]struct {
		query  url.Values
		status int
	}{
		"unknown id":       {url.Values{"upload": {valid}}, http.StatusNotFound},
		"traversal":        {url.Values{"upload": {"../x"}}, http.StatusBadRequest},
		"too short":        {url.Values{"upload": {strings.Repeat("a", 63)}}, http.StatusBadRequest},
		"too long":         {url.Values{"upload": {strings.Repeat("a", 65)}}, http.StatusBadRequest},
		"uppercase":        {url.Values{"upload": {strings.Repeat("A", 64)}}, http.StatusBadRequest},
		"both":             {url.Values{"upload": {valid}, "replay": {"same.gtr"}}, http.StatusBadRequest},
		"neither":          {url.Values{}, http.StatusBadRequest},
		"unknown replay":   {url.Values{"replay": {"missing.gtr"}}, http.StatusNotFound},
		"replay traversal": {url.Values{"replay": {"../x.gtr"}}, http.StatusBadRequest},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			svc, replayDir, _ := newUploadService(t)
			require.NoError(t, os.WriteFile(filepath.Join(replayDir, "same.gtr"), testUploadPayload(), 0o600))

			// Act
			data := getData(svc, testCase.query)

			audioRequest := httptest.NewRequest(http.MethodGet, "/api/tuneassist/audio?"+testCase.query.Encode(), nil)
			audio := httptest.NewRecorder()
			svc.HandleAudio(audio, audioRequest)

			// Assert
			assert.Equal(t, testCase.status, data.Code)
			assert.Equal(t, testCase.status, audio.Code)
		})
	}
}

func TestResolveRequestSourceRefreshesUploadModTime(t *testing.T) {
	t.Parallel()

	// Arrange
	svc, _, cacheDir := newUploadService(t)
	upload := decodeUploadID(t, postUpload(svc, http.MethodPost, gzipBytes(t, testUploadPayload())))

	path := filepath.Join(cacheDir, "upload-"+upload+".gtr")
	old := time.Now().Add(-30 * time.Minute)
	require.NoError(t, os.Chtimes(path, old, old))

	// Act
	source, video, status, err := svc.resolveRequestSource(httptest.NewRequest(http.MethodGet, "/?upload="+upload, nil))

	// Assert
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Nil(t, video)
	assert.Equal(t, "file://"+path, source)

	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.WithinDuration(t, time.Now(), info.ModTime(), time.Minute)
}

func TestHandleReplaysOmitsUploads(t *testing.T) {
	t.Parallel()

	// Arrange
	svc, _, _ := newUploadService(t)
	postUpload(svc, http.MethodPost, gzipBytes(t, testUploadPayload()))

	recorder := httptest.NewRecorder()

	// Act
	svc.HandleReplays(recorder, httptest.NewRequest(http.MethodGet, "/api/tuneassist/replays", nil))

	// Assert
	assert.JSONEq(t, `{"replays":[]}`, recorder.Body.String())
}
