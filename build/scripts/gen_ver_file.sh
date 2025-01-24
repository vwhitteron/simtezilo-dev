#!/bin/sh

buildversion=$(git describe --tags --always --dirty)
buildtime=$(date -u '+%Y-%m-%d_%H:%M:%S')

cat <<EOD >> dist/Simtezilo/VERSION
BUILDVERSION=${buildversion}
BUILDTIME=${buildtime}
EOD