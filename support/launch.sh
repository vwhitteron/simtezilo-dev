#!/bin/bash

set -e

OPTIONS="$@"

if [ -f /boot/firmware/simtezilo/SETUPMODE ]; then
    echo "Setup mode detected. Launching setup wizard"
    
    /opt/simtezilo/bin/setupwizard
else
    echo "Run mode detected. Launching Simtezilo"

    /opt/simtezilo/bin/simtezilo $OPTIONS
fi