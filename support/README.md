# Simtezilo


## Installation

Copy the Simtezilo directory to a location of your choice. All instructions here assume that the
files remain contained within the Simtezilo directory.

## Configuration

### Application Section

#### logLevel

The output log level. Defaults to `warn` and can be set to any of the following:

`trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic` and `off`

#### replayMode

Enable output of haptics during replays or only live racing. Defaults to `false`

### Telemetry Section

TODO: Complete sections for the missing configuration options

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

To find an appropriate value run the app and adjust the volume setting using the [live controls](#live-controls).

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
however many audio devices do not accept sample rates below 32kHz.

The haptics signal outputs frequencies between 16Hz and 60Hz so a sample rate as low as 8kHz is more than
adequate if the audio device supports it.

## Running the App

Running the app is as simple changing to the Simtezilo directory and executing the binary:

#### Windows
```
cd c:\Users\MyUser\Simtezilo
simtezilo.exe
```

#### MacOS

MacOS will block execution of the script by default since it is not signed. To enable execution do the following:

1. Try starting the app once and click the _Done_ button when presented with a dialog stating that the app could not be verified.
2. Open _System Preferences_, navigate to _Privacy & Security_ and scroll down to the _Security_ section.
3. Locate the entry saying _"simtezilo" was blocked to protect your Mac_ and click on the _Allow Anyway_ button.
4. Start the app again and it should now be running.

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

### Web UI

The web user interface provides live graphing of telemetry, open http://localhost:8080 to view the UI.

```
simtezilo -w=true
```

## Live Controls

When running the app controls are available to view and modify settings live.

|     Key     |               Action               |
|-------------|------------------------------------|
| Left Arrow  | Switch to the previous setting     |
| Right Arrow | Switch to the next setting         |
| Up arrow    | Increase the current setting value | 
| Down arrow  | Decrease the current setting value |
| Q or Esc    | Quit the application               |

Once acceptable values have been found using the live controls the configuration file will need to be manually updated to make them the default at startup.