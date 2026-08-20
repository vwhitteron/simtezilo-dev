#!/bin/sh
# gen_dist.sh - Generate distribution archives for all platforms
#
# Usage:
#   ./gen_dist.sh
#
# The channel is automatically derived from the version string:
#   v1.0.0           -> stable
#   v1.0.0-beta.1    -> beta
#   v1.0.0-rc.1      -> beta
#   v1.0.0-alpha.1   -> dev
#   v1.0.0-dev.1     -> dev
#
# Environment variables:
#   OUT_DIR   - Directory containing built binaries (default: ./out)
#
# This script creates distribution archives and places them in:
#   dist/releases/<channel>/v<version>/
#
# The output directory structure is ready for direct upload to a web server.

set -eu


# Load version utilities
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "${SCRIPT_DIR}/lib/version.sh"

appname='simtezilo'

# Directories
out_dir="${OUT_DIR:-./out}"
dist_dir="${dist_dir:-./dist}"
release_dir="${dist_dir}/releases/${VERSION_CHANNEL}/${VERSION_TAG}"
work_dir="${dist_dir}/.work"
archive_dir="${work_dir}/${appname}"

# Cleanup working directory
clean() {
   rm -rf "${work_dir}"
}

# Generate per-archive manifest
gen_manifest() {
   local platform=$1
   local arch=$2
   local release_date
   release_date=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

   cat <<EOF > "${archive_dir}/manifest.json"
{
  "version": "${VERSION}",
  "releaseDate": "${release_date}",
  "channel": "${VERSION_CHANNEL}",
  "platform": "${platform}-${arch}"
}
EOF
}

# Compute SHA256 hash
get_sha256() {
    local file="$1"
    local sum
    # Capture first, then split. Under set -e a failure of the hashing command
    # aborts here, which is what pipefail used to catch in the pipeline.
    if command -v sha256sum > /dev/null 2>&1; then
        sum=$(sha256sum "$file")
    elif command -v shasum > /dev/null 2>&1; then
        sum=$(shasum -a 256 "$file")
    else
        echo "Error: No sha256 command found" >&2
        exit 1
    fi

    printf '%s\n' "$sum" | cut -d' ' -f1
}

# Prepare common directory structure
prep_common() {
   clean
   mkdir -p "./${archive_dir}/init" \
            "./${archive_dir}/bin" \
            "./${archive_dir}/etc" \
            "./${archive_dir}/data/replays" \
            "./${release_dir}"

   cp README.md "${archive_dir}/"
   cp support/simtezilo.conf "${archive_dir}/etc/simtezilo.conf"
   cp data/replays/demo.gtz "${archive_dir}/data/replays/demo.gtz"
}

gen_windows_amd64() {
   local platform="windows-amd64"
   local distname="simtezilo-${VERSION_TAG}-windows-amd64.zip"
   local absdistpath="$(pwd)/${release_dir}/${distname}"

   gen_manifest "windows" "amd64"
   cp "${out_dir}/simtezilo.exe" "${archive_dir}/bin/simtezilo.exe"

   (cd "${work_dir}" && zip -rq "${absdistpath}" "${appname}")

   # Generate checksum
   get_sha256 "${absdistpath}" > "${absdistpath}.sha256"

   rm "${archive_dir}/bin/simtezilo.exe" "${archive_dir}/manifest.json"
   echo "✓ Created ${platform}: ${distname}"
}

gen_macos_silicon() {
   local platform="darwin-arm64"
   local distname="simtezilo-${VERSION_TAG}-darwin-arm64.tar.gz"
   local absdistpath="${release_dir}/${distname}"

   gen_manifest "darwin" "arm64"
   cp "${out_dir}/simtezilo-macos" "${archive_dir}/bin/simtezilo"
   cp init/recover.sh "${archive_dir}/init/recover.sh"

   tar -czf "${absdistpath}" -C "${work_dir}" "${appname}"

   # Generate checksum
   get_sha256 "${absdistpath}" > "${absdistpath}.sha256"

   rm "${archive_dir}/bin/simtezilo" \
      "${archive_dir}/manifest.json" \
      "${archive_dir}/init/recover.sh"
   echo "✓ Created ${platform}: ${distname}"
}

gen_linux_arm64() {
   local platform="linux-arm64"
   local distname="simtezilo-${VERSION_TAG}-linux-arm64.tar.gz"
   local absdistpath="${release_dir}/${distname}"

   gen_manifest "linux" "arm64"
   cp "${out_dir}/simtezilo-linux-arm64-8" "${archive_dir}/bin/simtezilo"
   cp "${out_dir}/platform-m1-linux-arm64-8" "${archive_dir}/bin/platform"
   cp init/simtezilo.service "${archive_dir}/init/simtezilo.service"
   cp init/recover.sh "${archive_dir}/init/recover.sh"
   cp init/rt-tune.sh "${archive_dir}/init/rt-tune.sh"

   tar -czf "${absdistpath}" -C "${work_dir}" "${appname}"

   # Generate checksum
   get_sha256 "${absdistpath}" > "${absdistpath}.sha256"

   rm "${archive_dir}/bin/simtezilo" \
      "${archive_dir}/bin/platform" \
      "${archive_dir}/manifest.json"
   echo "✓ Created ${platform}: ${distname}"
}

gen_linux_amd64() {
   local platform="linux-amd64"
   local distname="simtezilo-${VERSION_TAG}-linux-amd64.tar.gz"
   local absdistpath="${release_dir}/${distname}"

   # Skip if binary doesn't exist
   if [ ! -f "${out_dir}/simtezilo-linux-amd64" ]; then
      echo "⚠ Skipped ${platform}: binary not found"
      return
   fi

   gen_manifest "linux" "amd64"
   cp "${out_dir}/simtezilo-linux-amd64" "${archive_dir}/bin/simtezilo"
   cp init/simtezilo.service "${archive_dir}/init/simtezilo.service"
   cp init/recover.sh "${archive_dir}/init/recover.sh"
   cp init/rt-tune.sh "${archive_dir}/init/rt-tune.sh"

   tar -czf "${absdistpath}" -C "${work_dir}" "${appname}"

   # Generate checksum
   get_sha256 "${absdistpath}" > "${absdistpath}.sha256"

   rm "${archive_dir}/bin/simtezilo" \
      "${archive_dir}/manifest.json"
   echo "✓ Created ${platform}: ${distname}"
}

echo "Generating distribution archives..."
echo "Version: ${VERSION}"
echo "Channel: ${VERSION_CHANNEL}"
echo "Output:  ${release_dir}/"
echo ""

prep_common
gen_windows_amd64
gen_macos_silicon
gen_linux_arm64
gen_linux_amd64
clean

echo ""
echo "Distribution archives ready in: ${release_dir}/"