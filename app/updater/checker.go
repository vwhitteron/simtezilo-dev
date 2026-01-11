package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// UpdateStatus represents the current state of the updater.
type UpdateStatus int

const (
	StatusIdle UpdateStatus = iota
	StatusChecking
	StatusUpdateAvailable
	StatusDownloading
	StatusReadyToInstall
	StatusInstalling
	StatusError
)

func (s UpdateStatus) String() string {
	return [...]string{
		"idle",
		"checking",
		"update_available",
		"downloading",
		"ready_to_install",
		"installing",
		"error",
	}[s]
}

// UpdateInfo contains information about an available update.
type UpdateInfo struct {
	CurrentVersion   string
	AvailableVersion string
	Channel          string
	Changelog        string
	DownloadURL      string
	DownloadSize     int64
	SHA256           string
	ReleaseDate      time.Time
}

// Checker handles update checking and orchestration.
type Checker struct {
	log zerolog.Logger

	baseURL       string // Base URL without channel-specific path
	checkInterval time.Duration
	httpTimeout   time.Duration
	channel       string
	dataDir       string

	currentVersion *Version

	mu             sync.RWMutex
	status         UpdateStatus
	lastCheck      time.Time
	lastError      error
	availableInfo  *UpdateInfo
	cachedManifest *Manifest

	// Lifecycle management using context and WaitGroup pattern
	// - cancel is called to signal shutdown to the background goroutine
	// - wg tracks when the goroutine has finished cleanup
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// CheckerConfig holds configuration for the update checker.
type CheckerConfig struct {
	BaseURL        string
	CheckInterval  time.Duration
	HTTPTimeout    time.Duration
	Channel        string
	CurrentVersion string
	DataDir        string
}

// NewChecker creates a new update checker.
func NewChecker(cfg CheckerConfig, log zerolog.Logger) (*Checker, error) {
	currentVer, err := ParseVersion(cfg.CurrentVersion)
	if err != nil {
		// If version is "dev" or unparseable, create a zero version
		currentVer = &Version{Major: 0, Minor: 0, Patch: 0, Raw: cfg.CurrentVersion}
	}

	return &Checker{
		log:            log.With().Str("component", "updater").Logger(),
		baseURL:        cfg.BaseURL,
		checkInterval:  cfg.CheckInterval,
		httpTimeout:    cfg.HTTPTimeout,
		channel:        cfg.Channel,
		dataDir:        cfg.DataDir,
		currentVersion: currentVer,
		status:         StatusIdle,
	}, nil
}

// Start begins periodic update checking.
func (c *Checker) Start(ctx context.Context) {
	// Create a cancellable context for this checker
	childCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.wg.Add(1)

	go c.runPeriodicCheck(childCtx)
}

// Stop gracefully stops the update checker.
func (c *Checker) Stop() {
	if c.cancel != nil {
		c.cancel()
	}

	c.wg.Wait()
}

// CheckNow performs an immediate update check.
func (c *Checker) CheckNow() (*UpdateInfo, error) {
	c.mu.Lock()
	c.status = StatusChecking
	baseURL := c.baseURL
	channel := c.channel
	dataDir := c.dataDir
	c.mu.Unlock()

	// For custom channel, check for local files instead of fetching from URL
	if channel == "custom" {
		return c.checkCustomUpdate(dataDir)
	}

	// Build manifest URL based on current channel
	manifestURL := fmt.Sprintf("%s/%s/latest.json", baseURL, channel)

	ctx, cancel := context.WithTimeout(context.Background(), c.httpTimeout)
	defer cancel()

	log.Debug().
		Str("url", manifestURL).
		Dur("timeout", c.httpTimeout).
		Msg("Fetching update manifest")

	manifest, err := FetchManifest(ctx, manifestURL)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastCheck = time.Now()

	if err != nil {
		c.status = StatusError
		c.lastError = err
		c.availableInfo = nil // Clear any previous update info on error
		c.log.Warn().Err(err).Msg("Failed to check for updates")

		return nil, err
	}

	c.cachedManifest = manifest

	// Check if manifest is for the correct channel
	if manifest.Channel != c.channel && c.channel != "" {
		c.log.Debug().
			Str("expected", c.channel).
			Str("got", manifest.Channel).
			Msg("Manifest channel mismatch, skipping")
		c.status = StatusIdle

		return nil, nil
	}

	c.log.Debug().
		Str("version", manifest.Version).
		Str("channel", manifest.Channel).
		Time("releaseDate", manifest.ReleaseDate).
		Int("platforms", len(manifest.Platforms)).
		Msg("Manifest fetched successfully")

	// Parse available version
	availableVer, err := ParseVersion(manifest.Version)
	if err != nil {
		c.status = StatusError
		c.lastError = err
		c.log.Warn().Err(err).Str("version", manifest.Version).Msg("Failed to parse manifest version")

		return nil, err
	}

	// Check minimum upgrade version if specified
	if manifest.MinUpgradeVersion != "" {
		minVer, err := ParseVersion(manifest.MinUpgradeVersion)
		if err == nil && c.currentVersion.LessThan(minVer) {
			c.log.Warn().
				Str("current", c.currentVersion.String()).
				Str("minimum", minVer.String()).
				Msg("Current version is below minimum upgrade version")
			// Still allow the update, but log a warning
		}
	}

	// Compare versions
	if !availableVer.GreaterThan(c.currentVersion) {
		c.log.Debug().
			Str("current", c.currentVersion.String()).
			Str("available", availableVer.String()).
			Msg("Already running latest version")
		c.status = StatusIdle
		// Only clear availableInfo if it's not a custom update
		if c.availableInfo == nil || c.availableInfo.Channel != "custom" {
			c.availableInfo = nil
		}

		return nil, nil
	}

	// Get platform-specific information
	platform := manifest.GetPlatform()
	if platform == nil {
		c.log.Warn().
			Str("platform", GetPlatformKey()).
			Msg("No binary available for current platform")
		c.status = StatusIdle

		return nil, nil
	}

	// Update available
	c.availableInfo = &UpdateInfo{
		CurrentVersion:   c.currentVersion.String(),
		AvailableVersion: availableVer.String(),
		Channel:          manifest.Channel,
		Changelog:        manifest.Changelog,
		DownloadURL:      platform.URL,
		DownloadSize:     platform.Size,
		SHA256:           platform.SHA256,
		ReleaseDate:      manifest.ReleaseDate,
	}
	// Only change status to UpdateAvailable if not already in a more advanced state
	if c.status != StatusReadyToInstall && c.status != StatusDownloading && c.status != StatusInstalling {
		c.status = StatusUpdateAvailable
	}

	c.lastError = nil

	c.log.Info().
		Str("current", c.currentVersion.String()).
		Str("available", availableVer.String()).
		Str("channel", manifest.Channel).
		Msg("Update available")

	return c.availableInfo, nil
}

// Status returns the current update status.
func (c *Checker) Status() UpdateStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.status
}

