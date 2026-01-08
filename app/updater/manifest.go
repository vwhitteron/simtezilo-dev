// Package updater provides self-update functionality for the application.
package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/rs/zerolog"
)

// Manifest represents the release manifest JSON structure hosted on the update server.
type Manifest struct {
	Version           string              `json:"version"`
	ReleaseDate       time.Time           `json:"releaseDate"`
	Channel           string              `json:"channel"`
	MinUpgradeVersion string              `json:"minUpgradeVersion,omitempty"`
	Changelog         string              `json:"changelog,omitempty"`
	Platforms         map[string]Platform `json:"platforms"`
}

// Platform contains platform-specific binary information.
type Platform struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// GetPlatformKey returns the platform key for the current OS/architecture.
func GetPlatformKey() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

// GetPlatform returns the platform information for the current OS/architecture.
// Returns nil if the current platform is not available in the manifest.
func (m *Manifest) GetPlatform() *Platform {
	key := GetPlatformKey()
	if p, ok := m.Platforms[key]; ok {
		return &p
	}

	return nil
}

// FetchManifest retrieves and parses the release manifest from the given URL.
func FetchManifest(manifestURL string, timeout time.Duration, log zerolog.Logger) (*Manifest, error) {
	client := &http.Client{
		Timeout: timeout,
	}

	log.Debug().
		Str("url", manifestURL).
		Dur("timeout", timeout).
		Msg("Fetching update manifest")

	resp, err := client.Get(manifestURL)
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
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	log.Debug().
		Str("version", manifest.Version).
		Str("channel", manifest.Channel).
		Time("releaseDate", manifest.ReleaseDate).
		Int("platforms", len(manifest.Platforms)).
		Msg("Manifest fetched successfully")

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
