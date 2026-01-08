package updater

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
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

	manifestURL   string
	checkInterval time.Duration
	httpTimeout   time.Duration
	channel       string

	currentVersion *Version

	mu             sync.RWMutex
	status         UpdateStatus
	lastCheck      time.Time
	lastError      error
	availableInfo  *UpdateInfo
	cachedManifest *Manifest

	stopChan chan struct{}
	doneChan chan struct{}
}

// CheckerConfig holds configuration for the update checker.
type CheckerConfig struct {
	ManifestURL    string
	CheckInterval  time.Duration
	HTTPTimeout    time.Duration
	Channel        string
	CurrentVersion string
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
		manifestURL:    cfg.ManifestURL,
		checkInterval:  cfg.CheckInterval,
		httpTimeout:    cfg.HTTPTimeout,
		channel:        cfg.Channel,
		currentVersion: currentVer,
		status:         StatusIdle,
		stopChan:       make(chan struct{}),
		doneChan:       make(chan struct{}),
	}, nil
}

// Start begins periodic update checking.
func (c *Checker) Start(ctx context.Context) {
	go c.runPeriodicCheck(ctx)
}

// Stop gracefully stops the update checker.
func (c *Checker) Stop() {
	close(c.stopChan)
	<-c.doneChan
}

// CheckNow performs an immediate update check.
func (c *Checker) CheckNow() (*UpdateInfo, error) {
	c.mu.Lock()
	c.status = StatusChecking
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), c.httpTimeout)
	defer cancel()

	manifest, err := FetchManifest(ctx, c.manifestURL, c.httpTimeout, c.log)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastCheck = time.Now()

	if err != nil {
		c.status = StatusError
		c.lastError = err
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
		c.availableInfo = nil

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
	c.status = StatusUpdateAvailable
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

// runPeriodicCheck runs the periodic update check loop.
func (c *Checker) runPeriodicCheck(ctx context.Context) {
	defer close(c.doneChan)

	// Initial check after a short delay
	initialDelay := time.NewTimer(10 * time.Second)
	select {
	case <-initialDelay.C:
		_, _ = c.CheckNow() //nolint:contextcheck // CheckNow creates its own timeout context
	case <-c.stopChan:
		initialDelay.Stop()

		return
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
		case <-c.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}
