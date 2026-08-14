# Chassis waveform naming cleanup

Rename the confusingly-named variables in the chassis pulse generation pathway and
document the three pieces of unavoidable DSP jargon at their definition sites.

Pure naming and comments — no behaviour change intended. Verification below relies
on that.

## Background

`pulseWidth` is `fs/2f`, a **half**-period, but "pulse width" conventionally means
the full duration of a pulse (as in pulse-width modulation). `pulseLength` is
`2 * pulseWidth`, the full period. So "width" and "length" mean different things
despite reading as synonyms, and the more standard-sounding of the two carries the
non-standard meaning. `signal.Equalize` confirms the half-period reading by
inverting it as `fs / (2 * pulseWidth)`.

`waveSamplePeriod` is `π / pulseWidth` — radians per sample, i.e. an angular
frequency. The name states the reciprocal of what the value is.

Terminology was chosen to avoid analogy. Granular-synthesis vocabulary (grain, hop
size) was considered and rejected: "grain" is a metaphor from Gabor's acoustic
quanta and means nothing without that background. The file already uses "burst"
for the same object, so that stays.

## Phase 1 — legacy waveform pathway

`app/haptics/chassis.go`, `legacyPulseWaveform` (~L213-230):

| From | To |
| --- | --- |
| `pulseWidth` (param) | `halfPeriodSamples` |
| `pulseLength` | `periodSamples` |
| `waveOffset` | `quarterPeriodSamples` |
| `waveSamplePeriod` | `phaseIncrement` |
| `amplitude` (param) | `signedAmplitude` |

`signedAmplitude` because the sign is load-bearing — it carries the jerk's sign,
which is what stops a train of bumps accumulating a DC offset (see the comment at
`calculateChassisHapticPulseAmplitude`). Easy to mistake for a magnitude.

`periodSamples` is already the name used for this quantity in `chassisDecayTau`,
so this aligns the two.

Call sites to update:

- L98, L101, L114 — `channelPulseWidth` -> `channelHalfPeriodSamples`
- L122-124 — metrics block; `pulseDuration` is accurate, leave it
- `app/haptics/chassis_shape_internal_test.go` L107-108

**Log key:** L137 is `Float64("pulseWidth", …)` — a zerolog key, so observable
output rather than just an identifier. Rename to `half_period_samples` to match the
snake_case keys around it (`process_time`, `sequence_id`). It is debug-only and
behind an amplitude-clip branch, so the risk of breaking a consumer is low.

## Phase 2 — decay pathway consistency

`decayPulseWaveform` (~L176-205): `period` -> `periodSamples`,
`amplitude` -> `signedAmplitude`. `bumpLength`, `ringLength` and `tau` already read
fine.

Naming both builders' period identically exposes that they are not computed
identically: legacy uses `2 * round(fs/2f)` (rounds to an even sample count), decay
uses `int(fs/f)` (truncates). They can differ by one sample. Leave the behaviour
alone; add a one-line comment noting the difference.

## Phase 3 — plain-language block naming

`minSamplesPerFrame` -> `minBlockSamples` (6 sites, all in `chassis.go`).

"Frame" currently means three different things in this codebase: a telemetry packet
(`Frame`, `FrameIndex`, `telemetryFrameRate`), an audio block (`minSamplesPerFrame`,
`hapticFrameRate`), and — in PortAudio, which this eventually feeds — one sample
across all channels. This removes one of the three.

### Trap

`minSamplesPerFrame` derives from `hapticFrameRate = 120` (8.33 ms), but bursts
regenerate once per **telemetry** packet at 60 Hz (16.7 ms). These are not the same
quantity. Any burst-interval or concurrent-burst figure must be derived from
`telemetryFrameRate`. Getting this backwards halves every overlap number.

Because of that, do **not** introduce a computed `burstOverlap` value — nothing
would consume it. Instead extend the `maxBurstCycles`/`maxBurstSeconds` comment
(L17-20), which already speaks plainly about "overlapping sixty telemetry frames",
with the measured figures: roughly 7.5 concurrent bursts for the legacy shape
versus 22 at the decay defaults, measured on a Spa/GR010 capture.

## Phase 4 — define the jargon at its definition site

Three terms have no plainer equivalent, so document them where they are defined
rather than renaming them.

- **T60** — extend the `decayEnvelopeTails` comment (L12-15). It already says
  "4 tau … -35 dB"; add that a time constant is the 1/e decay time, and that the
  audio convention T60 (60 dB down) is about 6.91 tau, making 4 tau roughly half a
  T60.
- **Knee** — extend the `softKneeThreshold` comment in
  `app/synthesizer/synth_utils.go` (L140-144). It says what the threshold does but
  not what "knee" means: the bend in the input/output transfer curve, above which
  the mapping stops being 1:1.
- **Hann** — no const exists to hang it on; it is inline arithmetic in both
  builders. Add one clause to each builder's doc comment: a Hann window is a single
  raised-cosine hump, zero at both ends, peak in the middle. The comment at L162-163
  already half-does this.

Extracting a `hannWindow(index, periodSamples)` helper was considered and rejected
for now: it adds a call inside a per-sample loop, and it would force both builders
through one definition when they currently differ (see Phase 2). That is a
behaviour-adjacent refactor, not a naming change.

## Phase 5 — deferred

`pulseWidth` is also the parameter name of `signal.Equalize`
(`app/signal/signal.go` L21), consumed by `app/haptics/texture.go` (L165, L187),
and `app/synthesizer/synth_effects.go` (L108-110) carries a near-copy of the
`waveOffset`/`waveSamplePeriod` idiom — with `waveOffset := pulseWidth`, not
`/2`, so it is similar but not identical.

Same misnomer, but the fix spreads across three packages. Deferred.

## Verification

1. `go build ./...`
2. `go test ./app/haptics/... ./app/synthesizer/... ./app/signal/...`
3. Re-run the offline chassis A/B capture harness and confirm the output is
   byte-identical to the pre-change numbers.

Step 3 is the real check. Every change above is a rename or a comment, so any drift
in RMS, band power or spectral centroid means something broke. The harness drives
`haptics.CaptureChassis` over a recorded replay and is CGO-free, so it runs without
an audio backend.

Reference figures from the pre-change run (Spa / GR010, 46.63 s, 2798 frames,
legacy shape): RMS 0.2045, peak 0.9711, crest 13.53 dB, spectral centroid 9.767 Hz.
