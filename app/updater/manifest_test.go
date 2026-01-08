package updater //nolint:testpackage // testing internal functions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestParseManifest(t *testing.T) {
	t.Parallel()

	manifestJSON := `{
		"version": "1.2.3",
		"releaseDate": "2026-01-07T10:00:00Z",
		"channel": "stable",
		"minUpgradeVersion": "1.0.0",
		"changelog": "- Bug fixes\n- New features",
		"platforms": {
			"linux-arm64": {
				"url": "https://example.com/releases/v1.2.3/simtezilo-linux-arm64",
				"sha256": "abc123",
				"size": 15728640
			},
			"linux-amd64": {
				"url": "https://example.com/releases/v1.2.3/simtezilo-linux-amd64",
				"sha256": "def456",
				"size": 16252928
			}
		}
	}`

	manifest, err := ParseManifest([]byte(manifestJSON))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	if manifest.Version != "1.2.3" {
		t.Errorf("Version = %v, want 1.2.3", manifest.Version)
	}

	if manifest.Channel != "stable" {
		t.Errorf("Channel = %v, want stable", manifest.Channel)
	}

	if manifest.MinUpgradeVersion != "1.0.0" {
		t.Errorf("MinUpgradeVersion = %v, want 1.0.0", manifest.MinUpgradeVersion)
	}

	if len(manifest.Platforms) != 2 {
		t.Errorf("len(Platforms) = %v, want 2", len(manifest.Platforms))
	}

	linuxArm64 := manifest.Platforms["linux-arm64"]
	if linuxArm64.SHA256 != "abc123" {
		t.Errorf("linux-arm64 SHA256 = %v, want abc123", linuxArm64.SHA256)
	}

	if linuxArm64.Size != 15728640 {
		t.Errorf("linux-arm64 Size = %v, want 15728640", linuxArm64.Size)
	}
}

func TestFetchManifest(t *testing.T) {
	t.Parallel()

	manifest := Manifest{
		Version:     "2.0.0",
		ReleaseDate: time.Now().UTC(),
		Channel:     "stable",
		Platforms: map[string]Platform{
			"linux-arm64": {
				URL:    "https://example.com/binary",
				SHA256: "abc123",
				Size:   1024,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(respWriter http.ResponseWriter, _ *http.Request) {
		respWriter.Header().Set("Content-Type", "application/json")

		err := json.NewEncoder(respWriter).Encode(manifest)
		if err != nil {
			t.Errorf("Failed to encode manifest: %v", err)
		}
	}))
	defer server.Close()

	log := zerolog.Nop()

	ctx := context.Background()

	fetched, err := FetchManifest(ctx, server.URL, 10*time.Second, log)
	if err != nil {
		t.Fatalf("FetchManifest() error = %v", err)
	}

	if fetched.Version != manifest.Version {
		t.Errorf("Version = %v, want %v", fetched.Version, manifest.Version)
	}

	if fetched.Channel != manifest.Channel {
		t.Errorf("Channel = %v, want %v", fetched.Channel, manifest.Channel)
	}
}

func TestFetchManifest_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(respWriter http.ResponseWriter, _ *http.Request) {
		respWriter.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	log := zerolog.Nop()

	ctx := context.Background()

	_, err := FetchManifest(ctx, server.URL, 10*time.Second, log)
	if err == nil {
		t.Error("Expected error for server error response")
	}
}

func TestFetchManifest_InvalidJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(respWriter http.ResponseWriter, _ *http.Request) {
		_, err := respWriter.Write([]byte("not json"))
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	log := zerolog.Nop()

	ctx := context.Background()

	_, err := FetchManifest(ctx, server.URL, 10*time.Second, log)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestGetPlatformKey(t *testing.T) {
	t.Parallel()

	key := GetPlatformKey()
	if key == "" {
		t.Error("GetPlatformKey() returned empty string")
	}
	// Should contain a hyphen (os-arch format)
	if len(key) < 3 {
		t.Errorf("GetPlatformKey() = %v, expected longer format", key)
	}
}
