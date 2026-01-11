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
	BaseURL         string
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
		return nil, errors.New("config is required")
	}

	if cfg.BaseURL == "" {
		return nil, errors.New("manifest URL is required")
	}

	logger := log.With().Str("component", "updater").Logger()

	checker, err := NewChecker(CheckerConfig{
		BaseURL:        cfg.BaseURL,
		CheckInterval:  cfg.CheckInterval,
		HTTPTimeout:    cfg.HTTPTimeout,
		Channel:        cfg.Channel,
		CurrentVersion: currentVersion,
		DataDir:        cfg.DataDir,
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

	// Check for existing downloads that might be ready to install
	u.CheckExistingDownloads()

	u.log.Info().
		Str("manifestURL", u.cfg.BaseURL).
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

// Downloader returns the downloader for checking download status.
func (u *Updater) Downloader() *Downloader {
	return u.downloader
}

// SetChannel updates the channel for update checking.
func (u *Updater) SetChannel(channel string) {
	if u.checker != nil {
		u.checker.SetChannel(channel)
	}
}

// CheckExistingDownloads checks if there are any valid downloaded updates in the download directory.
// If a valid newer version is found, it sets the status to ReadyToInstall.
func (u *Updater) CheckExistingDownloads() {
	// First, check for updates to get the latest version info
	updateInfo, err := u.checker.CheckNow()
	if err != nil {
		u.log.Debug().Err(err).Msg("Could not check for updates during startup")

		return
	}

	// If no update is available (already on latest or newer), nothing to check
	if updateInfo == nil {
		u.log.Debug().Msg("No updates available, skipping download check")

		return
	}

	// Check if the download already exists
	exists, downloadPath := u.downloader.DownloadExists(updateInfo)
	if !exists {
		u.log.Debug().Msg("No existing download found")

		return
	}

	u.log.Info().
		Str("path", downloadPath).
		Str("version", updateInfo.AvailableVersion).
		Msg("Found existing download")

	// Verify the downloaded file matches the expected checksum
	if updateInfo.SHA256 != "" {
		valid, err := u.downloader.VerifyFile(downloadPath, updateInfo.SHA256)
		if err != nil {
			u.log.Warn().Err(err).Str("path", downloadPath).Msg("Could not verify existing download")

			return
		}

		if !valid {
			u.log.Warn().Str("path", downloadPath).Msg("Existing download checksum mismatch, ignoring")

			return
		}
	}

	// Set status to ready to install (this preserves the availableInfo from CheckNow)
	u.checker.SetStatus(StatusReadyToInstall)
	u.log.Info().
		Str("version", updateInfo.AvailableVersion).
		Msg("Existing download is valid and ready to install")
}
