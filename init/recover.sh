#!/bin/bash
# recover.sh - Rescue/Rollback script for failed updates
#
# This script is called by systemd AFTER the platform command has attempted
# to apply updates. It acts as a safety net to perform rollback if the
# platform command or simtezilo service fails repeatedly.
#
# Primary update installation is handled by: platform update-apply
# This script only handles: rescue/rollback scenarios

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

INSTALL_DIR="${INSTALL_DIR:-/opt/simtezilo/bin}"
DATA_DIR="${DATA_DIR:-/opt/simtezilo/data/update}"
BINARY_NAME="${BINARY_NAME:-simtezilo}"
STATE_FILE="${DATA_DIR}/update-state.json"
LOG_TAG="simtezilo-rescue"

# Maximum failures before automatic rollback
MAX_FAILURES=3

# =============================================================================
# Logging Functions
# =============================================================================

log() {
    logger -t "$LOG_TAG" "$@"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $@"
}

# =============================================================================
# State Management Functions
# =============================================================================

read_state_field() {
    local field="$1"
    local default="${2:-}"
    if [[ ! -f "$STATE_FILE" ]]; then
        echo "$default"
        return
    fi
    local value
    value=$(jq -r ".${field} // empty" "$STATE_FILE" 2>/dev/null || echo "")
    if [[ -z "$value" || "$value" == "null" ]]; then
        echo "$default"
    else
        echo "$value"
    fi
}

update_state() {
    local jq_expr="$1"
    if [[ -f "$STATE_FILE" ]]; then
        jq "$jq_expr" "$STATE_FILE" > "${STATE_FILE}.tmp" && mv "${STATE_FILE}.tmp" "$STATE_FILE"
    fi
}

# =============================================================================
# Rollback Functions
# =============================================================================

perform_rollback() {
    local current="${INSTALL_DIR}/${BINARY_NAME}"
    local rollback="${INSTALL_DIR}/${BINARY_NAME}.rollback"
    
    if [[ ! -f "$rollback" ]]; then
        log "No rollback binary available at $rollback"
        return 1
    fi

    log "Performing emergency rollback"

    # Move current to .failed
    if [[ -f "$current" ]]; then
        mv "$current" "${current}.failed" 2>/dev/null || true
    fi

    # Restore rollback binary
    if ! mv "$rollback" "$current"; then
        log "Failed to restore rollback binary"
        # Try to restore the failed binary
        mv "${current}.failed" "$current" 2>/dev/null || true
        return 1
    fi

    # Update state
    update_state '.status = "rolled_back"'
    
    log "Rollback complete - restored previous version"
    return 0
}

# =============================================================================
# Main Entry Point
# =============================================================================

main() {
    # Check if jq is available (needed for state file parsing)
    if ! command -v jq &> /dev/null; then
        log "jq not found, skipping rescue check"
        exit 0
    fi

    # Check if state file exists
    if [[ ! -f "$STATE_FILE" ]]; then
        # No state file means no update in progress - nothing to do
        exit 0
    fi

    # Read state
    local status
    local fail_count
    local pending_version
    local current_version

    status=$(read_state_field "status" "unknown")
    fail_count=$(read_state_field "failCount" "0")
    pending_version=$(read_state_field "pendingVersion" "unknown")
    current_version=$(read_state_field "currentVersion" "unknown")

    log "Rescue check: status=$status, failCount=$fail_count"

    # Only act on failed state with enough failures
    case "$status" in
        "failed")
            if [[ "$fail_count" -ge "$MAX_FAILURES" ]]; then
                log "Update failed $fail_count times (max: $MAX_FAILURES), triggering rollback"
                if perform_rollback; then
                    log "Emergency rollback successful"
                else
                    log "Emergency rollback failed - manual intervention required"
                fi
            else
                log "Update failed but only $fail_count times (rollback at $MAX_FAILURES)"
            fi
            ;;
        "installing")
            # If still installing, the platform command may have crashed
            # Increment fail count
            log "Found stale 'installing' state - platform command may have crashed"
            update_state ".status = \"failed\" | .lastError = \"platform command crashed during install\" | .failCount += 1"
            ;;
        "complete"|"pending"|"rolled_back"|"unknown")
            # Nothing to do for these states
            ;;
    esac

    exit 0
}

main "$@"
