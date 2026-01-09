package updater

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
)

const (
	// DefaultCheckInterval is the default time between update checks.
	DefaultCheckInterval = 1 * time.Hour

	// DefaultHTTPTimeout is the default timeout for HTTP operations.
	DefaultHTTPTimeout = 30 * time.Second

	// DefaultDownloadTimeout is the timeout for downloading binaries.
	DefaultDownloadTimeout = 10 * time.Minute

	// DefaultChannel is the default update channel.
	DefaultChannel = "stable"

	// DefaultMaxFailures is the maximum number of consecutive failures before auto-rollback.
	DefaultMaxFailures = 3
)

// Config holds the updater configuration.
type Config struct {
	Enabled         bool
	ManifestURL     string
	Channel         string
	CheckInterval   time.Duration
	HTTPTimeout     time.Duration
	DownloadTimeout time.Duration
	AutoInstall     bool
	InstallDir      string
	DataDir         string
	BinaryName      string
	ServiceName     string
	UseSystemd      bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:         false,
		ManifestURL:     "",
		Channel:         DefaultChannel,
		CheckInterval:   DefaultCheckInterval,
		HTTPTimeout:     DefaultHTTPTimeout,
		DownloadTimeout: DefaultDownloadTimeout,
		AutoInstall:     false,
		InstallDir:      "/opt/simtezilo/bin",
		DataDir:         "/opt/simtezilo/data/update",
		BinaryName:      "simtezilo",
		ServiceName:     "simtezilo",
		UseSystemd:      true,
	}
}

// Updater is the main entry point for the update system.
type Updater struct {
	log zerolog.Logger
	cfg *Config

	checker    *Checker
	downloader *Downloader
	installer  *Installer

	currentVersion string
}

// New creates a new Updater instance.
func New(cfg *Config, currentVersion string, log zerolog.Logger) (*Updater, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.ManifestURL == "" {
		return nil, errors.New("manifest URL is required")
	}

	logger := log.With().Str("component", "updater").Logger()

	checker, err := NewChecker(CheckerConfig{
		ManifestURL:    cfg.ManifestURL,
		CheckInterval:  cfg.CheckInterval,
		HTTPTimeout:    cfg.HTTPTimeout,
		Channel:        cfg.Channel,
		CurrentVersion: currentVersion,
	}, logger)
	if err != nil {
		return nil, err
	}

	downloadDir := filepath.Join(cfg.DataDir, "downloads")
	downloader := NewDownloader(downloadDir, cfg.DownloadTimeout, logger)

	installer := NewInstaller(cfg.InstallDir, cfg.DataDir, cfg.BinaryName, cfg.UseSystemd, logger)

	return &Updater{
		log:            logger,
		cfg:            cfg,
		checker:        checker,
		downloader:     downloader,
		installer:      installer,
		currentVersion: currentVersion,
	}, nil
}

// Start begins the update checker if updates are enabled.
func (u *Updater) Start(ctx context.Context) {
	if !u.cfg.Enabled {
		u.log.Debug().Msg("Updates disabled")

		return
	}

	// Check if we should auto-rollback due to previous failures
	if u.installer.ShouldAutoRollback(DefaultMaxFailures) {
		u.log.Warn().Msg("Too many failed starts, initiating auto-rollback")

		err := u.installer.Rollback()
		if err != nil {
			u.log.Error().Err(err).Msg("Auto-rollback failed")
		}
	}

	// Confirm successful start after an update
	err := u.installer.ConfirmSuccess()
	if err != nil {
		u.log.Warn().Err(err).Msg("Failed to confirm successful update")
	}

	u.log.Info().
		Str("manifestURL", u.cfg.ManifestURL).
		Str("channel", u.cfg.Channel).
		Dur("checkInterval", u.cfg.CheckInterval).
		Msg("Starting update checker")

	u.checker.Start(ctx)
}

// Stop gracefully stops the updater.
func (u *Updater) Stop() {
	if u.checker != nil {
		u.checker.Stop()
	}
}

// CheckNow performs an immediate update check.
func (u *Updater) CheckNow() (*UpdateInfo, error) {
	return u.checker.CheckNow()
}

// Status returns the current update status.
func (u *Updater) Status() UpdateStatus {
	return u.checker.Status()
}

// AvailableUpdate returns information about an available update.
func (u *Updater) AvailableUpdate() *UpdateInfo {
	return u.checker.AvailableUpdate()
}

// DownloadUpdate downloads the available update.
func (u *Updater) DownloadUpdate(progressCb ProgressCallback) (string, error) {
	info := u.checker.AvailableUpdate()
	if info == nil {
		return "", errors.New("no update available")
	}

	u.checker.SetStatus(StatusDownloading)

	path, err := u.downloader.Download(info, progressCb)
	if err != nil {
		u.checker.SetStatus(StatusError)

		return "", err
	}

	u.checker.SetStatus(StatusReadyToInstall)

	return path, nil
}

// PrepareInstall stages the downloaded update for installation on next restart.
func (u *Updater) PrepareInstall(downloadPath string) error {
	info := u.checker.AvailableUpdate()
	if info == nil {
		return errors.New("no update available")
	}

	return u.installer.Prepare(downloadPath, info, u.currentVersion)
}

// RestartToApply requests a service restart to apply the update.
func (u *Updater) RestartToApply() error {
	u.checker.SetStatus(StatusInstalling)

	return u.installer.RestartService(u.cfg.ServiceName)
}

// DownloadAndPrepare is a convenience method that downloads and prepares an update.
func (u *Updater) DownloadAndPrepare(progressCb ProgressCallback) error {
	path, err := u.DownloadUpdate(progressCb)
	if err != nil {
		return err
	}

	return u.PrepareInstall(path)
}

// Rollback reverts to the previous version.
func (u *Updater) Rollback() error {
	return u.installer.Rollback()
}

// RollbackAvailable returns true if a previous version is available for rollback.
func (u *Updater) RollbackAvailable() bool {
	return u.installer.RollbackAvailable()
}

// RollbackVersion returns the version that would be rolled back to.
func (u *Updater) RollbackVersion() string {
	return u.installer.RollbackVersion()
}

// Installer returns the installer for use by external scripts.
func (u *Updater) Installer() *Installer {
	return u.installer
}

// Checker returns the update checker.
func (u *Updater) Checker() *Checker {
	return u.checker
}

// SetChannel updates the channel for update checking.
func (u *Updater) SetChannel(channel string) {
	if u.checker != nil {
		u.checker.SetChannel(channel)
	}
}