// SetStatus updates the current status (used by Installer).
func (c *Checker) SetStatus(status UpdateStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.status = status
}

// SetAvailableUpdate sets the available update information (for custom uploads).
func (c *Checker) SetAvailableUpdate(info *UpdateInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.availableInfo = info
}

// LastCheck returns the time of the last update check.
func (c *Checker) LastCheck() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lastCheck
}

// LastError returns the last error encountered during update checking.
func (c *Checker) LastError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lastError
}

// AvailableUpdate returns information about an available update, or nil if none.
func (c *Checker) AvailableUpdate() *UpdateInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.availableInfo
}

// CurrentVersion returns the current version string.
func (c *Checker) CurrentVersion() string {
	return c.currentVersion.String()
}

// SetChannel updates the channel for update checking.
func (c *Checker) SetChannel(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.channel = channel
	c.log.Info().Str("channel", channel).Msg("Update channel changed")
}

// runPeriodicCheck runs the periodic update check loop.
func (c *Checker) runPeriodicCheck(ctx context.Context) {
	defer c.wg.Done()

	// Initial check after a short delay
	initialDelay := time.NewTimer(10 * time.Second)
	select {
	case <-initialDelay.C:
		_, _ = c.CheckNow() //nolint:contextcheck // CheckNow creates its own timeout context
	case <-ctx.Done():
		initialDelay.Stop()

		return
	}

	ticker := time.NewTicker(c.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_, _ = c.CheckNow() //nolint:contextcheck // CheckNow creates its own timeout context
		case <-ctx.Done():
			return
		}
	}
}

