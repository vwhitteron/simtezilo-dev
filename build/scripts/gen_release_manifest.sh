#!/bin/bash
# gen_release_manifest.sh - Generate release manifest JSON for the update system
#
# Usage:
#   ./gen_release_manifest.sh
#
# The channel is automatically derived from the version string:
#   v1.0.0           -> stable
#   v1.0.0-beta.1    -> beta
#   v1.0.0-rc.1      -> beta
#   v1.0.0-alpha.1   -> dev
#   v1.0.0-dev.1     -> dev
#
# Environment variables:
#   BASE_URL        - Base URL for binary downloads (default: https://updates.simtezilo.com)
#   DIST_DIR        - Directory containing distribution archives (default: ./dist/releases)
#   MIN_VERSION     - Minimum version required to upgrade (optional)
#   CHANGELOG       - Release changelog text (optional)
#
# Example:
#   ./gen_release_manifest.sh
#
# This script expects distribution archives to exist at:
#   ${DIST_DIR}/<channel>/v<version>/simtezilo-v<version>-<platform>.tar.gz|.zip

set -euo pipefail

# Load version utilities
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/version.sh"

BASE_URL="${BASE_URL:-https://updates.simtezilo.com}"
DIST_DIR="${DIST_DIR:-./dist/releases}"
MIN_VERSION="${MIN_VERSION:-}"
CHANGELOG="${CHANGELOG:-}"

# Archive directory for this release
ARCHIVE_DIR="${DIST_DIR}/${VERSION_CHANNEL}/${VERSION_TAG}"
OUTPUT="${ARCHIVE_DIR}/latest.json"

RELEASE_DATE=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

# Platform archive mappings
get_archive_name() {
    case "$1" in
        "linux-arm64")   echo "simtezilo-${VERSION_TAG}-linux-arm64.tar.gz" ;;
        "linux-amd64")   echo "simtezilo-${VERSION_TAG}-linux-amd64.tar.gz" ;;
        "darwin-arm64")  echo "simtezilo-${VERSION_TAG}-darwin-arm64.tar.gz" ;;
        "windows-amd64") echo "simtezilo-${VERSION_TAG}-windows-amd64.zip" ;;
    esac
}

# Function to compute SHA256 hash of a file
get_sha256() {
    local file="$1"
    if command -v sha256sum &> /dev/null; then
        sha256sum "$file" | cut -d' ' -f1
    elif command -v shasum &> /dev/null; then
        shasum -a 256 "$file" | cut -d' ' -f1
    else
        echo "Error: No sha256 command found" >&2
        exit 1
    fi
}

get_file_size() {
    local file="$1"

    if [[ "$(uname)" == "Darwin" ]]; then
        stat -f%z "$file"
    else
        stat -c%s "$file"
    fi
}

# Function to get file info
get_file_info() {
    local file="$1"
    local platform="$2"
    local archive_name="$3"
    
    if [[ ! -f "$file" ]]; then
        echo "null"
        return
    fi

    local url="${BASE_URL}/releases/${VERSION_CHANNEL}/${VERSION_TAG}/${archive_name}"
    local sha256
    sha256=$(get_sha256 "$file")
    local size
    size=$(get_file_size "$file")

    cat <<EOF
{
      "url": "${url}",
      "sha256": "${sha256}",
      "size": ${size}
    }
EOF
}

# Check archive directory exists
if [[ ! -d "$ARCHIVE_DIR" ]]; then
    echo "Error: Archive directory not found: ${ARCHIVE_DIR}"
    echo "Run 'make dist' first to generate distribution archives."
    exit 1
fi

# Platform keys
PLATFORM_KEYS="linux-arm64 linux-amd64 darwin-arm64 windows-amd64"

# Build platforms JSON
PLATFORMS_JSON=""
FIRST=true

for platform in $PLATFORM_KEYS; do
    archive_name=$(get_archive_name "$platform")
    file="${ARCHIVE_DIR}/${archive_name}"
    
    info=$(get_file_info "$file" "$platform" "$archive_name")
    
    if [[ "$info" != "null" ]]; then
        if [[ "$FIRST" != "true" ]]; then
            PLATFORMS_JSON+=","
        fi
        PLATFORMS_JSON+="
    \"${platform}\": ${info}"
        FIRST=false
        echo "✓ Found ${platform}: ${archive_name}"
    else
        echo "⚠ Missing ${platform}: ${archive_name}"
    fi
done

# Build minimum version field
MIN_VERSION_JSON=""
if [[ -n "$MIN_VERSION" ]]; then
    MIN_VERSION_JSON="
  \"minUpgradeVersion\": \"${MIN_VERSION}\","
fi

# Build changelog field
CHANGELOG_JSON=""
if [[ -n "$CHANGELOG" ]]; then
    # Escape newlines and quotes for JSON
    CHANGELOG_ESCAPED=$(echo "$CHANGELOG" | sed 's/\\/\\\\/g' | sed 's/"/\\"/g' | sed ':a;N;$!ba;s/\n/\\n/g')
    CHANGELOG_JSON="
  \"changelog\": \"${CHANGELOG_ESCAPED}\","
fi

# Generate manifest in the archive directory
cat > "$OUTPUT" <<EOF
{
  "version": "${VERSION}",
  "releaseDate": "${RELEASE_DATE}",
  "channel": "${VERSION_CHANNEL}",${MIN_VERSION_JSON}${CHANGELOG_JSON}
  "platforms": {${PLATFORMS_JSON}
  }
}
EOF

echo ""
echo "Generated manifest: $OUTPUT"
echo "Version: ${VERSION}"
echo "Channel: ${VERSION_CHANNEL}"
echo "Release Date: ${RELEASE_DATE}"

# Copy manifest to channel root as latest.json for update checks
CHANNEL_LATEST="${DIST_DIR}/${VERSION_CHANNEL}/latest.json"
cp "$OUTPUT" "$CHANNEL_LATEST"
echo ""
echo "Copied to channel root: ${CHANNEL_LATEST}"

echo ""
echo "Ready to publish: scp -r ${DIST_DIR}/${VERSION_CHANNEL}/ <server>:/var/www/updates/releases/"
echo ""
echo "Done!"
