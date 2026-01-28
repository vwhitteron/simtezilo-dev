package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
)

const (
	// Update-related paths.
	updateInstallDir = "/opt/simtezilo/bin"
	updateInitDir    = "/opt/simtezilo/init"
	updateDataDir    = "/opt/simtezilo/data/update"
	updateBinaryName = "simtezilo"
	updateStateFile  = "/opt/simtezilo/data/update/update-state.json"

	// Update status constants.
	updateStatusPending    = "pending"
	updateStatusInstalling = "installing"
	updateStatusComplete   = "complete"
	updateStatusFailed     = "failed"
	updateStatusRolledBack = "rolled_back"
)

// updateState tracks the state of a pending installation (matches app/updater/installer.go).
type updateState struct {
	PendingVersion string    `json:"pendingVersion"`
	CurrentVersion string    `json:"currentVersion"`
	DownloadPath   string    `json:"downloadPath"`
	ExtractDir     string    `json:"extractDir"`
	SHA256         string    `json:"sha256"`
	Timestamp      time.Time `json:"timestamp"`
	Status         string    `json:"status"`
	FailCount      int       `json:"failCount"`
	LastError      string    `json:"lastError"`
}

// loadUpdateState reads and parses the update state file from disk.
// Returns nil with no error if the state file does not exist.
func (m *manager) loadUpdateState() (*updateState, error) {
	data, err := os.ReadFile(updateStateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state updateState

	err = json.Unmarshal(data, &state)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// saveUpdateState persists the update state to disk as JSON, creating the
// data directory if it doesn't exist.
func (m *manager) saveUpdateState(state *updateState) error {
	err := os.MkdirAll(updateDataDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	err = os.WriteFile(updateStateFile, data, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// clearUpdateState removes the update state file from disk.
func (m *manager) clearUpdateState() error {
	err := os.Remove(updateStateFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}

// updateApply processes a pending update based on the current update state.
// It handles pending, complete, failed, and rolled back states appropriately.
func (m *manager) updateApply() exitcode.Code {
	state, err := m.loadUpdateState()
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to load update state")

		outputJSON(map[string]any{
			"result": "failure",
			"error":  err.Error(),
		})

		return exitcode.GeneralErr
	}

	if state == nil {
		m.log.Debug().Msg("No update state file found, nothing to do")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
		})

		return exitcode.Success
	}

	m.log.Info().
		Str("status", state.Status).
		Str("pending", state.PendingVersion).
		Str("current", state.CurrentVersion).
		Int("failCount", state.FailCount).
		Msg("Update state loaded")

	switch state.Status {
	case updateStatusPending:
		return m.applyPendingUpdate(state)
	case updateStatusComplete:
		return m.handleCompleteState(state)
	case updateStatusFailed:
		// If failed, just log and exit - rescue script will handle if needed
		m.log.Warn().
			Int("failCount", state.FailCount).
			Str("lastError", state.LastError).
			Msg("Previous update failed")

		outputJSON(map[string]any{
			"result":    "failure",
			"status":    state.Status,
			"failCount": state.FailCount,
			"lastError": state.LastError,
		})

		return exitcode.GeneralErr
	case updateStatusRolledBack, updateStatusInstalling:
		m.log.Info().Str("status", state.Status).Msg("No action needed for current state")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
			"status": state.Status,
		})

		return exitcode.Success
	default:
		m.log.Warn().Str("status", state.Status).Msg("Unknown update state")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
			"status": state.Status,
		})

		return exitcode.Success
	}
}

// applyPendingUpdate processes an update in pending state by verifying the
// download, extracting the archive, and installing the new binary.
func (m *manager) applyPendingUpdate(state *updateState) exitcode.Code {
	m.log.Info().
		Str("from", state.CurrentVersion).
		Str("to", state.PendingVersion).
		Msg("Applying pending update")

	// Verify download exists and checksum
	if code := m.verifyUpdateDownload(state); code != exitcode.Success {
		return code
	}

	// Mark as installing
	state.Status = updateStatusInstalling

	err := m.saveUpdateState(state)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to update state to installing")
	}

	// Extract and install update
	return m.extractAndInstallUpdate(state)
}

