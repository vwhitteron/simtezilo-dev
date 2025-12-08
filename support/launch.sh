#!/bin/bash

set -e

SETUPMODE_FILE="/boot/firmware/simtezilo/SETUPMODE"

OPTIONS="$@"

if [ -f "$SETUPMODE_FILE" ]; then
    echo "Setup mode detected. Launching  setup wizard"
    
    /opt/simtezilo/bin/setupwizard

    if [ $? -ne 0 ]; then
        echo "Setup wizard exited with an error"

        exit $?
    fi

    echo "Setup wizard completed successfully, disabling setup mode"

    rm -f "$SETUPMODE_FILE"

    exit 0
else
    echo "Run mode detected. Launching Simtezilo"

    /opt/simtezilo/bin/simtezilo $OPTIONS

    if [ $? -eq 33 ]; then
        echo "Simtezilo exited into setup mode."

        touch "$SETUPMODE_FILE"

        exit 0
    elif [ $? -ne 0 ]; then
        echo "Simtezilo exited with error code $?"

        exit $?
    fi

    echo "Simtezilo exited normally"
fi