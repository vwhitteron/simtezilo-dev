package webui //nolint:testpackage // white-box testing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveCached issues a GET through handler with an optional If-None-Match header.
func serveCached(handler http.HandlerFunc, path, ifNoneMatch string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}

	recorder := httptest.NewRecorder()
	handler(recorder, request)

	return recorder
}

func testCacheHeaders(t *testing.T, handler http.HandlerFunc, path string) {
	t.Helper()

	// Arrange / Act
	first := serveCached(handler, path, "")
	etag := first.Header().Get("ETag")

	matching := serveCached(handler, path, etag)
	different := serveCached(handler, path, `"does-not-match"`)

	// Assert
	require.Equal(t, http.StatusOK, first.Code)
	assert.Equal(t, "no-cache", first.Header().Get("Cache-Control"))
	assert.Regexp(t, `^"[0-9a-f]{32}"$`, etag)
	assert.NotEmpty(t, first.Body.Bytes())

	assert.Equal(t, http.StatusNotModified, matching.Code)
	assert.Empty(t, matching.Body.Bytes())
	assert.Equal(t, etag, matching.Header().Get("ETag"))

	assert.Equal(t, http.StatusOK, different.Code)
	assert.Equal(t, first.Body.Bytes(), different.Body.Bytes())
}

func TestStaticFileCacheHeaders(t *testing.T) {
	t.Parallel()

	testCacheHeaders(t, createTestWebUI().cssHandlerFunc(), "/css/custom.css")
}

func TestHTMLPageCacheHeaders(t *testing.T) {
	t.Parallel()

	testCacheHeaders(t, createTestWebUI().htmlRouterHandlerFunc(), "/")
}
