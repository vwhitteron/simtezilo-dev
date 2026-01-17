# Simtezilo Update System

This document describes how the Simtezilo update system works, including the update checking, downloading, staging, and installation processes.

## Architecture Overview

The update system consists of three main components:

1. **`app/updater` package** (Go):      Runs within the simtezilo application to check for updates, download them, and stage them for installation
2. **`platform update-apply`** (Go):    Runs at service startup via systemd to extract archives, swap binaries, and install updates
3. **`recover.sh` script** (Bash): Rescue/rollback safety net that runs after platform command to handle repeated failures

This three-phase approach ensures safe updates:
- The running application never replaces itself while running
- The platform command handles all system-specific installation logic
- The rescue script provides a bulletproof fallback if the Go code fails

## Update Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              UPDATE LIFECYCLE                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   ┌───────────────────┐    ┌───────────────────┐    ┌───────────────────┐   │
│   │       CHECK       │───▶│      DOWNLOAD     │───▶│       STAGE       │   │
│   │     (Checker)     │    │    (Downloader)   │    │    (Installer)    │   │
│   │   [simtezilo]     │    │   [simtezilo]     │    │   [simtezilo]     │   │
│   └───────────────────┘    └───────────────────┘    └───────────────────┘   │
│             │                                                 │             │
│             │                                                 ▼             │
│             │                                       ┌───────────────────┐   │
│             │                                       │ update-state.json │   │
│             │                                       └───────────────────┘   │
│             │                                                 │             │
│  ─ ─ ─ ─ ─ ─│─ ─ ─ ─ ─ ─ ─  SERVICE RESTART  ─ ─ ─ ─ ─ ─ ─ ─ ─│─ ─ ─ ─ ─ ─  │
│             │                                                 │             │
│             │                                                 ▼             │
│             │                                       ┌───────────────────┐   │
│             │                                       │ platform update-  │   │
│             │                                       │      apply        │   │
│             │                                       └───────────────────┘   │
│             │                                                 │             │
│             │                                                 ▼             │
│             │                                       ┌───────────────────┐   │
│             │                                       │    recover.sh     │   │
│             │                                       │  (rescue check)   │   │
│             │                                       └───────────────────┘   │
│             │                                                 │             │
│             ▼                                                 ▼             │
│   ┌───────────────────┐                             ┌───────────────────┐   │
│   │  CONFIRM SUCCESS  │◀────────────────────────────│  SERVICE STARTS   │   │
│   └───────────────────┘                             └───────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

### Simtezilo Application (app/updater package)

The main application handles **platform-agnostic** update operations:

| Component  | File            | Purpose                              |
|------------|-----------------|--------------------------------------|
| Updater    | `updater.go`    | Orchestrates the update workflow     |
| Checker    | `checker.go`    | Fetches manifests, compares versions |
| Downloader | `downloader.go` | Downloads and verifies archives      |
| Installer  | `installer.go`  | Manages state, stages updates        |

**Responsibilities:**
- Check for available updates
- Download update archives
- Verify checksums
- Write state file for platform command
- Request service restart
- Confirm successful update after restart

### Platform Command (`platform update-apply`)

The platform command handles **system-specific** installation:

**Responsibilities:**
- Read state file
- Verify checksums
- Extract archives (tar.gz, zip)
- Backup current binary to `.rollback`
- Install new binaries
- Install init scripts
- Update state file
- Handle complete/failed states

### Rescue Script (`recover.sh`)

A minimal bash script that provides **emergency rollback**:

**Responsibilities:**
- Detect repeated failures (3+ times)
- Perform emergency rollback to previous version
- Handle crashed platform command (stale "installing" state)
- Log to syslog for alerting

## Update States

The system tracks updates through these states in `update-state.json`:

| Status       | Description                                         |
|--------------|-----------------------------------------------------|
| `pending`    | Update downloaded and staged, waiting for restart   |
| `installing` | Installation in progress (set by platform command)  |
| `complete`   | Installation succeeded, awaiting confirmation       |
| `failed`     | Installation failed, may retry or rollback          |
| `rolled_back`| Rolled back to previous version after failures      |

