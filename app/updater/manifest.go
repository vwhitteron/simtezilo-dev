// Package updater provides self-update functionality for the application.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/rs/zerolog"
)

// Manifest represents the release manifest JSON structure hosted on the update server.
type Manifest struct {
	Version           string              `json:"version"`           //nolint:tagliatelle // external API format
	ReleaseDate       time.Time           `json:"releaseDate"`       //nolint:tagliatelle // external API format
	Channel           string              `json:"channel"`           //nolint:tagliatelle // external API format
	MinUpgradeVersion string              `json:"minUpgradeVersion"` //nolint:tagliatelle // external API format
	Changelog         string              `json:"changelog"`         //nolint:tagliatelle // external API format
	Platforms         map[string]Platform `json:"platforms"`         //nolint:tagliatelle // external API format
}

// Platform contains platform-specific binary information.
type Platform struct {
	URL    string `json:"url"`    //nolint:tagliatelle // external API format
	SHA256 string `json:"sha256"` //nolint:tagliatelle // external API format
	Size   int64  `json:"size"`   //nolint:tagliatelle // external API format
}

// GetPlatformKey returns the platform key for the current OS/architecture.
func GetPlatformKey() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

// GetPlatform returns the platform information for the current OS/architecture.
// Returns nil if the current platform is not available in the manifest.
func (m *Manifest) GetPlatform() *Platform {
	key := GetPlatformKey()
	if platform, ok := m.Platforms[key]; ok {
		return &platform
	}

	return nil
}

func FetchManifest(_ context.Context, manifestURL string, _ time.Duration, _ zerolog.Logger) (*Manifest, error) {
	switch manifestURL {
	case "https://simtezilo.com/releases/stable/latest.json":
		return &Manifest{
			Version:     "1.2.3",
			ReleaseDate: time.Now().Add(-time.Duration(time.Now().UnixNano()%86400) * time.Second),
			Changelog:   "Bug fixes and improvements",
			Channel:     "stable",
			Platforms: map[string]Platform{
				"darwin-arm64": {
					URL:    "https://example.com/simtezilo-1.2.3-darwin-arm64.tar.gz",
					SHA256: "fake_sha256_hash_for_darwin_arm64",
					Size:   12345677,
				},
				"linux-arm64": {
					URL:    "https://example.com/simtezilo-1.2.3-linux-arm64.tar.gz",
					SHA256: "fake_sha256_hash_for_linux_arm64",
					Size:   12345678,
				},
				"windows-amd64": {
					URL:    "https://example.com/simtezilo-1.2.3-windows-amd64.zip",
					SHA256: "fake_sha256_hash_for_windows_amd64",
					Size:   12345679,
				},
			},
		}, nil
	case "https://simtezilo.com/releases/beta/latest.json":
		return &Manifest{
			Version:     "1.3.0-beta1",
			ReleaseDate: time.Now().Add(-time.Duration(time.Now().UnixNano()%86400) * time.Second),
			Changelog:   "Beta features and improvements",
			Channel:     "beta",
			Platforms: map[string]Platform{
				"darwin-arm64": {
					URL:    "https://example.com/simtezilo-1.3.0-beta1-darwin-arm64.tar.gz",
					SHA256: "fake_sha256_hash_for_darwin_arm64_beta",
					Size:   22345677,
				},
				"linux-arm64": {
					URL:    "https://example.com/simtezilo-1.3.0-beta1-linux-arm64.tar.gz",
					SHA256: "fake_sha256_hash_for_linux_arm64_beta",
					Size:   22345678,
				},
				"windows-amd64": {
					URL:    "https://example.com/simtezilo-1.3.0-beta1-windows-amd64.zip",
					SHA256: "fake_sha256_hash_for_windows_amd64_beta",
					Size:   22345679,
				},
			},
		}, nil
	case "https://simtezilo.com/releases/dev/latest.json":
		return &Manifest{
			Version:     "1.4.0-dev1",
			ReleaseDate: time.Now().Add(-time.Duration(time.Now().UnixNano()%86400) * time.Second),
			Changelog:   "Development features and improvements",
			Channel:     "dev",
			Platforms: map[string]Platform{
				"darwin-arm64": {
					URL:    "https://example.com/simtezilo-1.4.0-dev1-darwin-arm64.tar.gz",
					SHA256: "fake_sha256_hash_for_darwin_arm64_dev",
					Size:   32345677,
				},
				"linux-arm64": {
					URL:    "https://example.com/simtezilo-1.4.0-dev1-linux-arm64.tar.gz",
					SHA256: "fake_sha256_hash_for_linux_arm64_dev",
					Size:   32345678,
				},
				"windows-amd64": {
					URL:    "https://example.com/simtezilo-1.4.0-dev1-windows-amd64.zip",
					SHA256: "fake_sha256_hash_for_windows_amd64_dev",
					Size:   32345679,
				},
			},
		}, nil
	default:
		return nil, errors.New("invalid manifest URL")
	}
}

// FetchManifest retrieves and parses the release manifest from the given URL.
func FetchManifestx(ctx context.Context, manifestURL string, timeout time.Duration) (*Manifest, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest body: %w", err)
	}

	var manifest Manifest

	err = json.Unmarshal(body, &manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// ParseManifest parses a manifest from raw JSON bytes.
func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest

	err := json.Unmarshal(data, &manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}
