#!/bin/sh

set -eux

appname='Simtezilo'
distdir='dist'
workdir="${distdir}/${appname}"
version=$(cat VERSION 2>/dev/null || echo "dev")

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
  "changelog": [
   "## Version ${version}",
   "",
   "### New Features",
   "- Added custom update upload support",
   "- Improved error handling",
   "",
   "### Bug Fixes"m
   "- Fixed telemetry connection issues",
   "- Resolved UI rendering glitches",
   "",
   "### Performance",
   "- Optimized haptics processing",
  ],
  "platform": "${platform}-${arch}"
}
EOF
}

function prep_common() {
   clean
   mkdir -p ./${workdir}/init \
            ./${workdir}/bin \
            ./${workdir}/etc \
            ./${workdir}/data/replays

   cp support/README.md ${workdir}/ 
   cp support/simtezilo-dist.conf ${workdir}/etc/simtezilo.conf
   cp data/replays/20251012.173043-suzuka-circuit-bmw-mclaren-f1-gtr-race-car-97.gtz ${workdir}/data/replays/demo.gtz
}

function gen_windows_amd64() {
   gen_manifest "windows" "amd64"

   cp out/simtezilo.exe ${workdir}/bin/simtezilo.exe

   distname=$(echo "${appname}-${version}-windows-amd64.zip" | tr '[A-Z]' '[a-z]')

   pushd ./${distdir} && zip -r ${distname} ${appname}
   popd

   rm ${workdir}/bin/simtezilo.exe ${workdir}/manifest.json
}

function gen_macos_silicon() {
   gen_manifest "darwin" "arm64"

   cp out/simtezilo-macos ${workdir}/bin/simtezilo 
   cp init/recover.sh ${workdir}/init/recover.sh

   distname=$(echo "${appname}-${version}-darwin-arm64.tar.gz" | tr '[A-Z]' '[a-z]')

   pushd ./${distdir} && tar -czvf ${distname} ${appname}
   popd

   rm ${workdir}/bin/simtezilo \
      ${workdir}/manifest.json \
      ${workdir}/init/recover.sh
}

function gen_linux_arm64() {
   gen_manifest "linux" "arm64"

   cp out/simtezilo-linux-arm64-8 ${workdir}/bin/simtezilo
   cp out/platform-m1-linux-arm64-8 ${workdir}/bin/platform
   cp init/simtezilo.service ${workdir}/init/simtezilo.service
   cp init/recover.sh ${workdir}/init/recover.sh

   distname=$(echo "${appname}-${version}-linux-arm64.tar.gz" | tr '[A-Z]' '[a-z]')

   pushd ./${distdir} && tar -czvf ${distname} ${appname}
   popd

   rm  ${workdir}/bin/simtezilo \
       ${workdir}/bin/platform \
       ${workdir}/manifest.json
}

prep_common
gen_windows_amd64
gen_macos_silicon
gen_linux_arm64
clean