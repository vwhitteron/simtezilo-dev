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
