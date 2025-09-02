# Haptic tuning

## Engine tuning

## Primary balance

TODO

## Secondary balance

TODO

### Engine pulse gain

Due to the differences in percieved haptic strength resulting from the balance
and pulse scale parameters some engines will feel stronger than others. The
gain setting is used to reduce the haptics of engines with stronger vibrations
so that they are about equal with the weaker ones. Ideally engines with less
balance should generally feel a little stronger than those with inherent natual
balance.

#### Tuning procedure

1. Set the main engine gain to -0.0
2. Test drive each engine type in the `haptics.engineProfiles` configuration
and adjust the engine gain so that the vibrations are just within tactile
range. The aim is ti find the quietest level at which useable tactile feedback
can be achieved.
3. When all engine parameters have been recorded find the smallest value and
set the main engine gain to this value
4. Increase the gain value for each engine in the config by the smallest value
found in step 3

Now the user can adjust the engine haptics through the global engine gain
setting according to their preference.

### Engine pulse scale

This setting adjusts the frequency at which engine pulses are created. Engines
with  high cylinder counts and/or high RPM will result in the pulses being too
close together and will fall out of tactile range. This setting can bring the
effective frequencies back into tactile range and can also be used to get an
approporate vibration feel for a given engine geometry.