# What telemetry data should be used to generate the haptic transducer output signals?

## Status

Accepted

## Context

The ouptut signal for the haptic transducers (bass shakers) needs to be generated from data provided by the game
telemetry stream.

The initial project is designed for Gran Turismo 7 telemetry data which provides both individual
suspension height changes at each wheel as well as chassis height and rotationa changes. Either or both of these
sources could be used to generate the haptic output signal.

Using suspension movement as the source for haptic generation has an important issue to consider. One
of the roles of the suspension system is driver comfort so it is designed to absorb the changes in road surface
so that the effects are not felt by the occupant. This is true even for race oriented vehicles as excessive vibration
can result in both fatigue to the driver as well as increased failures within the vehicles systems. Another
consideration is that the damping characteristics of a given suspension system is highly vehicle dependant. In
order to faithfully produce a haptic signal the damping characteristics would need to be simulated to produce an
intermediate signal representing the driver experience. This is both computationally expensive and requires access to
data which the game telemetry may not provide (damping rates, suspension geometry, sprung and unsprung weight, etc).

Alternatively, using the chassis height and rotation as the source for haptic generation avoids a lot of the
complication as it is already a good representation of what the driver will feel. There is still computation involved
to onvert these movements to higher order positional derivatives but it should be significantly less than those
reqiured by using the suspension movement as the source. One minor issue with the chassis movement signal is that
subtle vibration effects like tyre/road noise are harder to expose and may not present at all in the data.

## Decision

Use only the chassis movements to generate the primary haptic output signal as it more faithfully mimics the bump
effects felt by the driver. The suspension movement can also be used to generate a secondary output signal to mimic
low level road noise typically felt through the pedals and floor pan.

## Consequences

The processing requirements for the main bump signal should be reduced as only the chassis height and rotation is
involved in the output signal computation. The secondary road noise signal can also be generated from a simple
analysis of all for corners or choose to monitor only a single corner if it compute overhead becomes a concern.

### 2026-08 update

The reserved secondary road-noise signal now ships as the road-texture layer's roughness envelope: a ~200 ms
sliding RMS of high-passed per-corner suspension velocity, averaged across the four corners, that modulates the
layer's amplitude and cutoff. It deliberately does not supply the carrier, because telemetry at 59.94 Hz carries
nothing usable above ~30 Hz while the texture band sits at 55-150 Hz. The "simple analysis of all four corners"
this ADR anticipated is what shipped, and per-corner damping was not simulated, as the Context predicted would be
impractical.
