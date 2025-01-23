# Simtezilo


## Installation

Copy the Simtezilo directory to a location of your choice. All instructions here assume that the
files remain contained within the Simtezilo directory.

## Configuration

### Application Section

#### assetDir

The filesystem location where application support files can be found.

For Windows paths make sure to use double forward slashes for all directory delimeters, for example
`assetDir = "c:\\Users\\MyUser\\Simtezilo\\assets"`

#### logLevel

The output log level. Defaults to `warn` and can be set to any of the following:

`trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` and `off`

#### replayMode

Enable output of haptics during replays or only live racing. Defaults to `false`

### Telemetry Section

#### source

The telemetry source to read for generating haptics. Accepts both network and filesystem locations.

The default location is `udp://255.255.255.255:33739` which should automatically find a PlayStation running
Gran Turismo 7. If this doesn't work for any reason replace the IP address with the address assigned to the
PlayStation device.

Filesystem locations should always be prefixed with `file://`. When setting a file location you will also
need to set `replayMode = true` for haptics to be output.

For Windows paths make sure to use double forward slashes for all directory delimeters.

An example replay file named `replay.gtz` is included in the Simtezilo directory.

### Synthesizer section

#### masterGain

The master gain or volume level for the output signal. This is set to a low value by default to reduce the
risk of damage to your equipment.

To find an appropriate value run the app and adjust the volume to using the [volume controls](#live-controls).

#### chassisVolume

The output volume of chassis bump haptics. Accepts an integer percentage between 0 and 100 and defaults to
100.

#### gearRaceVolume

The output volume of gear change haptics for racing vehicles. Accepts an integer percentage between 0 and 100
and defaults to 100.

You may find the default value of 100% too strong. If so consider reducing this value.

#### gearStreetVolume

The output volume of gear change haptics for street vehicles. Accepts an integer percentage between 0 and 100
and defailts to 60.

#### sampleRateHz

The sampe rate to use when generating the haptics signal. Lower sample rates require less computing power
however most audio devices do not accept sample rates below 32kHz.

The haptics signal outputs frequencies between 16Hz and 60Hz so a sample rate as low as 8kHz is more than
adequate if the audio device supports it.

#### forceProfile

There are 10 force feedback profiles that can be selected, with 1 being fairly weak and 10 being quite strong.

All profiles will output a signal volume between 0 and 100% however profile 1 is spread more evenly across the
range while 10 has a faster ramp rate from weak to strong forces so will feel a lot more aggresive.

Play around with the profiles using the [live control](#live-controls) to find a value you prefer.

#### grainProfile

There are 10 feedback grain profiles that can be selected, with 1 being fairly weak and 10 being quite strong.

All profiles adjust the pulse frequency of the haptics between the minimum (16Hz) and maximum (60Hz) frequencies
Profile 1 is spread more evenly across the range of frequencies so will generally provide more subtle feedback
except for very strong impacts. Profile 10 ramps up faster so will feel a lot more aggresive with less dynamic
range between small and large impacts.

Play around with the profiles using the [live control](#live-controls) to find a value you prefer.


## Running the App

Running the app is as simple changing to the Simtezilo directory and executing the binary:

#### Windows
```
cd c:\Users\MyUser\Simtezilo
simtezilo.exe
```

#### MacOS
```
cd /Users/myuser/Simtezilo
./simtezilo
```

#### Linux
```
cd /home/myuser/Simtezilo
./simtezilo
```

### Arguments

The app supports some arguments as follows:

### Help

Show help for the application

```
simtezilo -h
```

### Log Level

Override the log level in the configuration file. Accepts the same log levels as mentioned in the config
section above.

```
simtezilo -l debug
```

### Web Charts

Web charts of some of the live telemetry can be viewed in a web browser at http://localhost:8080 when enabled.

```
simtezilo -w
```

Note that closure of the websockets are not porperly handled at this time. If you refresh the browser while
the app is running you might find that it slows down. This can be fixed by restarting the app and avoiding
refreshing the browser window.

## Live Controls

When running the app there are a few controls for 

### Volume

Keys: +/-

The [master gain](#mastergain) setting can be adjusted up and down using the equals (plus) and minus keys.

### Force Feedback Strength

Keys: up/down arrow

The [force profile](#forceprofile) setting can be adjusted up or down with the up and down arrow keys.

### Force Feedback Grain

Keys: left/right arrow

The [grain profile](#grainprofile) setting can be adjusted up or down with the left and right arrow keys.


Once acceptable values have been found using the live controls that can be updated in the configuration file to make them the default.