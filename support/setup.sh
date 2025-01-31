#!/bin/sh

if [ $(whoami) != 'root' ]; then
    echo "This script must be run as root"
    exit 1
fi

if [ -z "$1" ]; then
    echo "Usage: $0 <hw>"
    exit 1
fi

hw=$1

bootConfig=''
if [ -e '/boot/firmware/config.txt' ]; then
    bootConfig='/boot/firmware/config.txt'
elif [ -e '/boot/config.txt' ]; then
    bootConfig='/boot/config.txt'
else
    echo "boot config.txt file not found"
    exit 1
fi

cp -a ${bootConfig} ${bootConfig}.bak

function enableSPI() {
    sed 's/#dtparam=spi=.*/dtparam=spi=on/' ${bootConfig}
    if [ $? -ne 0 ]; then
        echo "Failed to add dtparam=spi=on to ${bootConfig}"
        exit 1
    fi    
}

case $hw in
    'none')
        ;;
    'pirateaudio')
        enableSPI
        # Pirate Audio DAC
        #sed -i 's/dtparam=audio=on/dtparam=audio=on\ndtoverlay=hifiberry-dac\ngpio=25=op,dh/' ${bootConfig}
        ;;
    'waveshare')
        enableSPI
        ;;
    *)
        echo "Unknown hardware: $hw"
        exit 1
        ;;
esac
