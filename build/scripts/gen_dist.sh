#!/bin/sh

set -eux

appname=Simtezilo
distdir=dist
appdir=${distdir}/${appname}

rm -rf ./${appdir}

mkdir -p ./${appdir}

./build/scripts/gen_ver_file.sh

version=$(awk -F '=' '/BUILDVERSION/{print $2}' ${appdir}/VERSION)

cp out/simtezilo-linux-arm64-8 \
   out/simtezilo-macos \
   out/simtezilo.exe \
   support/README.md \
   ${appdir}/ 

cp data/replays/trial-mountain-porsche-911-rsr-991-17.gtz ${appdir}/replay.gtz

zipname=$(echo "${appname}-${version}.zip" | tr '[A-Z]' '[a-z]')

cd ./${distdir} && zip -r ${zipname} ${appname}