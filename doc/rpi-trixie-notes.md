# Raspberry Pi OS based on Debian Trixie (or Bookworm)

* sysfs is fully deprecated and has bee replaced by gpiod

## Setting up GPIO inputs

Set buttons to inputs with pull-up
`sudo pinctrl set 5,6,16,24 ip pu`

Monitor input buttons
`sudo gpiomon -c /dev/gpiochip0 5 6 16 24`


## Wifi AP mode

Get <serial> from /etc/cpuinfo
```
nmcli con add type wifi ifname wlan0 con-name SetupMode autoconnect yes Simtezilo-7f46fe8e Simtezilo-<serial>
nmcli con modify SetupMode ipv4.address 10.33.0.1/24
nmcli con modify SetupMode 802-11-wireless.mode ap 802-11-wireless.band bg ipv4.method shared
nmcli con modify SetupMode wifi-sec.key-mgmt wpa-psk
nmcli con modify SetupMode wifi-sec.psk "5imtezil0"
nmcli con modify SetupMode wifi-sec.group ccmp
nmcli con modify SetupMode wifi-sec.pairwise ccmp
nmcli con modify SetupMode 802-11-wireless.security.pmf 1
```


## Wifi infra mode

User provides <ssid> and <password>
```
nmcli connection add type wifi ifname wlan0 con-name RunMode ssid <ssid>
nmcli connection modify RunMode ipv4.method auto
nmcli connection modify RunMode wifi-sec.key-mgmt wpa-psk
nmcli connection modify RunMode wifi-sec.psk <password>
nmcli connection up RunMode
```

## Wifi other

```
# List available networks
sudo nmcli -f SSID -t d wifi list ifname wlan0 --rescan auto

# Get AP mode SSID
nmcli -f AP dev show wlan0 | awk '/\.SSID:/ {print $NF}'
```