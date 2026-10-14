package webcommon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// etagKey identifies an embedded file. The size is included so that two embedded
// files sharing a name but differing in content never share an ETag.
type etagKey struct {
	name string
	size int
}

// etags caches the computed ETag per embedded file. Callers that serve different
// content under the same path must prefix the name. Embedded content is immutable
// for the life of the process, so each file only needs hashing once.
var etags sync.Map //nolint:gochecknoglobals // process-wide cache of immutable embedded content

// etagFor returns the quoted strong ETag for the named embedded content.
func etagFor(name string, content []byte) string {
	key := etagKey{name: name, size: len(content)}

	cached, ok := etags.Load(key)
	if ok {
		etag, _ := cached.(string)

		return etag
	}

	sum := sha256.Sum256(content)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	etags.Store(key, etag)

	return etag
}

// ServeEmbedded writes embedded content so browsers revalidate on every load
// (Cache-Control: no-cache) and receive 304 Not Modified when nothing changed.
// It replaces manual cache-busting query strings on asset URLs. The name only
// identifies the content for ETag caching and range handling.
func ServeEmbedded(response http.ResponseWriter, request *http.Request, name, contentType string, content []byte) {
	header := response.Header()
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", "no-cache")
	header.Set("ETag", etagFor(name, content))

	http.ServeContent(response, request, name, time.Time{}, bytes.NewReader(content))
}
