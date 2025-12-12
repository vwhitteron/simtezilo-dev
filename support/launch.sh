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
    exitCode=$?
    if [ $exitCode -ne 0 ]; then
        echo "Starting ${CONNNAME} connection failed with error ${exitCode}"

        exit 1
    fi
}

enableDNSMasq() {
    echo "Enabling DNSMasq"

    systemctl enable dnsmasq
    systemctl start dnsmasq

    if [ $? -ne 0 ]; then
        exitCode=$?
        echo "Enabling DNSMasq failed with error ${exitCode}"

        exit ${exitCode}
    fi

}

disableDNSMasq() {
    echo "Disabling DNSMasq"

    systemctl disable dnsmasq
    systemctl stop dnsmasq

    if [ $? -ne 0 ]; then
        exitCode=$?
        echo "Disabling DNSMasq failed with error ${exitCode}"

        exit ${exitCode}
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

initSetupMode() {
    if ! apConnExists; then
        echo "Creating SetupMode access point connection."

        setupAPConn
        startAPConn

        enableSetupModeFlag
    fi
}



if [ -f "$SETUPMODE_FILE" ]; then
    echo "Setup mode detected. Staring AP mode and launching setup wizard"
    
    startAPConn
    if [ $? -eq 10 ]; then
        initSetupMode
    elif [ $? -ne 0 ]; then
        echo "Starting SetupMode access point failed with error $?"
    fi

    enableDNSMasq

    /opt/simtezilo/bin/setupwizard
    if [ $? -ne 0 ]; then
        exitCode=$?
        echo "Setup wizard exited with error ${exitCode}"

        exit ${exitCode}
    fi
    echo "Setup wizard completed successfully"
    
    disableDNSMasq
    disableSetupModeFlag

    exit 0
else
    echo "Run mode detected. Launching Simtezilo"

    /opt/simtezilo/bin/simtezilo $OPTIONS

    if [ $? -eq 33 ]; then
        echo "Simtezilo exited into setup mode."

        enableDNSMasq
        enableSetupModeFlag

        exit 0
    elif [ $? -ne 0 ]; then
        exitCode=$?
        echo "Simtezilo exited with error ${exitCode}"

        exit ${exitCode}
    fi

    echo "Simtezilo exited normally"
fi
