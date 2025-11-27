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

function backupConfig() {
    cp -a ${bootConfig} ${bootConfig}.bak
    if [ $? -ne 0 ]; then
        echo "Failed to backup ${bootConfig}"
        exit 1
    fi

    cp -a /etc/systemd/journald.conf /etc/systemd/journald.conf.bak
    if [ $? -ne 0 ]; then
        echo "Failed to backup /etc/systemd/journald.conf"
        exit 1
    fi
}

function generalSetup() {
    backupConfig

    apt-get update && apt-get install -y log2ram

    echo "SystemMaxUse=50M" >> /etc/systemd/journald.conf
    echo "ForwardToSyslog=no" >> /etc/systemd/journald.conf
}

function enableSPI() {
    sed 's/#dtparam=spi=.*/dtparam=spi=on/' ${bootConfig}
    if [ $? -ne 0 ]; then
        echo "Failed to add dtparam=spi=on to ${bootConfig}"
        exit 1
    fi    
}

function disableHDMIAudio() {
    sed -i 's/dtparam=audio=on/dtparam=audio=off/' ${bootConfig}
    if [ $? -ne 0 ]; then
        echo "Failed to disable HDMI audio in ${bootConfig}"
        exit 1
    fi    
}

function addPirateAudioBasicSettings() {
    sed -i "s/[cm4]/\n# Pirate Audio\ngpio=13=op,dl\ndtoverlay=hifiberry-dac\n\n[cm4]/" ${bootConfig}
    if [ $? -ne 0 ]; then
        echo "Failed to add Pirate Audio settings to ${bootConfig}"
        exit 1
    fi
}

function addPirateAudioButtonConfig() {
    sed -i "s/dtoverlay=hifiberry-dac/dtoverlay=hifiberry-dac\ngpio=25=op,dh\ngpio=5=ip,dh\ngpio=6=ip,dh\ngpio=16=ip,dh\ngpio=24=ip,dh/" ${bootConfig}
    if [ $? -ne 0 ]; then
        echo "Failed to add Pirate Audio button config to ${bootConfig}"
        exit 1
    fi
}

case $hw in
    'none')
        generalSetup
        ;;
    'pirateaudio')
        generalSetup
        enableSPI
        disableHDMIAudio
        addPirateAudioSettings
        addPirateAudioButtonConfig
        ;;
    'waveshare')
        generalSetup
        enableSPI
        ;;
    *)
        echo "Unknown hardware: $hw"
        echo "Valid options are: none, pirateaudio, waveshare"
        exit 1
        ;;
esac
