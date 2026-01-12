#!/bin/sh

set -eux

appname='Simtezilo'
distdir='dist'
workdir="${distdir}/${appname}"
version=$(cat ../../VERSION 2>/dev/null || echo "dev")

function clean() {
   rm -rf ./${workdir}
}

function gen_manifest() {
   platform=$1
   arch=$2

   cat <<EOF > ${workdir}/manifest.json
{
  "version": "${version}",
  "releaseDate": "2026-01-11T10:30:00Z",
  "changelog": "## Version ${version}\n\n### New Features\n- Added custom update upload support\n- Improved error handling\n\n### Bug Fixes\n- Fixed telemetry connection issues\n- Resolved UI rendering glitches\n\n### Performance\n- Optimized haptics processing",
  "platform": "${platform}-${arch}"
}
EOF
}

function prep_common() {
   clean
   mkdir -p ./${workdir}

   cp support/README.md ${workdir}/ 
   cp support/simtezilo-dist.conf ${workdir}/simtezilo.conf
   cp data/replays/20251012.173043-suzuka-circuit-bmw-mclaren-f1-gtr-race-car-97.gtz ${workdir}/demo.gtz
}

function gen_windows_amd64() {
   gen_manifest "windows" "amd64"

   cp out/simtezilo.exe ${workdir}/simtezilo.exe

   distname=$(echo "${appname}-${version}-windows-amd64.zip" | tr '[A-Z]' '[a-z]')

   pushd ./${distdir} && zip -r ${distname} ${appname}
   popd

   rm ${workdir}/simtezilo.exe ${workdir}/manifest.json
}

function gen_macos_silicon() {
   gen_manifest "darwin" "arm64"

   cp out/simtezilo-macos \
      ${workdir}/simtezilo 

   distname=$(echo "${appname}-${version}-darwin-arm64.tar.gz" | tr '[A-Z]' '[a-z]')

   pushd ./${distdir} && tar -czvf ${distname} ${appname}
   popd

   rm ${workdir}/simtezilo \
      ${workdir}/manifest.json
}

function gen_linux_arm64() {
   gen_manifest "linux" "arm64"

   cp out/simtezilo-linux-arm64-8 ${workdir}/simtezilo
   cp out/platform-m1-linux-arm64-8 ${workdir}/platform

   distname=$(echo "${appname}-${version}-linux-arm64.tar.gz" | tr '[A-Z]' '[a-z]')

   pushd ./${distdir} && tar -czvf ${distname} ${appname}
   popd

   rm  ${workdir}/simtezilo \
       ${workdir}/platform \
       ${workdir}/manifest.json
}

prep_common
gen_windows_amd64
gen_macos_silicon
gen_linux_arm64
clean