// verifyUpdateDownload checks that the downloaded update file exists and
// verifies its SHA256 checksum if one was provided in the state.
func (m *manager) verifyUpdateDownload(state *updateState) exitcode.Code {
	if state.DownloadPath == "" {
		return m.markUpdateFailed(state, "download path is empty")
	}

	_, err := os.Stat(state.DownloadPath)
	if err != nil {
		return m.markUpdateFailed(state, fmt.Sprintf("download file not found: %v", err))
	}

	if state.SHA256 != "" {
		checkErr := m.verifyChecksum(state.DownloadPath, state.SHA256)
		if checkErr != nil {
			return m.markUpdateFailed(state, fmt.Sprintf("checksum verification failed: %v", checkErr))
		}

		m.log.Debug().Msg("Checksum verified")
	}

	return exitcode.Success
}

// extractAndInstallUpdate extracts the downloaded archive to a temporary directory
// and proceeds with installation of the extracted files.
func (m *manager) extractAndInstallUpdate(state *updateState) exitcode.Code {
	extractDir := state.ExtractDir
	if extractDir == "" {
		extractDir = filepath.Join(updateDataDir, "extract")
	}

	_ = os.RemoveAll(extractDir)

	err := os.MkdirAll(extractDir, 0o755)
	if err != nil {
		return m.markUpdateFailed(state, fmt.Sprintf("failed to create extract directory: %v", err))
	}

	m.log.Info().Str("archive", state.DownloadPath).Str("dest", extractDir).Msg("Extracting archive")

	err = m.extractArchive(state.DownloadPath, extractDir)
	if err != nil {
		_ = os.RemoveAll(extractDir)

		return m.markUpdateFailed(state, fmt.Sprintf("failed to extract archive: %v", err))
	}

	extractRoot := m.findExtractRoot(extractDir)

	return m.installExtractedUpdate(state, extractDir, extractRoot)
}

// installExtractedUpdate installs the extracted update by backing up the current
// binary, installing the new one, and copying any additional binaries and init scripts.
func (m *manager) installExtractedUpdate(state *updateState, extractDir, extractRoot string) exitcode.Code {
	extractedBinary := filepath.Join(extractRoot, "bin", updateBinaryName)

	_, err := os.Stat(extractedBinary)
	if err != nil {
		_ = os.RemoveAll(extractDir)

		return m.markUpdateFailed(state, "extracted binary not found at "+extractedBinary)
	}

	currentBinary := filepath.Join(updateInstallDir, updateBinaryName)
	rollbackBinary := filepath.Join(updateInstallDir, updateBinaryName+".rollback")

	initSourceDir := filepath.Join(extractRoot, "init")

	err = m.installInitScripts(initSourceDir)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to install init scripts")
	}

	_, err = os.Stat(currentBinary)
	if err == nil {
		m.log.Debug().Str("to", rollbackBinary).Msg("Backing up current binary")

		copyErr := m.copyFile(currentBinary, rollbackBinary)
		if copyErr != nil {
			_ = os.RemoveAll(extractDir)

			return m.markUpdateFailed(state, fmt.Sprintf("failed to backup current binary: %v", copyErr))
		}
	}

	m.log.Debug().Str("from", extractedBinary).Str("to", currentBinary).Msg("Installing new binary")

	err = m.copyFile(extractedBinary, currentBinary)
	if err != nil {
		_ = m.copyFile(rollbackBinary, currentBinary)
		_ = os.RemoveAll(extractDir)

		return m.markUpdateFailed(state, fmt.Sprintf("failed to install new binary: %v", err))
	}

	err = os.Chmod(currentBinary, 0o755)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to set executable permissions")
	}

	binSourceDir := filepath.Join(extractRoot, "bin")

	err = m.installAdditionalBinaries(binSourceDir, updateInstallDir, updateBinaryName)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to install additional binaries")
	}

	_ = os.RemoveAll(extractDir)
	_ = os.Remove(state.DownloadPath)

	state.Status = updateStatusComplete

	err = m.saveUpdateState(state)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to save completion state")
	}

	m.log.Info().Str("version", state.PendingVersion).Msg("Update installed successfully")

	outputJSON(map[string]any{
		"result":  "success",
		"action":  "installed",
		"version": state.PendingVersion,
	})

	return exitcode.Success
}

