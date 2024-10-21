# RaceSig #

Software for processing sim racing telemetry signals and outputting the data to various interfaces:

* Haptic devices
* LCD dasboard
* Web dashboard


# Installation


## Raspberry Pi

Disable on-board audio by adding `dtparam=audio=off` to `/boot/config.txt`.

### Pimoroni PirateAudio Line Out

Add the following to `/boot/config.txt`.

```
dtoverlay=hifiberry-dac
gpio=25=op,dh 
```

Recommended gain setting is -15dB

### Buttkicker USB

With `dtparam=audio=off` set, Alsa should see the Butkicker USB device as the only audio output.

Recommended gain setting is 0dB.


# Notes

## Disable/fix USB audio device ordering
`/lib/modprove.d/aliases.conf`
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