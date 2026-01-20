#!/bin/bash
# gen_release_manifest.sh - Generate release manifest JSON for the update system
#
# Usage:
#   ./gen_release_manifest.sh <version> <channel> [output_file]
#
# Environment variables:
#   BASE_URL        - Base URL for binary downloads (default: https://updates.simtezilo.com)
#   OUT_DIR         - Directory containing built binaries (default: ./out)
#   MIN_VERSION     - Minimum version required to upgrade (optional)
#
# Example:
#   BUILDVERSION=1.2.3 ./gen_release_manifest.sh 1.2.3 stable releases/latest.json
#
# This script expects binaries to exist at:
#   ${OUT_DIR}/simtezilo-linux-arm64
#   ${OUT_DIR}/simtezilo-linux-amd64
#   ${OUT_DIR}/simtezilo-macos
#   ${OUT_DIR}/simtezilo.exe

set -euo pipefail

VERSION="${1:-}"
CHANNEL="${2:-stable}"
OUTPUT="${3:-releases/latest.json}"
BASE_URL="${BASE_URL:-https://updates.simtezilo.com}"
OUT_DIR="${OUT_DIR:-./out}"
MIN_VERSION="${MIN_VERSION:-}"
CHANGELOG="${CHANGELOG:-}"

if [[ -z "$VERSION" ]]; then
    echo "Usage: $0 <version> [channel] [output_file]"
    echo ""
    echo "Arguments:"
    echo "  version     - Release version (e.g., 1.2.3 or v1.2.3)"
    echo "  channel     - Release channel: stable, beta, dev (default: stable)"
    echo "  output_file - Output JSON file (default: releases/latest.json)"
    echo ""
    echo "Environment variables:"
    echo "  BASE_URL    - Base URL for downloads (default: https://updates.simtezilo.com)"
    echo "  OUT_DIR     - Directory with binaries (default: ./out)"
    echo "  MIN_VERSION - Minimum upgrade version (optional)"
    echo "  CHANGELOG   - Release changelog text (optional)"
    exit 1
fi

# Normalize version (ensure it starts with v for URL, but store without v)
VERSION_CLEAN="${VERSION#v}"
VERSION_TAG="v${VERSION_CLEAN}"

RELEASE_DATE=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

# Platform binary mappings (using parallel arrays for bash 3.x compatibility on macOS)
PLATFORM_KEYS="linux-arm64-6 linux-arm64-8 linux-arm64 linux-amd64 darwin-arm64 windows-amd64"
get_binary_name() {
    case "$1" in
        "linux-arm64")   echo "simtezilo-linux-arm64" ;;
        "linux-amd64")   echo "simtezilo-linux-amd64" ;;
        "darwin-arm64")  echo "simtezilo-macos" ;;
        "windows-amd64") echo "simtezilo.exe" ;;
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
    local binary_name="$3"
    
    if [[ ! -f "$file" ]]; then
        echo "null"
        return
    fi

    local url="${BASE_URL}/releases/${VERSION_TAG}/${binary_name}"
    local sha256=$(get_sha256 "$file")
    local size=$(get_file_size "$file")

    cat <<EOF
{
      "url": "${url}",
      "sha256": "${sha256}",
      "size": ${size}
    }
EOF
}

# Build platforms JSON
PLATFORMS_JSON=""
FIRST=true

for platform in $PLATFORM_KEYS; do
    binary_name=$(get_binary_name "$platform")
    file="${OUT_DIR}/${binary_name}"
    
    info=$(get_file_info "$file" "$platform" "$binary_name")
    
    if [[ "$info" != "null" ]]; then
        if [[ "$FIRST" != "true" ]]; then
            PLATFORMS_JSON+=","
        fi
        PLATFORMS_JSON+="
    \"${platform}\": ${info}"
        FIRST=false
        echo "✓ Found ${platform}: ${binary_name}"
    else
        echo "⚠ Missing ${platform}: ${binary_name}"
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

# Generate manifest
mkdir -p "$(dirname "$OUTPUT")"

cat > "$OUTPUT" <<EOF
{
  "version": "${VERSION_CLEAN}",
  "releaseDate": "${RELEASE_DATE}",
  "channel": "${CHANNEL}",${MIN_VERSION_JSON}${CHANGELOG_JSON}
  "platforms": {${PLATFORMS_JSON}
  }
}
EOF

echo ""
echo "Generated manifest: $OUTPUT"
echo "Version: ${VERSION_CLEAN}"
echo "Channel: ${CHANNEL}"
echo "Release Date: ${RELEASE_DATE}"

# Also generate individual checksum files
echo ""
echo "Generating checksum files..."
for platform in $PLATFORM_KEYS; do
    binary_name=$(get_binary_name "$platform")
    file="${OUT_DIR}/${binary_name}"
    
    if [[ -f "$file" ]]; then
        checksum_file="${file}.sha256"
        get_sha256 "$file" > "$checksum_file"
        echo "✓ ${checksum_file}"
    fi
done

echo ""
echo "Done!"