// verifyChecksum calculates the SHA256 hash of a file and compares it against
// the expected value, returning an error if they don't match.
func (m *manager) verifyChecksum(filePath, expected string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()

	_, err = io.Copy(hasher, file)
	if err != nil {
		return fmt.Errorf("failed to hash file: %w", err)
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

// findExtractRoot determines the root directory of the extracted archive contents,
// checking for a "Simtezilo" subdirectory or returning the extract directory itself.
func (m *manager) findExtractRoot(extractDir string) string {
	// Check for Simtezilo/ subdirectory
	simteziloDir := filepath.Join(extractDir, "Simtezilo")

	_, err := os.Stat(simteziloDir)
	if err == nil {
		return simteziloDir
	}

	return extractDir
}

// handleCompleteState handles an update that has already completed successfully
// by cleaning up the rollback binary and clearing the state file.
func (m *manager) handleCompleteState(_ *updateState) exitcode.Code {
	m.log.Info().Msg("Update already complete, cleaning up")

	// Remove rollback binary
	rollbackBinary := filepath.Join(updateInstallDir, updateBinaryName+".rollback")
	_ = os.Remove(rollbackBinary)

	// Clear state
	_ = m.clearUpdateState()

	outputJSON(map[string]any{
		"result": "success",
		"action": "cleanup",
	})

	return exitcode.Success
}

// markUpdateFailed records a failure in the update state, incrementing the fail
// count and persisting the error reason to the state file.
func (m *manager) markUpdateFailed(state *updateState, reason string) exitcode.Code {
	m.log.Error().Str("reason", reason).Msg("Update failed")

	state.Status = updateStatusFailed
	state.LastError = reason
	state.FailCount++

	err := m.saveUpdateState(state)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to save failed state")
	}

	outputJSON(map[string]any{
		"result":    "failure",
		"error":     reason,
		"failCount": state.FailCount,
	})

	return exitcode.GeneralErr
}

// updateRollback restores the previous binary version from the rollback backup,
// moving the current (failed) binary aside and updating the state file.
func (m *manager) updateRollback() exitcode.Code {
	currentBinary := filepath.Join(updateInstallDir, updateBinaryName)
	rollbackBinary := filepath.Join(updateInstallDir, updateBinaryName+".rollback")

	_, err := os.Stat(rollbackBinary)
	if err != nil {
		m.log.Error().Msg("No rollback binary available")

		outputJSON(map[string]any{
			"result": "failure",
			"error":  "no rollback binary available",
		})

		return exitcode.GeneralErr
	}

	m.log.Info().Msg("Rolling back to previous version")

	// Move current to .failed
	failedBinary := filepath.Join(updateInstallDir, updateBinaryName+".failed")

	_, err = os.Stat(currentBinary)
	if err == nil {
		renameErr := os.Rename(currentBinary, failedBinary)
		if renameErr != nil {
			m.log.Error().Err(renameErr).Msg("Failed to move current binary to .failed")

			outputJSON(map[string]any{
				"result": "failure",
				"error":  fmt.Sprintf("failed to move current binary: %v", renameErr),
			})

			return exitcode.GeneralErr
		}
	}

	// Restore rollback
	err = os.Rename(rollbackBinary, currentBinary)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to restore rollback binary")

		// Try to restore the failed binary
		_ = os.Rename(failedBinary, currentBinary)

		outputJSON(map[string]any{
			"result": "failure",
			"error":  fmt.Sprintf("failed to restore rollback binary: %v", err),
		})

		return exitcode.GeneralErr
	}

	// Update state
	state, _ := m.loadUpdateState()
	if state != nil {
		state.Status = updateStatusRolledBack
		_ = m.saveUpdateState(state)
	}

	m.log.Info().Msg("Rollback complete")

	outputJSON(map[string]any{
		"result": "success",
		"action": "rolled_back",
	})

	return exitcode.Success
}
