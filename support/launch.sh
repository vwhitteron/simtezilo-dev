#!/bin/bash

##### Configuration #####
SETUPMODE_FILE="/boot/firmware/simtezilo/SETUPMODE"
WLANDEV="wlan0"
APCONNNAME="SetupMode"
RUNCONNNAME="RunMode"
SSIDPREFIX="Simtezilo-"
WPAPSK="5imtezil0"
IPV4ADDR="10.33.0.1/24"
#########################

NMCLI="/usr/bin/nmcli"
SYSTEMCTL="/usr/bin/systemctl"
SIMTEZILO="/opt/simtezilo/bin/simtezilo"

OPTIONS="$@"

initSetupMode() {
    if ! setupModeExists; then
        echo "Creating SetupMode access point connection."

        setupAPConn
        startAPConn

        enableSetupModeFlag
    fi
}

connExists() {
    name="$1"
    
    if networkManagerRunning; then
        ${NMCLI} con show | grep -q "^${name}\s"
        return $?
    fi

    test -f "/etc/NetworkManager/system-connections/${name}.nmconnection"
}

setupModeExists() {
    connExists "${APCONNNAME}"
}

runModeExists() {
    connExists "${RUNCONNNAME}"
}

networkManagerRunning() {
    ${SYSTEMCTL} is-active --quiet NetworkManager
}

waitForNetworkManager() {
    if networkManagerRunning; then
        return
    fi
    
    echo "Waiting for NetworkManager to start."

    until networkManagerRunning; do    
        sleep 2
    done

    echo "NetworkManager is running."
}

connUp() {
    name="$1"

    waitForNetworkManager

    echo "Starting ${name} wifi connection."

    ${NMCLI} con up "${name}"
    exitCode=$?

    if [ $exitCode -eq 10 ]; then
        initSetupMode
    elif [ $exitCode -ne 0 ]; then
        echo "Failed to start ${name} wifi connection with error $?"
    fi
}

setupAPConn() {
    waitForNetworkManager

    echo "Setting up SetupMode access point connection."

    serial=$(awk '/^Serial/ {sub(/^0*/, "", $NF); print $NF}' /proc/cpuinfo)
    ssid="${SSIDPREFIX}${serial}"

    ${NMCLI} con add type wifi ifname "${WLANDEV}" con-name "${APCONNNAME}" autoconnect yes ssid "${ssid}"
    ${NMCLI} con modify "${APCONNNAME}" ipv4.address ${IPV4ADDR}
    ${NMCLI} con modify "${APCONNNAME}" 802-11-wireless.mode ap 802-11-wireless.band bg ipv4.method manual
    ${NMCLI} con modify "${APCONNNAME}" wifi-sec.key-mgmt wpa-psk
    ${NMCLI} con modify "${APCONNNAME}" wifi-sec.psk "${WPAPSK}"
    ${NMCLI} con modify "${APCONNNAME}" wifi-sec.proto rsn
    ${NMCLI} con modify "${APCONNNAME}" wifi-sec.group ccmp
    ${NMCLI} con modify "${APCONNNAME}" wifi-sec.pairwise ccmp
    # ${NMCLI} con modify "${APCONNNAME}" 802-11-wireless.security.pmf 1
}

startAPConn() {
    connUp "${APCONNNAME}"
    exitCode=$?

    if [ $exitCode -eq 10 ]; then
        initSetupMode
    elif [ $exitCode -ne 0 ]; then
        echo "Failed to start SetupMode wifi connectio with error $?"
    fi
}

startRunModeConn() {
    connUp "${RUNCONNNAME}"
    exitCode=$?

    if [ $exitCode -eq 10 ]; then
        echo "RunMode connection not present, switching to SetupMode $?"

        enableSetupModeFlag

        exit 1
    elif [ $exitCode -ne 0 ]; then
        echo "Failed to start RunMode wifi connection with error $?"
    fi
}

enableSetupModeFlag() {
    echo "Enabling setup mode"
    touch "$SETUPMODE_FILE"
}

disableSetupModeFlag() {
    echo "Disabling setup mode"
    rm -f "$SETUPMODE_FILE"
}

enableDNSMasq() {
    echo "Enabling DNSMasq"

    ${SYSTEMCTL} enable dnsmasq && \
    ${SYSTEMCTL} start dnsmasq
    exitCode=$?

    if [ $exitCode -ne 0 ]; then
        echo "Failed to enable DNSMasq with error ${exitCode}"

        exit ${exitCode}
    fi

}

disableDNSMasq() {
    echo "Disabling DNSMasq"

    ${SYSTEMCTL} stop dnsmasq && \
    ${SYSTEMCTL} disable dnsmasq
    exitCode=$?

    if [ $exitCode -ne 0 ]; then
        echo "Failed to disable DNSMasq with error ${exitCode}"

        exit ${exitCode}
    fi

}

setupRequired() {
    if ! runModeExists; then
        return 0
    fi

    if [ -f "$SETUPMODE_FILE" ]; then
        return 0
    else
        return 1
    fi
}


if setupRequired; then
    echo "Setup required, starting captured Wifi network and launching in setup mode."
    
    startAPConn
    enableDNSMasq

   ${SIMTEZILO} -s
    if [ $? -ne 0 ]; then
        exitCode=$?
        echo "Simtezilo setup mode exited with error ${exitCode}"

        exit ${exitCode}
    fi
    
    disableDNSMasq
    disableSetupModeFlag
    startRunModeConn

    echo "Simtezilo setup mode completed successfully"

    exit 0
else
    echo "Run mode detected. Launching Simtezilo"

    ${SIMTEZILO} $OPTIONS
    if [ $? -eq 33 ]; then
        echo "Simtezilo exited into setup mode."

        enableSetupModeFlag

        exit 0
    elif [ $? -ne 0 ]; then
        exitCode=$?
        echo "Simtezilo run mode exited with error ${exitCode}"

        exit ${exitCode}
    fi

    echo "Simtezilo run mode exited normally"
fi
