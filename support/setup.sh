#!/bin/sh

SETUPMODEFLAG="/opt/simtezilo/etc/setupmode"

function handle_usage() {
    echo 'Usage: /opt/simtezilo/bin/setup <command>'
    echo 'Commands:'
    echo '  access       Provide the network access detaisl for the setup mode network'
    echo '  disable      Disable setup mode flag'
    echo '  enable       Enable setup mode flag'
    echo '  init         Initialize setup mode connection if not present'
    echo '  mode-run     Enter run mode'
    echo '  mode-setup   Enter setup mode'
    echo '  provision    Provision network connection'
    echo '  reset        Delete all connections and reinitialize setup mode'
    echo '  scan         Scan for available WiFi networks'
    echo '  status       Check current environment status'
    echo '  version      Print version information'
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
 	"access")
        handle_access
        ;;
 	"disable")
 		handle_disable
 		;;
 	"enable")
 		handle_enable
 		;;
    "init")
        return_success
        ;;
    "mode-run")
        return_success
        ;;
    "mode-setup")
        return_success
        ;;
    "provision")
        handle_provision
        ;;
    "reset")
        return_success
        ;;
    "scan")
        handle_scan
        ;;
 	"status")
 		handle_status
 		;;
 	"version")
 		handle_version
 		;;
 	*)
 		handle_usage
 		;;
esac