// checkCustomUpdate checks for custom update files in the data directory.
func (c *Checker) checkCustomUpdate(dataDir string) (*UpdateInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastCheck = time.Now()

	if dataDir == "" {
		c.status = StatusIdle
		c.lastError = errors.New("data directory not configured")

		return nil, c.lastError
	}

	downloadsDir := filepath.Join(dataDir, "downloads")

	// Look for files starting with "custom-"
	files, err := os.ReadDir(downloadsDir)
	if err != nil {
		if os.IsNotExist(err) {
			c.log.Debug().Msg("Downloads directory does not exist, no custom update found")
			c.status = StatusIdle
			// Don't clear availableInfo for custom channel - it may have been set via upload
			return nil, nil
		}

		c.status = StatusError
		c.lastError = err

		return nil, err
	}

	var customFile string

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if strings.HasPrefix(file.Name(), "custom-") {
			customFile = filepath.Join(downloadsDir, file.Name())

			break
		}
	}

	if customFile == "" {
		c.log.Debug().Msg("No custom update file found")
		c.status = StatusIdle
		// Don't clear availableInfo for custom channel - it may have been set via upload
		return nil, nil
	}

	c.log.Debug().Str("file", customFile).Msg("Found custom update file")

	// Extract and parse manifest.json
	manifest, err := c.extractManifest(customFile)
	if err != nil {
		c.status = StatusError
		c.lastError = fmt.Errorf("failed to extract manifest: %w", err)
		c.log.Warn().Err(err).Str("file", customFile).Msg("Failed to extract manifest from custom update")

		return nil, c.lastError
	}

	// Get file size
	fileInfo, err := os.Stat(customFile)
	if err != nil {
		c.log.Warn().Err(err).Msg("Failed to get custom file size")
	}

	// Create UpdateInfo from manifest
	c.availableInfo = &UpdateInfo{
		CurrentVersion:   c.currentVersion.String(),
		AvailableVersion: manifest.Version,
		Channel:          "custom",
		Changelog:        manifest.Changelog,
		DownloadURL:      "",
		DownloadSize:     fileInfo.Size(),
		SHA256:           "",
		ReleaseDate:      manifest.ReleaseDate,
	}

	c.status = StatusReadyToInstall
	c.lastError = nil

	c.log.Info().
		Str("version", manifest.Version).
		Str("file", customFile).
		Msg("Custom update detected")

	return c.availableInfo, nil
}

// extractManifest extracts manifest.json from a zip or tar.gz archive.
func (c *Checker) extractManifest(archivePath string) (*Manifest, error) {
	ext := filepath.Ext(archivePath)

	if ext == ".zip" {
		return c.extractManifestFromZip(archivePath)
	}

	if ext == ".gz" || strings.HasSuffix(archivePath, ".tar.gz") {
		return c.extractManifestFromTarGz(archivePath)
	}

	return nil, fmt.Errorf("unsupported archive format: %s", ext)
}

// extractManifestFromZip extracts manifest.json from a zip archive.
func (c *Checker) extractManifestFromZip(zipPath string) (*Manifest, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if filepath.Base(f.Name) == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("failed to open manifest.json: %w", err)
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("failed to read manifest.json: %w", err)
			}

			var manifest Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("failed to parse manifest.json: %w", err)
			}

			return &manifest, nil
		}
	}

	return nil, errors.New("manifest.json not found in archive")
}

// extractManifestFromTarGz extracts manifest.json from a tar.gz archive.
func (c *Checker) extractManifestFromTarGz(tarGzPath string) (*Manifest, error) {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open tar.gz: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read tar header: %w", err)
		}

		if filepath.Base(header.Name) == "manifest.json" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read manifest.json: %w", err)
			}

			var manifest Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("failed to parse manifest.json: %w", err)
			}

			return &manifest, nil
		}
	}

	return nil, errors.New("manifest.json not found in archive")
}
