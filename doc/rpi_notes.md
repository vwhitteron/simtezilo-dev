# Raspberry Pi Compute Module 4

## Installation

From a macOS machine, you can also run usbboot, just follow the same steps:

1. Clone the usbboot repository
2. Install libusb (`brew install libusb`)
3. Install pkg-config (`brew install pkg-config`)
4. (Optional) Export the `PKG_CONFIG_PATH` so that it includes the directory enclosing `libusb-1.0.pc`
5. Build using make
6. Run the binary

```
git clone --recurse-submodules --shallow-submodules --depth=1 https://github.com/raspberrypi/usbboot
cd usbboot
brew install libusb
brew install pkg-config
make
sudo ./rpiboot
```

If the build is unable to find the header file libusb.h then most likely the `PKG_CONFIG_PATH` is not set properly. This should be set via `export PKG_CONFIG_PATH="$(brew --prefix libusb)/lib/pkgconfig"`.

If the build fails on an ARM-based Mac with a linker error such as `ld: warning: ignoring file '/usr/local/Cellar/libusb/1.0.27/lib/libusb-1.0.0.dylib': found architecture 'x86_64', required architecture 'arm64'` then you may need to build and install `libusb-1.0` yourself:

```
curl -OL https://github.com/libusb/libusb/releases/download/v1.0.27/libusb-1.0.27.tar.bz2
tar -xf libusb-1.0.27.tar.bz2
cd libusb-1.0.27
./configure
make
make check
sudo make INSTALL_PREFIX=/usr/local install
cd ..
```

Running make again should now succeed.


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