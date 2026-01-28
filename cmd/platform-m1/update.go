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
	// Update binary name.
	updateBinaryName = "simtezilo"

	// Update status constants.
	updateStatusPending    = "pending"
	updateStatusInstalling = "installing"
	updateStatusComplete   = "complete"
	updateStatusFailed     = "failed"
	updateStatusRolledBack = "rolled_back"
)

// installDir returns the installation directory path.
func (p *platform) installDir() string {
	return filepath.Join(p.baseDir, "bin")
}

// initDir returns the init scripts directory path.
func (p *platform) initDir() string {
	return filepath.Join(p.baseDir, "init")
}

// etcDir returns the configuration files directory path.
func (p *platform) etcDir() string {
	return filepath.Join(p.baseDir, "etc")
}

// dataDir returns the update data directory path.
func (p *platform) dataDir() string {
	return filepath.Join(p.baseDir, "data/update")
}

// stateFile returns the path to the update state file.
func (p *platform) stateFile() string {
	return filepath.Join(p.baseDir, "data/update/update-state.json")
}

func (p *platform) rollbackArchive() string {
	return filepath.Join(p.baseDir, "data/update/rollback.tgz")
}

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
func (p *platform) loadUpdateState() (*updateState, error) {
	data, err := os.ReadFile(p.stateFile())
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
func (p *platform) saveUpdateState(state *updateState) error {
	err := os.MkdirAll(p.dataDir(), 0o755)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	err = os.WriteFile(p.stateFile(), data, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// clearUpdateState removes the update state file from disk.
func (p *platform) clearUpdateState() error {
	err := os.Remove(p.stateFile())
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}

// updateApply processes a pending update based on the current update state.
// It handles pending, complete, failed, and rolled back states appropriately.
func (p *platform) updateApply() exitcode.Code {
	state, err := p.loadUpdateState()
	if err != nil {
		p.log.Error().Err(err).Msg("Failed to load update state")

		outputJSON(map[string]any{
			"result": "failure",
			"error":  err.Error(),
		})

		return exitcode.GeneralErr
	}

	if state == nil {
		p.log.Debug().Msg("No update state file found, nothing to do")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
		})

		return exitcode.Success
	}

	p.log.Info().
		Str("status", state.Status).
		Str("pending", state.PendingVersion).
		Str("current", state.CurrentVersion).
		Int("failCount", state.FailCount).
		Msg("Update state loaded")

	switch state.Status {
	case updateStatusPending:
		return p.applyPendingUpdate(state)
	case updateStatusComplete:
		return p.handleCompleteState(state)
	case updateStatusFailed:
		// If failed, just log and exit - rescue script will handle if needed
		p.log.Warn().
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
		p.log.Info().Str("status", state.Status).Msg("No action needed for current state")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
			"status": state.Status,
		})

		return exitcode.Success
	default:
		p.log.Warn().Str("status", state.Status).Msg("Unknown update state")

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
func (p *platform) applyPendingUpdate(state *updateState) exitcode.Code {
	p.log.Info().
		Str("from", state.CurrentVersion).
		Str("to", state.PendingVersion).
		Msg("Applying pending update")

	// Verify download exists and checksum
	if code := p.verifyUpdateDownload(state); code != exitcode.Success {
		return code
	}

	// Mark as installing
	state.Status = updateStatusInstalling

	err := p.saveUpdateState(state)
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to update state to installing")
	}

	// Extract and install update
	return p.extractAndInstallUpdate(state)
}

// verifyUpdateDownload checks that the downloaded update file exists and
// verifies its SHA256 checksum if one was provided in the state.
func (p *platform) verifyUpdateDownload(state *updateState) exitcode.Code {
	if state.DownloadPath == "" {
		return p.markUpdateFailed(state, "download path is empty")
	}

	_, err := os.Stat(state.DownloadPath)
	if err != nil {
		return p.markUpdateFailed(state, fmt.Sprintf("download file not found: %v", err))
	}

	if state.SHA256 != "" {
		checkErr := p.verifyChecksum(state.DownloadPath, state.SHA256)
		if checkErr != nil {
			return p.markUpdateFailed(state, fmt.Sprintf("checksum verification failed: %v", checkErr))
		}

		p.log.Debug().Msg("Checksum verified")
	}

	return exitcode.Success
}

// extractAndInstallUpdate extracts the downloaded archive to a temporary directory
// and proceeds with installation of the extracted files.
func (p *platform) extractAndInstallUpdate(state *updateState) exitcode.Code {
	extractDir := state.ExtractDir
	if extractDir == "" {
		extractDir = filepath.Join(p.dataDir(), "extract")
	}

	_ = os.RemoveAll(extractDir)

	err := os.MkdirAll(extractDir, 0o755)
	if err != nil {
		return p.markUpdateFailed(state, fmt.Sprintf("failed to create extract directory: %v", err))
	}

	p.log.Info().Str("archive", state.DownloadPath).Str("dest", extractDir).Msg("Extracting archive")

	err = p.extractArchive(state.DownloadPath, extractDir)
	if err != nil {
		_ = os.RemoveAll(extractDir)

		return p.markUpdateFailed(state, fmt.Sprintf("failed to extract archive: %v", err))
	}

	extractRoot := p.findExtractRoot(extractDir)

	return p.installExtractedUpdate(state, extractDir, extractRoot)
}

