#!/bin/bash

##### Configuration #####
SETUPMODE_FILE="/boot/firmware/simtezilo/SETUPMODE"
WLANDEV="wlan0"
CONNNAME="SetupMode"
SSIDPREFIX="Simtezilo-"
WPAPSK="5imtezil0"
IPV4ADDR="10.33.0.1/24"
#########################

NMCLI="/usr/bin/nmcli"

OPTIONS="$@"

apConnExists() {
    ${NMCLI} con show | grep -q "^${CONNNAME}\s"
}

setupAPConn() {
    serial=$(awk '/^Serial/ {sub(/^0*/, "", $NF); print $NF}' /proc/cpuinfo)
    ssid="${SSIDPREFIX}${serial}"

    ${NMCLI} con add type wifi ifname ${WLANDEV} con-name ${CONNNAME} autoconnect yes ssid ${ssid}
    ${NMCLI} con modify ${CONNNAME} ipv4.address ${IPV4ADDR}
    ${NMCLI} con modify ${CONNNAME} 802-11-wireless.mode ap 802-11-wireless.band bg ipv4.method shared
    ${NMCLI} con modify ${CONNNAME} wifi-sec.key-mgmt wpa-psk
    ${NMCLI} con modify ${CONNNAME} wifi-sec.psk "${WPAPSK}"
    ${NMCLI} con modify ${CONNNAME} wifi-sec.proto rsn
    ${NMCLI} con modify ${CONNNAME} wifi-sec.group ccmp
    ${NMCLI} con modify ${CONNNAME} wifi-sec.pairwise ccmp
    # ${NMCLI} con modify ${CONNNAME} 802-11-wireless.security.pmf 1
}

startAPConn() {
    ${NMCLI} con up ${CONNNAME}
    }

enableSetupModeFlag() {
    touch "$SETUPMODE_FILE"
}

setup() {
    if ! apConnExists; then
        echo "SetupMode access point connection not found. Creating it."

        setupAPConn

        startAPConn

        enableSetupModeFlag
    fi
}



if [ -f "$SETUPMODE_FILE" ]; then
    echo "Setup mode detected. Staring AP mode and launching setup wizard"
    
    /usr/bin/nmcli con up SetupMode

    if [ $? -eq 10 ]; then
        echo "Creating SetupMode access point connection."

        setup

        if [ $? -ne 0 ]; then
            echo "SetupMode access point connection failed with error $?"

            exit 1
        fi

        /usr/bin/nmcli con up SetupMode
    elif [ $? -ne 0 ]; then
        echo "Starting SetupMode access point failed with error $?"
    fi

    /opt/simtezilo/bin/setupwizard

    if [ $? -ne 0 ]; then
        echo "Setup wizard exited with error $?"

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
        echo "Simtezilo exited with error $?"

        exit $?
    fi

    echo "Simtezilo exited normally"
fi
