#!/bin/sh

SETUPMODEFLAG="/opt/simtezilo/etc/setupmode"

function handle_usage() {
    echo 'Usage: /opt/simtezilo/bin/setup <command>'
    echo 'Commands:'
    echo '  init            Initialize setup mode connection if not present'
    echo '  mode-run        Enter run mode'
    echo '  mode-setup      Enter setup mode'
    echo '  reset           Delete all connections and reinitialize setup mode'
    echo '  setup-disable   Disable setup mode flag'
    echo '  setup-enable    Enable setup mode flag'
    echo '  status          Check current environment status'
    echo '  version         Print version information'
    echo '  wifi-access     Provide the network access detaisl for the setup mode network'
    echo '  wifi-provision  Provision network connection'
    echo '  wifi-scan       Scan for available WiFi networks'
    echo
    echo 'provision takes JSON on stdin with the following format:'
    echo '[{'
    echo '  "ssid":"<string>",'
    echo '  "psk":"<string>",'
    echo '  "security":"<wpa2|wpa3>",'
    echo '  "method":"<dhcp|static>",'
    echo '  "ip":"<address>",'
    echo '  "prefix":"<bits>",'
    echo '  "gateway":"<address>",'
    echo '  "dns":"<address>"'
    echo '}]'
}

function return_success() {
    echo '{"result":"success"}'
}

function handle_access() {
    return ""
}

function handle_enable() {
    touch ${SETUPMODEFLAG}
    return_success
}

function handle_disable() {
    rm ${SETUPMODEFLAG}
    return_success
}

function handle_provision() {
    # Read input JSON
    input=$(cat)

    echo "${input}" > /opt/simtezilo/etc/provision.json

    return_success
}

function handle_scan() {
    echo '{"networks":[{"ssid":"yarn","psk":"","security":"wpa2"},{"ssid":"Firetooth","psk":"","security":"wpa3"}],"result":"success"}'

}

function handle_status() {
    if [ -f ${SETUPMODEFLAG} ]; then
        echo '{"result":"success","status":{"activeConn":"SetupMode","available":true,"flagEnabled":true,"ready":true,"runModePresent":true,"setupModePresent":true,"setupRequired":false, "lcdPresent":false}}'
    else
        echo '{"result":"success","status":{"activeConn":"RunMode","available":true,"flagEnabled":false,"ready":true,"runModePresent":true,"setupModePresent":true,"setupRequired":false, "lcdPresent":false}}'
    fi
}

action=$1

case $action in
    "init")
        return_success
        ;;
    "mode-run")
        return_success
        ;;
    "mode-setup")
        return_success
        ;;
    "reset")
        return_success
        ;;
 	"setup-disable")
 		handle_disable
 		;;
 	"setup-enable")
 		handle_enable
 		;;
 	"status")
 		handle_status
 		;;
 	"version")
 		handle_version
 		;;
 	"wifi-access")
        handle_access
        ;;
    "wifi-provision")
        handle_provision
        ;;
    "wifi-scan")
        handle_scan
        ;;
 	*)
 		handle_usage
 		;;
esac