### State File Format

```json
{
  "pendingVersion": "2.0.0",
  "currentVersion": "1.5.0",
  "downloadPath": "/opt/simtezilo/data/update/downloads/simtezilo-2.0.0-linux-arm64.tar.gz",
  "extractDir": "/opt/simtezilo/data/update/extract",
  "sha256": "abc123...",
  "timestamp": "2026-01-16T10:30:00Z",
  "status": "pending",
  "failCount": 0,
  "lastError": ""
}
```

## Phase 1: Application-Side (simtezilo)

When the application detects and downloads an update:

1. **Check**    - Fetches manifest from update server, compares versions
2. **Download** - Downloads the archive, verifies SHA256 checksum
3. **Prepare**  - Saves state file with `status: "pending"`
4. **Wait**     - Application continues running; update applied on next restart

```go
// Simplified flow in the updater package
info, _ := updater.CheckNow()                          // Check for updates
updater.Download(ctx, info, callback)                  // Download archive  
installer.Prepare(downloadPath, info, currentVersion)  // Stage for install
```

## Phase 2: Installation (platform update-apply)

The `platform update-apply` command runs as a systemd `ExecStartPre` hook.

### Archive Layout

The update archive must follow this structure:

```
Simtezilo/
├── bin/
│   ├── simtezilo          # Main binary (INSTALLED)
│   └── platform           # Helper binary (INSTALLED)
├── init/
│   ├── recover.sh    # Rescue script (INSTALLED)
│   └── simtezilo.service  # Systemd unit (INSTALLED)
├── etc/
│   └── simtezilo.conf     # Config file (NOT overwritten)
└── data/
    └── replays/           # User data (NOT overwritten)
```

### Installation Process

1. **Load state**              - Read update-state.json
2. **Verify download**         - Check file exists and checksum matches
3. **Mark installing**         - Set status to "installing"
4. **Extract archive**         - Supports tar.gz, zip, or raw binary
5. **Install init scripts**    - Update recover.sh and service files
6. **Backup current binary**   - Create `.rollback` file for recovery
7. **Install new binaries**    - Copy from extracted archive
8. **Cleanup**                 - Remove extracted files and archive
9. **Mark complete**           - Set status to "complete"

## Phase 3: Rescue Check (recover.sh)

After the platform command runs, the rescue script performs safety checks:

```bash
# Check for repeated failures
if [[ "$status" == "failed" && "$fail_count" -ge 3 ]]; then
    perform_rollback
fi

# Check for crashed platform command
if [[ "$status" == "installing" ]]; then
    # Platform command crashed mid-install
    mark_failed "platform command crashed during install"
fi
```

### Rollback Mechanism

If the application fails to start 3 times after an update:

1. Rescue script detects `status: "failed"` with `failCount >= 3`
2. Current binary moved to `.failed`
3. Rollback binary (`.rollback`) restored as current
4. State updated to `rolled_back`

### Success Confirmation

After successful startup, the application calls `installer.ConfirmSuccess()`:

1. Removes `.rollback` binary (no longer needed)
2. Clears the state file
3. Update cycle complete

## Configuration

### Environment Variables (recover.sh)

| Variable      | Default                      | Description                   |
|---------------|------------------------------|-------------------------------|
| `INSTALL_DIR` | `/opt/simtezilo/bin`         | Binary installation directory |
| `DATA_DIR`    | `/opt/simtezilo/data/update` | State and download directory  |
| `BINARY_NAME` | `simtezilo`                  | Name of the main binary       |

### Platform Command Constants

| Constant           | Value                          | Description                   |
|--------------------|--------------------------------|-------------------------------|
| `updateInstallDir` | `/opt/simtezilo/bin`           | Binary installation directory |
| `updateInitDir`    | `/opt/simtezilo/init`          | Init scripts directory        |
| `updateDataDir`    | `/opt/simtezilo/data/update`   | State and download directory  |
| `updateBinaryName` | `simtezilo`                    | Name of the main binary       |

### Application Config

