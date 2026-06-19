#!/bin/sh

VERSION="1.1.0"

SETUPMODEFLAG="/opt/simtezilo/etc/setupmode"

# Status results
RESULT='success'
ACTIVECONN='RunMode'
AVAILABLE='true'
FLAGENABLED='false'
READY='true'
RUNMODEPRESENT='true'
SETUPMODEPRESENT='true'
SETUPREQUIRED='false'
LCDPRESENT='false'
SSHENABLED='true'

# Mock Bluetooth devices
BT_ADAPTER='"adapter":{"present":true,"powered":true,"discovering":false,"address":"00:00:00:00:00:01"}'
BT_PAIRED_SPEAKER='{"address":"00:11:22:33:44:01","name":"Inbuilt Speakers","type":"speaker","paired":true,"trusted":true,"connected":true,"rssi":-45}'
BT_PAIRED_HEADPHONES='{"address":"00:11:22:33:44:02","name":"Wireless Headphones","type":"headphones","paired":true,"trusted":true,"connected":false,"rssi":-62}'
BT_SCAN_SPEAKER='{"address":"00:11:22:33:44:10","name":"Portable Speaker","type":"speaker","paired":false,"trusted":false,"connected":false,"rssi":-74}'
BT_SCAN_HEADSET='{"address":"00:11:22:33:44:11","name":"Wireless Headset","type":"headset","paired":false,"trusted":false,"connected":false,"rssi":-83}'
BT_SCAN_FANCTLR='{"address":"00:11:22:33:44:03","name":"Wind Simulator","type":"fan","paired":false,"trusted":false,"connected":false,"rssi":-62}'



function handle_usage() {
    echo 'Usage: /opt/simtezilo/bin/setup [options] <command>'
    echo 'Options:'
    echo '  -h              Show this help message'
    echo '  -l <level>      Set log level (debug, info, warn, error)'
    echo '  -v              Show version information'
    echo
    echo 'Commands:'
    echo '  bt-connect      Connect to a Bluetooth device (stdin: {"address":"<string>"})'
    echo '  bt-disconnect   Disconnect from a Bluetooth device (stdin: {"address":"<string>"})'
    echo '  bt-list         List paired and connected Bluetooth devices'
    echo '  bt-pair         Pair and connect to a Bluetooth device (stdin: {"address":"<string>"})'
    echo '  bt-remove       Remove a paired Bluetooth device (stdin: {"address":"<string>"})'
    echo '  bt-scan         Scan for available Bluetooth devices'
    echo '  bt-status       Check Bluetooth adapter status'
    echo '  init            Initialize setup mode connection if not present'
    echo '  mode-run        Enter run mode'
    echo '  mode-setup      Enter setup mode'
    echo '  reset           Delete all connections and reinitialize setup mode'
    echo '  setup-disable   Disable setup mode flag'
    echo '  setup-enable    Enable setup mode flag'
    echo '  signal-start    Send start signal to the system'
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

function handle_bt_status() {
    echo '{"result":"success",'${BT_ADAPTER}'}'
}

function handle_bt_list() {
    echo '{"result":"success",'${BT_ADAPTER}',"btDevices":['$BT_PAIRED_SPEAKER','$BT_PAIRED_HEADPHONES']}'
}

function handle_bt_scan() {
    BT_PAIRED=''${BT_PAIRED_SPEAKER}','${BT_PAIRED_HEADPHONES}''
    BT_DISCOVERED=''${BT_SCAN_SPEAKER}','${BT_SCAN_HEADSET}','${BT_SCAN_FANCTLR}''
    echo '{"result":"success",'${BT_ADAPTER}',"btDevices":['${BT_PAIRED}','${BT_DISCOVERED}']}'
}

function handle_bt_action() {
    # Connect/disconnect/pair/remove: drain any stdin payload, report success.
    cat > /dev/null 2>&1
    echo '{"result":"success",'${BT_ADAPTER}'}'
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

function handle_status() {
    echo '{"result":"'${RESULT}'","status":{"activeConn":"'${ACTIVECONN}'","available":'${AVAILABLE}',"flagEnabled":'${FLAGENABLED}',"ready":'${READY}',"runModePresent":'${RUNMODEPRESENT}',"setupModePresent":'${SETUPMODEPRESENT}',"setupRequired":'${SETUPREQUIRED}',"lcdPresent":'${LCDPRESENT}',"sshEnabled":'${SSHENABLED}'}}'
}

function handle_version() {
    echo "simtezilo-platform-local version ${VERSION}"
}

function handle_wifi_access() {
    echo '{"result":"success","wifi":{"ssid":"Simtezilo-00000000","psk":"5imtezil0","security":"wpa2"}}'
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

while getopts "hl:v" Option
do
  case $Option in
    l ) LOGLEVEL=$OPTARG;;
    v ) handle_version
        exit 0
        ;;
    * ) handle_usage;;
  esac
done

shift $(($OPTIND - 1))

ACTION=$1

case $ACTION in
    "bt-connect")
        handle_bt_action
        ;;
    "bt-disconnect")
        handle_bt_action
        ;;
    "bt-list")
        handle_bt_list
        ;;
    "bt-pair")
        handle_bt_action
        ;;
    "bt-remove")
        handle_bt_action
        ;;
    "bt-scan")
        handle_bt_scan
        ;;
    "bt-status")
        handle_bt_status
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
    "reset")
        return_success
        ;;
 	"setup-disable")
 		handle_setup_disable
 		;;
 	"setup-enable")
 		handle_setup_enable
 		;;
    "signal-start")
        return_success
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
    "update-apply")
        return_success
        ;;
    "update-rollback")
        return_success
        ;;
 	"version")
 		handle_version
 		;;
 	"wifi-access")
        handle_wifi_access
        ;;
    "wifi-provision")
        handle_wifi_provision
        ;;
    "wifi-scan")
        handle_wifi_scan
        ;;
 	*)
 		handle_usage
 		;;
esac