// installExtractedUpdate installs the extracted update by backing up the current
// binary, installing the new one, and copying any additional binaries and init scripts.
func (p *platform) installExtractedUpdate(state *updateState, extractDir, extractRoot string) exitcode.Code {
	extractedBinary := filepath.Join(extractRoot, "bin", updateBinaryName)

	_, err := os.Stat(extractedBinary)
	if err != nil {
		_ = os.RemoveAll(extractDir)

		return p.markUpdateFailed(state, "extracted binary not found at "+extractedBinary)
	}

	currentBinary := filepath.Join(p.installDir(), updateBinaryName)

	initSourceDir := filepath.Join(extractRoot, "init")
	etcSourceDir := filepath.Join(extractRoot, "etc")

	// Create rollback archive before making any changes
	err = p.createRollbackArchive()
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to create rollback archive")
		// Continue anyway - rollback archive is a safety feature, not critical for install
	}

	err = p.installInitScripts(initSourceDir)
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to install init scripts")
	}

	err = p.installConfigFiles(etcSourceDir)
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to install config files")
	}

	p.log.Debug().Str("from", extractedBinary).Str("to", currentBinary).Msg("Installing new binary")

	err = p.copyFile(extractedBinary, currentBinary)
	if err != nil {
		_ = os.RemoveAll(extractDir)

		return p.markUpdateFailed(state, fmt.Sprintf("failed to install new binary: %v", err))
	}

	err = os.Chmod(currentBinary, 0o755)
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to set executable permissions")
	}

	binSourceDir := filepath.Join(extractRoot, "bin")

	err = p.installAdditionalBinaries(binSourceDir, p.installDir(), updateBinaryName)
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to install additional binaries")
	}

	_ = os.RemoveAll(extractDir)
	_ = os.Remove(state.DownloadPath)

	state.Status = updateStatusComplete

	err = p.saveUpdateState(state)
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to save completion state")
	}

	p.log.Info().Str("version", state.PendingVersion).Msg("Update installed successfully")

	outputJSON(map[string]any{
		"result":  "success",
		"action":  "installed",
		"version": state.PendingVersion,
	})

	return exitcode.Success
}

// verifyChecksum calculates the SHA256 hash of a file and compares it against
// the expected value, returning an error if they don't match.
func (p *platform) verifyChecksum(filePath, expected string) error {
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
func (p *platform) findExtractRoot(extractDir string) string {
	// Check for Simtezilo/ subdirectory
	simteziloDir := filepath.Join(extractDir, "Simtezilo")

	_, err := os.Stat(simteziloDir)
	if err == nil {
		return simteziloDir
	}

	return extractDir
}

// handleCompleteState handles an update that has already completed successfully
// by cleaning up the rollback archive and clearing the state file.
func (p *platform) handleCompleteState(_ *updateState) exitcode.Code {
	p.log.Info().Msg("Update already complete, cleaning up")

	// Remove rollback archive
	_ = os.Remove(p.rollbackArchive())

	// Clear state
	_ = p.clearUpdateState()

	outputJSON(map[string]any{
		"result": "success",
		"action": "cleanup",
	})

	return exitcode.Success
}

// markUpdateFailed records a failure in the update state, incrementing the fail
// count and persisting the error reason to the state file.
func (p *platform) markUpdateFailed(state *updateState, reason string) exitcode.Code {
	p.log.Error().Str("reason", reason).Msg("Update failed")

	state.Status = updateStatusFailed
	state.LastError = reason
	state.FailCount++

	err := p.saveUpdateState(state)
	if err != nil {
		p.log.Warn().Err(err).Msg("Failed to save failed state")
	}

	outputJSON(map[string]any{
		"result":    "failure",
		"error":     reason,
		"failCount": state.FailCount,
	})

	return exitcode.GeneralErr
}

// updateRollback restores the previous version from the rollback archive.
func (p *platform) updateRollback() exitcode.Code {
	p.log.Info().Msg("Rolling back from archive")

	// Extract archive directly to base directory, overwriting existing files
	err := p.extractArchive(p.rollbackArchive(), p.baseDir)
	if err != nil {
		p.log.Error().Err(err).Msg("Failed to extract rollback archive")

		outputJSON(map[string]any{
			"result": "failure",
			"error":  fmt.Sprintf("failed to extract rollback archive: %v", err),
		})

		return exitcode.GeneralErr
	}

	// Update state
	state, _ := p.loadUpdateState()
	if state != nil {
		state.Status = updateStatusRolledBack
		_ = p.saveUpdateState(state)
	}

	p.log.Info().Msg("Rollback complete")

	outputJSON(map[string]any{
		"result": "success",
		"action": "rolled_back",
	})

	return exitcode.Success
}
