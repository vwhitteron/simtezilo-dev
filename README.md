# Simtezilo #

Software for processing sim racing telemetry signals and outputting the data to various interfaces:

* Haptic devices
* LCD dasboard
* Web dashboard


# Installation

## Linux

1. Install [Log2Ram](https://github.com/azlux/log2ram)
2. Install Opus libraries for pit radio voice comms to Discord
   ```
   apt-get update && apt-get install libopus0
   ```


## Raspberry Pi

1. Disable on-board audio by adding `dtparam=audio=off` to `/boot/config.txt`.
2. Edit `/etc/fstab` and add the following line
   ```
   tmpfs /var/log tmpfs defaults,size=20M 0 0
   ```
3. Update journald config in `/etc/systemd/journald.conf`
   ```
   SystemMaxUse=50M
   ForwardToSyslog=no
   ```

### Pimoroni Pirate Audio Series

[Product page](https://learn.pimoroni.com/article/getting-started-with-pirate-audio)

Both the Pirate Audio Line Out and Pirate Audio Headphone Amp can be used. The Headphone Amp version will typically provide the 
best results when set to low gain mode as the output voltage more closely matches the various input voltage of most haptic
amplifiers (input levels of 50-200 millivolts). The Line Out version is better suited to amplifiers that accept around input
levels of ~2.1 volts.


1. Enable SPI in order to use the LCD
2. Turn the backlight off by driving GPIO 13 low when not in use
3. Enable the hifiberry-dac driver
4. Enable the audio amp by driving GPIO 25 high
5. Set button GPIO pins to input, driven high

Add the following to `/boot/config.txt`.

```
dtparam=spi=on
gpio=13=op,dl

dtoverlay=hifiberry-dac
gpio=25=op,dh

gpio=5=ip,dh
gpio=6=ip,dh
gpio=16=ip,dh
gpio=24=ip,dh
```

### Waveshare 1.3inch LCD HAT

[Product page](https://www.waveshare.com/wiki/1.3inch_LCD_HAT)

1. Enable SPI in order to use the LCD
2. Turn the backlight off by driving GPIO 24 low when not in use


```
dtparam=spi=on
gpio=24=op,dl
```

### Spotpear RPi-1.3inch-MINI-LCD

[Product page](https://spotpear.com/shop/Raspberry-Pi-LCD-Display-Screen-1.3-inch-LCD-Game-RP2040-PiZero-WS.html)

1. Download [audremap18.dtbo](https://cdn.static.spotpear.com/uploads/download/diver/gm154/audremap18.dtbo) to `/boot/overlays/`
2. Enable SPI in order to use the LCD
3. Enable PWM output on pin 18


```
dtparam=spi=on
dtoverlay=audremap18,pins_18_19
```

_If no audio devices are found when executing `aplay -l` then the standard remap overlay can be used by setting `dtoverlay=audremap,pins_18_19`. Note that the select button on GPIO 19 will no longer work as it will be configured for PWM output._


### Buttkicker USB

With `dtparam=audio=off` set, Alsa should see the Butkicker USB device as the only audio output.

Recommended gain setting is 0dB.

## Simtezilo

Recommended master gain setting:

|          HAT type          |     Gain setting     | Interface |
|----------------------------|----------------------|-----------|
| Pirate Audio Line-Out      | -17.75 dB            |   audio   |
| Pirate Audio Headphone Amp | - 7.00 dB (low gain) |   audio   |
| Spotpear Game 1.3          | -14.75 dB            |   audio   |
| Waveshare 1.3inch LCD HAT  | - 0.00 dB            |    usb    |

# Troubleshooting

## Fix USB audio device ordering

`/lib/modprobe.d/aliases.conf`
Comment out the following
#options snd-usb-audio index=-2

## Fix asound audio rerouting

`~/.asound.rc`
```
pcm.!default { 
   type hw 
#   card PRO 
   card 0 
} 
 
ctl.!default { 
   type hw 
#   card PRO 
   card 0 
}
```