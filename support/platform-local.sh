#!/bin/sh

SETUPMODEFLAG="/opt/simtezilo/etc/setupmode"

# Status results
RESULT='success'
ACTIVECONN='SetupMode'
AVAILABLE='true'
FLAGENABLED='true'
READY='true'
RUNMODEPRESENT='true'
SETUPMODEPRESENT='true'
SETUPREQUIRED='false'
LCDPRESENT='true'
SSHENABLED='true'



function handle_usage() {
    echo 'Usage: /opt/simtezilo/bin/setup [options] <command>'
    echo 'Options:'
    echo '  -h              Show this help message'
    echo '  -l <level>      Set log level (debug, info, warn, error)'
    echo '  -v              Show version information'
    echo
    echo 'Commands:'
    echo '  init            Initialize setup mode connection if not present'
    echo '  mode-run        Enter run mode'
    echo '  mode-setup      Enter setup mode'
    echo '  reset           Delete all connections and reinitialize setup mode'
    echo '  setup-disable   Disable setup mode flag'
    echo '  setup-enable    Enable setup mode flag'
    echo '  ssh-enable      Enable SSH access'
    echo '  ssh-disable     Disable SSH access'
    echo '  ssh-provision   Provision SSH access'
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

    exit 1
}

function return_success() {
    echo '{"result":"success"}'
}

function handle_setup_enable() {
    touch ${SETUPMODEFLAG}
    return_success
}

function handle_setup_disable() {
    rm ${SETUPMODEFLAG}
    return_success
}

function handle_ssh_provision() {
    # Read input JSON
    input=$(cat)

    echo "${input}" > /opt/simtezilo/etc/ssh_provision.json

    return_success
}

function handle_wifi_access() {
    return ""
}

function handle_wifi_provision() {
    # Read input JSON
    input=$(cat)

    echo "${input}" > /opt/simtezilo/etc/provision.json

    return_success
}

function handle_wifi_scan() {
    echo '{"networks":[{"ssid":"yarn","psk":"","security":"wpa2"},{"ssid":"Firetooth","psk":"","security":"wpa3"}],"result":"success"}'

}

function handle_status() {
    echo '{"result":"'${RESULT}'",status":{activeConn":"'${ACTIVECONN}'","available":"'${AVAILABLE}'","flagEnabled":"'${FLAGENABLED}'","ready":"'${READY}'","runModePresent":"'${RUNMODEPRESENT}'","setupModePresent":"'${SETUPMODEPRESENT}'","setupRequired":"'${SETUPREQUIRED}'","lcdPresent":"'${LCDPRESENT}'","sshEnabled":"'${SSHENABLED}'"}}'
}

while getopts "hl:v" Option
do
  case $Option in
    l ) LOGLEVEL=$OPTARG;;
    v ) echo "simtezilo-platform-local version 1.0.0"
        exit 0
        ;;
    * ) handle_usage;;
  esac
done

shift $(($OPTIND - 1))

ACTION=$1

case $ACTION in
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
 		handle_setup_disable
 		;;
 	"setup-enable")
 		handle_setup_enable
 		;;
    "ssh-enable")
        return_success
        ;;
    "ssh-disable")
        return_success
        ;;
    "ssh-provision")
        handle_ssh_provision
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