```go
updater.Config{
    Enabled:         true,
    BaseURL:         "https://updates.example.com",
    Channel:         "stable",
    CheckInterval:   1 * time.Hour,
    AutoInstall:     false,
    InstallDir:      "/opt/simtezilo/bin",
    InitDir:         "/opt/simtezilo/init",
    DataDir:         "/opt/simtezilo/data/update",
    BinaryName:      "simtezilo",
    ServiceName:     "simtezilo",
    UseSystemd:      true,
}
```

## Systemd Integration

The service unit should include both the platform command and rescue script:

```ini
[Service]
ExecStartPre=/opt/simtezilo/bin/platform update-apply
ExecStartPre=/opt/simtezilo/init/recover.sh
ExecStart=/opt/simtezilo/bin/simtezilo
Restart=on-failure
RestartSec=5
```

This ensures:
1. Platform command attempts to apply any pending update
2. Rescue script checks for failures and performs rollback if needed
3. Service starts with the (potentially new) binary
4. Restart policy allows for automatic rollback detection

---

# Custom Update Upload with Metadata

## Overview

When uploading custom update files via the web UI, you can include metadata to provide proper version information, changelog, and release date. This metadata will be displayed in the update panel just like manifest-based updates.

## Metadata File Format

Include a file named `manifest.json` in the root of your archive (zip or tar.gz) with the following structure:

```json
{
  "version": "1.2.3",
  "releaseDate": "2026-01-11T10:30:00Z",
  "changelog": [
    "## Version 1.2.3",
    "",
    "### New Features",
    "- Feature 1",
    "- Feature 2",
    "",
    "### Bug Fixes",
    "- Fix 1",
    "- Fix 2"
  ],
  "platform": "darwin-arm64"
}
```

### Fields

- **version** (required):     The version string (e.g., "1.2.3")
- **releaseDate** (required): ISO 8601 formatted date/time
- **changelog** (required):   Array of strings, each element is a line of the changelog (supports Markdown)
- **platform** (optional):    Target platform identifier

## Creating an Archive with Metadata

### For tar.gz archives:

```bash
# Create manifest.json
cat > manifest.json << 'EOF'
{
  "version": "1.2.3",
  "releaseDate": "2026-01-11T10:30:00Z",
  "changelog": [
    "## What's New",
    "",
    "- Improved performance",
    "- Fixed bugs"
  ],
  "platform": "darwin-arm64"
}
EOF

# Create archive with metadata and binary
tar czf simtezilo-1.2.3-darwin-arm64.tar.gz manifest.json simtezilo

# Clean up
rm manifest.json
```

### For zip archives:

```bash
# Create manifest.json (same as above)
cat > manifest.json << 'EOF'
{
  "version": "1.2.3",
  "releaseDate": "2026-01-11T10:30:00Z",
  "changelog": [
    "## What's New",
    "",
    "- Improved performance",
    "- Fixed bugs"
  ],
  "platform": "darwin-arm64"
}
EOF

# Create archive with metadata and binary
zip simtezilo-1.2.3-darwin-arm64.zip manifest.json simtezilo

# Clean up
rm manifest.json
```

## Upload Behavior

1. **With Metadata**: If `manifest.json` is found in the archive:
   - Version, changelog, and release date are extracted and displayed
   - The update panel shows the extracted information
   - File is saved with "custom-" prefix

2. **Without Metadata**: If no metadata file is found:
   - Falls back to synthetic version: "custom-{filename}"
   - Changelog shows: "Custom uploaded file: {filename}"
   - Release date is set to current time
   - File is saved with "custom-" prefix

## File Naming

Uploaded files are automatically prefixed with `custom-` to distinguish them from manifest-downloaded updates:
- Original: `simtezilo-1.2.3-darwin-arm64.tar.gz`
- Saved as: `custom-simtezilo-1.2.3-darwin-arm64.tar.gz`

## Cleanup

When uploading a new custom file, all previous downloads (including other custom uploads) are automatically cleaned up from the downloads directory.

## Example

See `doc/manifest.example.json` for a complete example of the metadata file.
