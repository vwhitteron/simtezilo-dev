#!/bin/sh

set -eux

appname=Simtezilo
distdir=dist
appdir=${distdir}/${appname}

mkdir -p ./${appdir}

./build/scripts/gen_ver_file.sh

cp out/simtezilo* ${appdir}/ 

cp support/README.md ${appdir}/

cp assets/replay/trial-mountain-porsche-911-rsr-991-17.gtz ${appdir}/replay.gtz

mkdir -p ${appdir}/assets/html

cp assets/html/index.html ${appdir}/assets/html/
cp assets/html/scichart.js ${appdir}/assets/html/

version=$(awk -F '=' '/BUILDVERSION/{print $2}' ${appdir}/VERSION)

cd ./${distdir} && zip -r ${appname}-${version}.zip ${appname}