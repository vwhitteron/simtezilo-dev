package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rs/zerolog"
)

// InstallState tracks the state of a pending installation.
type InstallState struct {
	PendingVersion string    `json:"pendingVersion"`
	CurrentVersion string    `json:"currentVersion"`
	DownloadPath   string    `json:"downloadPath"`
	SHA256         string    `json:"sha256"`
	Timestamp      time.Time `json:"timestamp"`
	Status         string    `json:"status"` // "pending", "installing", "failed", "complete"
	FailCount      int       `json:"failCount"`
	LastError      string    `json:"lastError,omitempty"`
}

// Installer handles the installation of downloaded updates.
type Installer struct {
	log zerolog.Logger

	installDir string // Directory containing the running binary
	dataDir    string // Directory for state files
	binaryName string // Name of the binary (e.g., "simtezilo")

	useSystemd bool // Whether to use systemd for restart
}

// NewInstaller creates a new Installer instance.
func NewInstaller(installDir, dataDir, binaryName string, useSystemd bool, log zerolog.Logger) *Installer {
	return &Installer{
		log:        log.With().Str("component", "installer").Logger(),
		installDir: installDir,
		dataDir:    dataDir,
		binaryName: binaryName,
		useSystemd: useSystemd,
	}
}

// stateFilePath returns the path to the install state file.
func (i *Installer) stateFilePath() string {
	return filepath.Join(i.dataDir, "update-state.json")
}

