#!/bin/bash
# apply-update.sh - ExecStartPre script for systemd to apply pending updates
#
# This script is called by systemd before starting the simtezilo service.
# It checks for a pending update and performs the binary swap if needed.

set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/simtezilo/bin}"
DATA_DIR="${DATA_DIR:-/opt/simtezilo/data/update}"
BINARY_NAME="${BINARY_NAME:-simtezilo}"
STATE_FILE="${DATA_DIR}/update-state.json"
LOG_TAG="simtezilo-update"

log() {
    logger -t "$LOG_TAG" "$@"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $@"
}

error() {
    log "ERROR: $@"
    exit 1
}

# Check if jq is available for JSON parsing
if ! command -v jq &> /dev/null; then
    log "jq not found, skipping update check"
    exit 0
fi

# Check if state file exists
if [[ ! -f "$STATE_FILE" ]]; then
    log "No update state file found, nothing to do"
    exit 0
fi

# Read update state
STATUS=$(jq -r '.status // "unknown"' "$STATE_FILE" 2>/dev/null || echo "unknown")
PENDING_VERSION=$(jq -r '.pendingVersion // "unknown"' "$STATE_FILE" 2>/dev/null || echo "unknown")
CURRENT_VERSION=$(jq -r '.currentVersion // "unknown"' "$STATE_FILE" 2>/dev/null || echo "unknown")
DOWNLOAD_PATH=$(jq -r '.downloadPath // ""' "$STATE_FILE" 2>/dev/null || echo "")
EXPECTED_SHA256=$(jq -r '.sha256 // ""' "$STATE_FILE" 2>/dev/null || echo "")
FAIL_COUNT=$(jq -r '.failCount // 0' "$STATE_FILE" 2>/dev/null || echo "0")

log "Update state: status=$STATUS, pending=$PENDING_VERSION, current=$CURRENT_VERSION, failCount=$FAIL_COUNT"

# Handle different states
case "$STATUS" in
    "pending")
        log "Applying pending update from $CURRENT_VERSION to $PENDING_VERSION"
        ;;
    "complete")
        log "Update already complete, cleaning up"
        # Remove rollback binary after successful boot
        rm -f "${INSTALL_DIR}/${BINARY_NAME}.rollback"
        rm -f "$STATE_FILE"
        exit 0
        ;;
    "failed")
        if [[ "$FAIL_COUNT" -ge 3 ]]; then
            log "Too many failures ($FAIL_COUNT), attempting rollback"
            if [[ -f "${INSTALL_DIR}/${BINARY_NAME}.rollback" ]]; then
                mv "${INSTALL_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}.failed" 2>/dev/null || true
                mv "${INSTALL_DIR}/${BINARY_NAME}.rollback" "${INSTALL_DIR}/${BINARY_NAME}"
                # Update state
                jq '.status = "rolled_back"' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
                log "Rollback complete"
            else
                log "No rollback binary available"
            fi
        fi
        exit 0
        ;;
    "rolled_back"|"installing"|"unknown")
        log "State is $STATUS, no action needed"
        exit 0
        ;;
esac

# Verify downloaded file exists
if [[ ! -f "$DOWNLOAD_PATH" ]]; then
    log "Downloaded binary not found at $DOWNLOAD_PATH"
    jq '.status = "failed" | .lastError = "downloaded binary not found" | .failCount += 1' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
    exit 0
fi

# Verify checksum if provided
if [[ -n "$EXPECTED_SHA256" ]]; then
    ACTUAL_SHA256=$(sha256sum "$DOWNLOAD_PATH" | cut -d' ' -f1)
    if [[ "$ACTUAL_SHA256" != "$EXPECTED_SHA256" ]]; then
        log "Checksum mismatch: expected $EXPECTED_SHA256, got $ACTUAL_SHA256"
        jq '.status = "failed" | .lastError = "checksum mismatch" | .failCount += 1' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
        rm -f "$DOWNLOAD_PATH"
        exit 0
    fi
    log "Checksum verified"
fi

# Update state to installing
jq '.status = "installing"' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"

CURRENT_BINARY="${INSTALL_DIR}/${BINARY_NAME}"
ROLLBACK_BINARY="${INSTALL_DIR}/${BINARY_NAME}.rollback"

# Backup current binary
if [[ -f "$CURRENT_BINARY" ]]; then
    log "Backing up current binary to $ROLLBACK_BINARY"
    if ! mv "$CURRENT_BINARY" "$ROLLBACK_BINARY"; then
        jq '.status = "failed" | .lastError = "failed to backup current binary" | .failCount += 1' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
        error "Failed to backup current binary"
    fi
fi

# Install new binary
log "Installing new binary from $DOWNLOAD_PATH"
if ! mv "$DOWNLOAD_PATH" "$CURRENT_BINARY"; then
    log "Failed to install new binary, restoring backup"
    mv "$ROLLBACK_BINARY" "$CURRENT_BINARY" 2>/dev/null || true
    jq '.status = "failed" | .lastError = "failed to install new binary" | .failCount += 1' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
    error "Failed to install new binary"
fi

# Set executable permissions
chmod 755 "$CURRENT_BINARY"

# Update state to complete
jq '.status = "complete"' "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"

log "Update to $PENDING_VERSION installed successfully"
exit 0
