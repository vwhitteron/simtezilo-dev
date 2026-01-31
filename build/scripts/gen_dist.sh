#!/bin/bash
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

set -euo pipefail

# Load version utilities
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/version.sh"

appname='Simtezilo'

# Directories
out_dir="${OUT_DIR:-./out}"
release_dir="dist/releases/${VERSION_CHANNEL}/${VERSION_TAG}"
workdir="dist/.work/${appname}"

# Cleanup working directory
clean() {
   rm -rf ./dist/.work
}

# Generate per-archive manifest
gen_manifest() {
   local platform=$1
   local arch=$2
   local release_date
   release_date=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

   cat <<EOF > "${workdir}/manifest.json"
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
    if command -v sha256sum &> /dev/null; then
        sha256sum "$file" | cut -d' ' -f1
    elif command -v shasum &> /dev/null; then
        shasum -a 256 "$file" | cut -d' ' -f1
    else
        echo "Error: No sha256 command found" >&2
        exit 1
    fi
}

# Prepare common directory structure
prep_common() {
   clean
   mkdir -p "./${workdir}/init" \
            "./${workdir}/bin" \
            "./${workdir}/etc" \
            "./${workdir}/data/replays" \
            "./${release_dir}"

   cp support/README.md "${workdir}/"
   cp support/simtezilo-dist.conf "${workdir}/etc/simtezilo.conf"
   cp data/replays/20251012.173043-suzuka-circuit-bmw-mclaren-f1-gtr-race-car-97.gtz "${workdir}/data/replays/demo.gtz"
}

gen_windows_amd64() {
   local platform="windows-amd64"
   local distname="simtezilo-${VERSION_TAG}-windows-amd64.zip"
   local distpath="${release_dir}/${distname}"

   gen_manifest "windows" "amd64"
   cp "${out_dir}/simtezilo.exe" "${workdir}/bin/simtezilo.exe"

   pushd ./dist/.work > /dev/null
   zip -rq "../releases/${VERSION_CHANNEL}/${VERSION_TAG}/${distname}" "${appname}"
   popd > /dev/null

   # Generate checksum
   get_sha256 "${distpath}" > "${distpath}.sha256"

   rm "${workdir}/bin/simtezilo.exe" "${workdir}/manifest.json"
   echo "✓ Created ${platform}: ${distname}"
}

gen_macos_silicon() {
   local platform="darwin-arm64"
   local distname="simtezilo-${VERSION_TAG}-darwin-arm64.tar.gz"
   local distpath="${release_dir}/${distname}"

   gen_manifest "darwin" "arm64"
   cp "${out_dir}/simtezilo-macos" "${workdir}/bin/simtezilo"
   cp init/recover.sh "${workdir}/init/recover.sh"

   tar -czf "${distpath}" -C ./dist/.work "${appname}"

   # Generate checksum
   get_sha256 "${distpath}" > "${distpath}.sha256"

   rm "${workdir}/bin/simtezilo" \
      "${workdir}/manifest.json" \
      "${workdir}/init/recover.sh"
   echo "✓ Created ${platform}: ${distname}"
}

gen_linux_arm64() {
   local platform="linux-arm64"
   local distname="simtezilo-${VERSION_TAG}-linux-arm64.tar.gz"
   local distpath="${release_dir}/${distname}"

   gen_manifest "linux" "arm64"
   cp "${out_dir}/simtezilo-linux-arm64-8" "${workdir}/bin/simtezilo"
   cp "${out_dir}/platform-m1-linux-arm64-8" "${workdir}/bin/platform"
   cp init/simtezilo.service "${workdir}/init/simtezilo.service"
   cp init/recover.sh "${workdir}/init/recover.sh"

   tar -czf "${distpath}" -C ./dist/.work "${appname}"

   # Generate checksum
   get_sha256 "${distpath}" > "${distpath}.sha256"

   rm "${workdir}/bin/simtezilo" \
      "${workdir}/bin/platform" \
      "${workdir}/manifest.json"
   echo "✓ Created ${platform}: ${distname}"
}

gen_linux_amd64() {
   local platform="linux-amd64"
   local distname="simtezilo-${VERSION_TAG}-linux-amd64.tar.gz"
   local distpath="${release_dir}/${distname}"

   # Skip if binary doesn't exist
   if [[ ! -f "${out_dir}/simtezilo-linux-amd64" ]]; then
      echo "⚠ Skipped ${platform}: binary not found"
      return
   fi

   gen_manifest "linux" "amd64"
   cp "${out_dir}/simtezilo-linux-amd64" "${workdir}/bin/simtezilo"
   cp init/simtezilo.service "${workdir}/init/simtezilo.service"
   cp init/recover.sh "${workdir}/init/recover.sh"

   tar -czf "${distpath}" -C ./dist/.work "${appname}"

   # Generate checksum
   get_sha256 "${distpath}" > "${distpath}.sha256"

   rm "${workdir}/bin/simtezilo" \
      "${workdir}/manifest.json"
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