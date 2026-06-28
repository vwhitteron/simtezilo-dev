# Phase-continuous engine haptics (Option B) + audio-cleanliness tests

## Context

Commit `3277fae` introduced pop/click artefacts in the engine haptic output —
clean at low frequencies, audibly worse at higher RPM. Root cause: the engine
waveform is **regenerated from pulse-phase 0 every 30 Hz tick**, then spliced into
the channel with a zero-crossing stitch (`prepareEngineBuffer` /
`adjustEngineBufferPolarity` + an `offset` overwrite). The same commit added
`CapDepth` to bound channel latency, which snaps `writePos = readPos + used`
**after** the write. That redefines `writePos` as the end of *readable* data, so:

- the next tick's zero-crossing search (`InspectChannelBuffer(..., -lookback)`
  then `inspectBuffer[lookback:samplesPerFrame]`) scans the region *forward* of
  `writePos` — now the discarded/stale tail, not the audio about to play; and
- the `offset` forward-shift leaves an `offset`-sized gap of **stale samples** in
  the readable stream every tick.

The consumer therefore plays `previous tail → stale gap → new head` each tick. At
high RPM the pulses are sharper/denser, so the seam lands across a steep edge — an
audible click. Low RPM waveforms are near-flat at the seam, so they stay clean.
The existing `synth_buffer_capdepth_test.go` only writes at `offset 0`, so it
never exercised the broken path.

**Option B** removes splicing entirely: make the generator a pure function of an
absolute, monotonic phase carried across ticks, and append each tick's samples
contiguously. Consecutive blocks are then inherently continuous — no stitch, no
cap, no stale gap. We also add reliable, threshold-free tests that catch any
re-introduction of seam artefacts.

Decisions confirmed with user: remove the obsolete stitch/cap machinery entirely;
extract the artefact metrics into a shared package; ramp amplitude across blocks.
**Work is sequenced test-first** so each implementation step is validated as it
lands (see "Implementation sequence" below).

## Implementation sequence (test-first)

1. **Extract `audioqa` (Part 2).** Pure refactor of existing, working metric code
   — lands green immediately and gives us the measurement vocabulary before any
   behaviour changes.
2. **Prove the detector catches the bug (throwaway).** Drive the *current*
   `offset-overwrite + CapDepth + drain` pattern with a real waveform and assert
   no stale-gap discontinuity via `audioqa`; confirm it **FAILS on today's code**.
   This validates that `audioqa` actually detects the artefact. It references the
   primitives this fix deletes, so it is transitional — not committed (or removed
   with those APIs in step 5).
