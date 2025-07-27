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

Add the following to `/boot/config.txt`.

```
dtoverlay=hifiberry-dac
gpio=25=op,dh
dtparam=spi=on
```

Recommended master gain setting:

|          HAT type          |     Gain setting     |
|----------------------------|----------------------|
| Pirate Audio Line-Out      | -17.75 dB            |
| Pirate Audio Headphone Amp | - 7.00 dB (low gain) |

### Buttkicker USB

With `dtparam=audio=off` set, Alsa should see the Butkicker USB device as the only audio output.

Recommended gain setting is 0dB.


# Notes

## Disable/fix USB audio device ordering
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