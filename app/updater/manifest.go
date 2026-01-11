// Package updater provides self-update functionality for the application.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"
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

// FetchManifest retrieves and parses the release manifest from the given URL.
func FetchManifest(ctx context.Context, manifestURL string) (*Manifest, error) {
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
