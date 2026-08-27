package tuneassist //nolint:testpackage // reaches captureLayers and the unexported render path

import (
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

// TestAudioEndpointLayers exercises the HTTP surface the page actually calls: one request per layer, plus an
// unknown layer, through HandleAudio rather than the render function underneath it.
func TestAudioEndpointLayers(t *testing.T) {
	t.Parallel()

	root, _ := filepath.Abs(filepath.Join("..", ".."))
	replayDir := filepath.Join(root, "data", "replays")

	_, err := os.Stat(replayDir)
	if err != nil {
		t.Skipf("replays not present: %v", err)
	}

	svc := New(Options{
		Log:       zerolog.New(io.Discard),
		ReplayDir: func() string { return replayDir },
	})

	replay := "20260801.111955-circuit-de-spa-francorchamps-toyota-supra-rz-97.gtz"

	for _, layer := range []string{"", "chassis", "texture", "transmission", "engine"} {
		url := "/api/tuneassist/audio?replay=" + replay + "&lap=2&from=0&to=1800&layer=" + layer

		rec := httptest.NewRecorder()
		svc.HandleAudio(rec, httptest.NewRequest(http.MethodGet, url, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("layer %q: status %d body %s", layer, rec.Code, rec.Body.String())
		}

		t.Logf("layer %-13q status=%d type=%s bytes=%d",
			layer, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())

		body := rec.Body.Bytes()
		if len(body) < wavHeaderLen {
			t.Fatalf("layer %q: response too short to contain a WAV header: %d bytes", layer, len(body))
		}

		pcm := body[wavHeaderLen:]

		var peak int16

		for i := 0; i+1 < len(pcm); i += 2 {
			sample := int16(binary.LittleEndian.Uint16(pcm[i : i+2])) //nolint:gosec // reinterpreting PCM bit pattern, not a numeric conversion
			if sample < 0 {
				sample = -sample
			}

			if sample > peak {
				peak = sample
			}
		}

		if peak == 0 {
			t.Fatalf("layer %q: rendered audio is silent (peak sample 0)", layer)
		}
	}

	// An unknown layer must be rejected rather than silently rendering the chassis.
	rec := httptest.NewRecorder()
	svc.HandleAudio(rec, httptest.NewRequest(http.MethodGet,
		"/api/tuneassist/audio?replay="+replay+"&lap=2&layer=nonsense", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown layer: expected 400, got %d", rec.Code)
	}

	t.Logf("layer %-13q status=%d (correctly rejected)", "nonsense", rec.Code)
}
