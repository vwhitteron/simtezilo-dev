# GT Pi #

Software for Raspberry Pi to read Gran Turismo telemetry and output the data to multiple interfaces:

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