// LoadState loads the current installation state from disk.
func (i *Installer) LoadState() (*InstallState, error) {
	data, err := os.ReadFile(i.stateFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state InstallState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// SaveState persists the installation state to disk.
func (i *Installer) SaveState(state *InstallState) error {
	if err := os.MkdirAll(i.dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(i.stateFilePath(), data, 0o644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// ClearState removes the installation state file.
func (i *Installer) ClearState() error {
	path := i.stateFilePath()
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}

// Prepare stages an update for installation on next restart.
// The actual binary swap happens via the ExecStartPre script in systemd.
func (i *Installer) Prepare(downloadPath string, info *UpdateInfo, currentVersion string) error {
	i.log.Info().
		Str("download", downloadPath).
		Str("version", info.AvailableVersion).
		Msg("Preparing update for installation")

	// Verify the downloaded file exists
	if _, err := os.Stat(downloadPath); err != nil {
		return fmt.Errorf("downloaded file not found: %w", err)
	}

	// Save state for the ExecStartPre script to pick up
	state := &InstallState{
		PendingVersion: info.AvailableVersion,
		CurrentVersion: currentVersion,
		DownloadPath:   downloadPath,
		SHA256:         info.SHA256,
		Timestamp:      time.Now(),
		Status:         "pending",
	}

	err := i.SaveState(state)
	if err != nil {
		return fmt.Errorf("failed to save install state: %w", err)
	}

	i.log.Info().Msg("Update staged for installation on next restart")

	return nil
}

// ApplyPendingUpdate is called by the ExecStartPre script to perform the actual binary swap.
// This should be called from a separate helper binary or script, not the main application.
func (i *Installer) ApplyPendingUpdate() error {
	state, err := i.LoadState()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	if state == nil || state.Status != "pending" {
		i.log.Debug().Msg("No pending update to apply")

		return nil
	}

	i.log.Info().
		Str("from", state.CurrentVersion).
		Str("to", state.PendingVersion).
		Msg("Applying pending update")

	state.Status = "installing"
	if err := i.SaveState(state); err != nil {
		return fmt.Errorf("failed to update state: %w", err)
	}

	currentBinary := filepath.Join(i.installDir, i.binaryName)
	rollbackBinary := filepath.Join(i.installDir, i.binaryName+".rollback")

	// Backup current binary
	if _, err := os.Stat(currentBinary); err == nil {
		i.log.Debug().Str("to", rollbackBinary).Msg("Backing up current binary")
		err := os.Rename(currentBinary, rollbackBinary)
		if err != nil {
			state.Status = "failed"
			state.LastError = fmt.Sprintf("failed to backup current binary: %v", err)
			state.FailCount++
			_ = i.SaveState(state)

			return fmt.Errorf("failed to backup current binary: %w", err)
		}
	}

	// Move new binary into place
	i.log.Debug().
		Str("from", state.DownloadPath).
		Str("to", currentBinary).
		Msg("Installing new binary")

	if err := os.Rename(state.DownloadPath, currentBinary); err != nil {
		// Try to restore from backup
		_ = os.Rename(rollbackBinary, currentBinary)
		state.Status = "failed"
		state.LastError = fmt.Sprintf("failed to install new binary: %v", err)
		state.FailCount++
		_ = i.SaveState(state)

		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Ensure executable permissions
	if err := os.Chmod(currentBinary, 0o755); err != nil {
		i.log.Warn().Err(err).Msg("Failed to set executable permissions")
	}

	state.Status = "complete"
	if err := i.SaveState(state); err != nil {
		i.log.Warn().Err(err).Msg("Failed to save completion state")
	}

	i.log.Info().
		Str("version", state.PendingVersion).
		Msg("Update installed successfully")

	return nil
}

// Rollback reverts to the previous version if available.
func (i *Installer) Rollback() error {
	currentBinary := filepath.Join(i.installDir, i.binaryName)
	rollbackBinary := filepath.Join(i.installDir, i.binaryName+".rollback")

	if _, err := os.Stat(rollbackBinary); err != nil {
		return errors.New("no rollback binary available")
	}

	i.log.Info().Msg("Rolling back to previous version")

	// Move current to .failed
	failedBinary := filepath.Join(i.installDir, i.binaryName+".failed")
	if _, err := os.Stat(currentBinary); err == nil {
		err := os.Rename(currentBinary, failedBinary)
		if err != nil {
			return fmt.Errorf("failed to move current binary: %w", err)
		}
	}

	// Restore rollback
	err := os.Rename(rollbackBinary, currentBinary)
	if err != nil {
		return fmt.Errorf("failed to restore rollback binary: %w", err)
	}

	// Update state
	state, _ := i.LoadState()
	if state != nil {
		state.Status = "rolled_back"
		_ = i.SaveState(state)
	}

	i.log.Info().Msg("Rollback complete")

	return nil
}

// RestartService triggers a service restart via systemd.
func (i *Installer) RestartService(serviceName string) error {
	if runtime.GOOS != "linux" {
		return errors.New("systemd restart only supported on Linux")
	}

	if !i.useSystemd {
		return errors.New("systemd integration not enabled")
	}

	i.log.Info().Str("service", serviceName).Msg("Requesting service restart")

	cmd := exec.Command("systemctl", "restart", serviceName)
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	// Don't wait for completion - we'll be killed by the restart
	return nil
}

// ShouldAutoRollback checks if automatic rollback should be triggered.
// Returns true if there have been too many consecutive failed starts.
func (i *Installer) ShouldAutoRollback(maxFailures int) bool {
	state, err := i.LoadState()
	if err != nil || state == nil {
		return false
	}

	return state.Status == "failed" && state.FailCount >= maxFailures
}

// ConfirmSuccess should be called after the application starts successfully
// to mark the update as confirmed and clean up.
func (i *Installer) ConfirmSuccess() error {
	state, err := i.LoadState()
	if err != nil {
		return err
	}

	if state == nil || state.Status != "complete" {
		return nil
	}

	i.log.Info().
		Str("version", state.PendingVersion).
		Msg("Confirming successful update")

	// Remove rollback binary
	rollbackBinary := filepath.Join(i.installDir, i.binaryName+".rollback")
	if err := os.Remove(rollbackBinary); err != nil && !os.IsNotExist(err) {
		i.log.Warn().Err(err).Msg("Failed to remove rollback binary")
	}

	// Clear state
	return i.ClearState()
}