3. **Write the durable tests against the target API (Part 3).** They are **red**
   (won't compile / fail) because the new generator + `ChannelDepth` don't exist
   yet — this is the specification.
4. **Implement Part 1** (generator + refill-append) until step-3 tests go green.
5. **Remove the obsolete machinery (Part 1 cleanup)** and rewire
   `generateEngineHaptic`; full build + the durable tests re-validate. Drop the
   throwaway test and the old `capdepth` test.

The detailed parts follow.

## Progress tracking

The checklist below is updated as each step completes — so progress is visible
in-tree, not just in chat.

- [x] 1. Extract `audioqa` package; `audio_cleanup` tool re-imports it; tool still PASSes
- [x] 2. Throwaway bug-repro proves `audioqa` flags the artefact on current code (248 glitches, maxStep 6.7x bound) + audioqa unit tests
- [x] 3. Durable tests A–E written against target API (red: generator API absent)
- [x] 4. Generator implemented; tests A–E green (E narrowed to resampler stage; async ring covered by async_test/tool)
- [x] 5. Obsolete machinery removed (`CapDepth`/`CapChannelDepth` → `ChannelDepth`, stitch helpers, dead per-tick generators), `generateEngineHaptic` rewired to refill-append; throwaway + `capdepth` tests dropped
- [x] 6. Verified: `go build/vet ./...` clean, full `go test ./...` passes, `audio_cleanup` PASSes all stages, new files lint-clean. Remaining: manual high-RPM listen on hardware (portaudio build) to confirm clicks gone.

## Part 1 — Phase-continuous engine generation

Primary file: `app/app_haptics_engine.go` (note its own TODO: "mostly LLM
generated and needs heavy refactoring"). Extract the pure generation into a small
testable unit so tests don't need a full `App`.

1. **New `engineWaveformGenerator`** (new file `app/app_haptics_engine_generator.go`,
   `package app`) holding the carried phase state:
   - `pulsePhase float64` — accumulated pulse cycles (NOT `index/samplesPerPulse`),
     so a per-tick `pulseRate` change does not jump phase.
   - `pulsePolarity bool` — flips when `floor(pulsePhase)` increments.
   - `lastAmplitude float64` — for cross-block amplitude ramping.
   - `roughnessSeq` cursor — replaces the per-tick `index`/`sequenceNumber`
     coupling in `applyPulseRoughnessVariation` / `calculateEngineRoughness` with
     the monotonic sample position.
   - Method `Generate(dst []float64, p pulseWaveformParams)` that, per sample:
     advances `pulsePhase += pulseRate/sampleRate`, derives phase-in-pulse and
     polarity from the accumulator, calls the existing
     `generatePulseValueByGeometry` (Wankel/2-stroke/4-stroke pulse shapes are
     reused unchanged), and applies a **linearly ramped amplitude** from
     `lastAmplitude` to `p.amplitude` across `len(dst)`. Stores `lastAmplitude` at
     the end.
   Reuse existing helpers: `generatePulseWankel/TwoStroke/FourStroke`,
   `calculatePulseAmplitude`, `calculatePulseWaveformParams`. The geometry/overlap/
   roughness math stays; only the phase source changes (per-tick `index` →
   absolute accumulator).

2. **Rewrite `generateEngineHaptic` to refill-and-append**:
   - Compute `need = targetDepth - currentUsed`, clamped to `[0, maxBlock]`, where
     `targetDepth` is the small cushion (e.g. 3 engine frames, the old
     `engineBufferFrames` intent) and `currentUsed` comes from a new
     `Synth.ChannelDepth(channel) int` (thin wrapper over the mixer →
     `AdaptiveBuffer.Used()`).
   - Generate exactly `need` samples via the generator, then **append in normal
     write mode at `writePos` (no offset, no overwrite)** using
     `WriteBuffer`/`WriteChannel`. Because phase is continuous, the seam is clean.
   - `rpm == 0`: append `need` samples of silence to keep the channel fed; freeze
     the phase (do not advance) so the engine restarts in phase.
   - Refill-to-target inherently bounds latency, so `CapChannelDepth` is gone.

3. **Remove obsolete machinery (full cleanup, all verified engine-only):**
   - `AdaptiveBuffer.CapDepth` (`synth_buffer_adaptive.go`), `Mixer.CapChannelDepth`
     (interface in `synth_mixer.go`, `StereoMixer` in `synth_mixer_stereo.go`,
     `MockMixer` in `synth_mixer_mock.go`), `Synthesizer.CapChannelDepth`
     (`synth.go`).
   - `engineBufferFrames` const; `prepareEngineBuffer`, `adjustEngineBufferPolarity`,
     `writeEngineBuffer`, and the engine's `InspectChannelBuffer`/
     `FindSampleZeroCrossing` usage.
   - Delete `synth_buffer_capdepth_test.go` (replaced by Part 3 tests).
   - Keep `FindSampleZeroCrossing`/`SamplePolarity` in `synth_utils.go` (general
     utilities) unless a grep shows they become entirely unused — if so, remove.

## Part 2 — Shared artefact-analysis package

Promote the analysis core out of `tools/audio_cleanup/main.go` (currently
`package main`) into **`app/audio/audioqa`**:
- Move/export `metrics`, `analyse`, `signalRegion`, `zeroRuns`, and the
  step/dropout/discontinuity detectors.
- Add a **tone-independent** analyser (engine output is not a pure sine): peak,
  clipping, NaN/Inf, dropouts (interior zero-runs), and max sample-to-sample step
  / glitch count — i.e. everything except the fundamental-fit SNR, which requires
  a known tone.
- Update `tools/audio_cleanup/main.go` to import `audioqa` (behaviour unchanged;
  the sine-fit SNR path stays for the tool's stages).

## Part 3 — High-level "clean output" tests

New `app/app_haptics_engine_generator_test.go` (generator-level, no full `App`)
plus a channel-level test driving the **real `AdaptiveBuffer`** with a simulated
consumer draining ~1 frame per tick (mirrors the live 30 Hz-generate vs
output-drain relationship). Strongest guard first:

- **A. Phase-continuity equivalence (definitive, threshold-free).** Generate N
  samples (a) in one contiguous call and (b) across many tick-sized appends
  through the real buffer + refill path; assert the readable streams are
  sample-for-sample equal within float epsilon. Any seam artefact = divergence.
- **B. Discontinuity bound at tick seams.** Over M ticks at fixed RPM with the
  simulated drain, assert the max sample-to-sample step ≤ the max step within a
  single uninterrupted block at the same RPM (seams add nothing beyond the
  waveform's own slope). Run at low / mid / **high** RPM.
- **C. RPM sweep.** Sweep idle→redline→idle across many ticks; assert no NaN/Inf,
  no clipping, no dropouts, and that amplitude ramping keeps inter-tick steps
  bounded (catches frequency-change phase jumps and amplitude steps).
- **D. No-underrun / bounded-latency.** With the real drain ratio over a long run,
  assert the channel never returns a short/zero-padded read and buffered depth
  stays bounded — replaces the old `CapDepth` test's intent.
- **E. Full-pipeline smoke (CI-gated).** Feed engine output through
  `audio.NewResamplingSource` + `audio.NewAsyncSource` and assert `audioqa`
  discontinuity/dropout metrics stay in tolerance — a Go-test version of the
  tool's `full` stage with the engine signal.

All tests use `app/audio/audioqa` from Part 2 for the metrics.

## Verification

- `go test ./app/... ./app/synthesizer/... ./app/audio/...` — new tests pass,
  `capdepth` test removed, nothing references the deleted APIs.
- `go build ./... && go vet ./...` and `golangci-lint run` (repo uses it).
- `go run ./tools/audio_cleanup` still PASSes all stages (sine path unchanged);
  `go run ./tools/audio_cleanup -wav out` to eyeball, and optionally a high-RPM
  engine WAV dump for a listening check.
- Manual: build with the portaudio tag and listen at a high-RPM case to confirm
  the clicks are gone.

## Risks / notes

- Amplitude ramping and absolute-phase polarity slightly change exact output vs
  today (intended); the first pulse's polarity may differ — acceptable.
- `ChannelDepth` adds one small method to the `Mixer` interface (offset by the
  three `CapChannelDepth` methods removed).
- Phase carried in cycles is the standard way to keep continuity across a
  frequency change; verify the roughness terms keyed off the old per-tick `index`
  read correctly from the monotonic cursor.
