# Simtezilo #

Software for processing sim racing telemetry signals and outputting the data to various interfaces:

* Haptic devices
* LCD dasboard
* Web dashboard


# Installation

## Linux

Install [Log2Ram](https://github.com/azlux/log2ram)

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

### Pimoroni PirateAudio Line Out

1. Enable SPI in order to use the LCD
2. Turn the backlight off by driving GPIO 13 low when not in use
3. Enable the hifiberry-dac driver
4. Enable the audio amp by driving GPIO 25 high

Add the following to `/boot/config.txt`.

```
dtparam=spi=on
gpio=13=op,dl
dtoverlay=hifiberry-dac
gpio=25=op,dh
```

### Waveshare 1.3inch LCD HAT

1. Enable SPI in order to use the LCD
2. Turn the backlight off by driving GPIO 24 low when not in use


```
dtparam=spi=on
gpio=24=op,dl
```

### Spotpear Gaeme 1.3

1. Download [audremap18.dtbo](https://cdn.static.spotpear.com/uploads/download/diver/gm154/audremap18.dtbo) to `/boot/overlays/`
2. Enable SPI in order to use the LCD
3. Enable PWM output on pin 18


```
dtparam=spi=on
dtoverlay=audremap18,pins_18_19
```

If no audio devices are found when executing `aplay -l` then the standard remap overlay can be used however the select button
on GPIO 19 will no longer work as it will be set for PWM output instead:

```
dtoverlay=audremap,pins_18_19
```


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