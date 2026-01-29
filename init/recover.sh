#!/bin/bash
# recover.sh - Rescue/Rollback script for failed updates
#
# This script is called by systemd when the simtezilo service fails to start.
# It tracks consecutive failures and performs an automatic rollback if failures
# exceed the configured threshold.
#
# Design principles:
# - Minimal external dependencies (no jq, python, etc.)
# - Simple text file for failure counter
# - Independent from platform command state
# - Fail-safe: errors are logged but don't prevent operation

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================

BASE_DIR="${BASE_DIR:-/opt/simtezilo}"
DATA_DIR="${BASE_DIR}/data"
UPDATE_DIR="${DATA_DIR}/update"
ROLLBACK_ARCHIVE="${UPDATE_DIR}/rollback.tgz"
COUNTER_FILE="${DATA_DIR}/failed_start.counter"
LOG_TAG="simtezilo-recover"

# Maximum consecutive startup failures before automatic rollback
MAX_FAILURES=10

# =============================================================================
# Functions
# =============================================================================

log() {
    logger -t "$LOG_TAG" "$@" 2>/dev/null || true
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $@" >&2
}

# Read current failure count from counter file
read_counter() {
    if [[ -f "$COUNTER_FILE" ]]; then
        local count
        count=$(cat "$COUNTER_FILE" 2>/dev/null || echo "0")
        # Validate it's a number
        if [[ "$count" =~ ^[0-9]+$ ]]; then
            echo "$count"
        else
            echo "0"
        fi
    else
        echo "0"
    fi
}

# Increment failure counter
increment_counter() {
    local count
    count=$(read_counter)
    count=$((count + 1))
    
    mkdir -p "$DATA_DIR" 2>/dev/null || true
    echo "$count" > "$COUNTER_FILE" 2>/dev/null || true
    
    echo "$count"
}

# Reset failure counter to zero
reset_counter() {
    rm -f "$COUNTER_FILE" 2>/dev/null || true
}

# Perform emergency rollback from archive
perform_rollback() {
    if [[ ! -f "$ROLLBACK_ARCHIVE" ]]; then
        log "ERROR: No rollback archive available at $ROLLBACK_ARCHIVE"
        return 1
    fi

    log "Performing emergency rollback from archive"

    # Extract archive directly to base directory, overwriting existing files
    if ! tar -xzf "$ROLLBACK_ARCHIVE" -C "$BASE_DIR" 2>/dev/null; then
        log "ERROR: Failed to extract rollback archive"
        return 1
    fi

    # Reset counter after successful rollback
    reset_counter
    
    log "Rollback complete - restored previous version from archive"
    return 0
}

# =============================================================================
# Main
# =============================================================================

main() {
    # Check if rollback archive exists
    if [[ ! -f "$ROLLBACK_ARCHIVE" ]]; then
        log "No rollback archive present - nothing to do"
        exit 0
    fi

    # Read and increment failure counter
    local fail_count
    fail_count=$(increment_counter)
    
    log "Failure count: $fail_count (threshold: $MAX_FAILURES)"

    # Check if we've exceeded the failure threshold
    if [[ "$fail_count" -ge "$MAX_FAILURES" ]]; then
        log "ALERT: Failure threshold exceeded - triggering automatic rollback"
        
        if perform_rollback; then
            log "Emergency rollback successful - system should recover on next start"
            exit 0
        else
            log "CRITICAL: Emergency rollback failed - manual intervention required"
            exit 1
        fi
    else
        exit 0
    fi
}

main "$@"
