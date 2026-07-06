# Audio pipeline allocation cleanup

Eliminate per-frame and per-event heap allocations in the audio mix path to
reduce GC pressure on the real-time audio callback thread. Allocations on the
callback thread risk GC-induced pauses, which manifest as audible dropouts /
jitter — that is the primary justification, ahead of raw throughput.

`MixToMaster` (`app/synthesizer/synth_mixer_stereo.go:457`) runs once per audio
block on the callback thread (~94×/sec at 512 frames / 48 kHz). Anything it
allocates per call multiplies into hundreds–thousands of small allocations per
second.

The codebase already endorses the reuse approach: `Streamer.ensureBufs`
(`app/synthesizer/synth_output.go:182`) and `s.settings`
(`app/synthesizer/synth_output.go:124`) grow reusable buffers on demand. New
work should mirror that pattern.

## Conventions

- Reusable scratch fields live on `StereoMixer` and are **audio-callback-thread
  only** — comment them as such. They must be grown on demand exactly like
  `ensureBufs` (grow when `cap` is exceeded, reslice to required length).
- These fields become a load-bearing invariant: `MixToMaster` must never run
  concurrently with itself. True today (single callback), but now undocumented
  assumptions become real. Document them.
- Verify after each phase: `go build ./...`, `go test ./app/synthesizer/...`,
  and the glitch tests (`app/synthesizer/synth_glitch_test.go`) as the safety
  net for buffer-behaviour changes.

---

## Phase 1 — Safe wins (trivial / low risk) — ✅ COMPLETE

### [x] Item 7: hoist `hidKeyAction` map literal
- **Done:** promoted the lookup map to a package-level `var hidKeyMap`
  (`app/framecapture.go`); `hidKeyAction` now just indexes it. No more
  per-keypress heap allocation.
- **Risk:** none (read-only lookup map).
- Verified: `go build ./...` and `go test ./app/...` pass.

### [x] Item 5a: replace `fmt.Sprintf` with `strconv.Itoa` in helpers
- **Files:** `app/synthesizer/synth.go:40` (`OutputChannelName`),
  `app/synthesizer/synth.go:45` (`ChassisChannelName`).
- **Done:** helpers now use `prefix + strconv.Itoa(n)`. This removes the `fmt`
  formatter overhead and `interface{}` boxing (~2 allocs/call → ~1 alloc/call,
  ~5–10× faster). `strconv.Itoa` is allocation-free for n < 100; the remaining
  alloc is the `+` concatenation producing the result string.

### [x] Item 5b: precompute channel name strings for the hot loops
- **Done:** added `outputChannelNames` / `chassisChannelNames []string` fields
  on `StereoMixer`, populated once at construction via `buildChannelNames`
  (`synth.go`). Hot loops now index the slices instead of allocating:
  `MixToMaster` (chassis + output writes), `mixCalibratorOutput` output writes,
  and `Streamer.getOutputChannels` / `readOutputBuffers`.
- Added `OutputChannelName(ch int) string` to the `Mixer` interface (and
  `MockMixer`) so the `Streamer` (which holds the interface, not `*StereoMixer`)
  can read precomputed names. The package-level `OutputChannelName` /
  `ChassisChannelName` helpers remain for non-hot callers.
- Channel count is fixed at construction (`numOutputChannels`), so no runtime
  rebuild is needed; loops bound by `numOutputChannels` keep indexing safe.
- Verified: `go build ./...` and `go test ./app/synthesizer/...` pass.

---

## Phase 2 — Shared mixer scratch (medium risk) — ✅ COMPLETE

Fixed Items 1, 2, and 4 together using shared preallocated fields on
`StereoMixer`, grown on demand via free helpers `growFloats` /
`growChannelScratch` (mirroring `ensureBufs`). Added scratch fields:
`mixOutSamples`, `mixChannelSamples`, `mixPeaks`, `engineWorkScratch`,
`calibratorEqAmplitudes` — documented as **audio-callback-thread only** with a
non-reentrancy note on `MixToMaster`.

### [x] Item 1: `MixToMaster` scratch buffers
- **Done:** `outSamples`/`channelSamples`/`peaks` and the per-channel slices now
  reuse `mixOutSamples` / `mixChannelSamples` / `mixPeaks`. The accumulators
  (`channelSamples`, `peaks`) are zeroed each call before mixing.

### [x] Item 2: `processEngineSamplesMulti` work buffer
- **Done:** `outSamplesWork` reuses `engineWorkScratch`. Cleared before use to
  preserve the original zero-the-tail behaviour on short (underrun-truncated)
  engine reads, since the copy-back overwrites all of `outSamples`.

### [x] Item 4: `mixCalibratorOutput` scratch buffers
- **Done:** `eqAmplitudes` reuses `calibratorEqAmplitudes`; the calibrator's
  per-channel buffers reuse `mixChannelSamples`. Both are fully overwritten per
  call, so no clear is needed.

- Verified: `go build ./...`, `go test ./app/synthesizer/...`, `go test -race`,
  and `golangci-lint` (no new issues; baseline of 14 unchanged).

**Risk (medium):** reusing fields removes the inherent per-call isolation. The
scratch is touched only on the single audio-callback thread; `MixToMaster` must
never run concurrently with itself (documented). Dimensions are grown/resliced
each call against `numOutputChannels`.

---

## Phase 3 — Buffer interface change (high risk, do alone) — ✅ COMPLETE

### [x] Item 3: `AdaptiveBuffer.readFromBuffer` return slice
- **Done:** the read API is now a single caller-fills form
  `Read(dst []float64) int` on the `Buffer` interface and `AdaptiveBuffer` — no
  parallel allocating variant. The read core lives in
  `readIntoBuffer(dst, consume)`, which copies into the caller's slice and
  returns the count written; `Read` delegates to it, so underrun semantics live
  in one place. (Initially landed as a separate `ReadInto` alongside the
  allocating `Read`, then consolidated: the allocating versions were removed and
  `ReadInto` renamed to `Read` so prod and tests drive one mechanism.)
- `MixerChannel.Read` and `StereoMixer.ReadChannel` are likewise the caller-fills
  form (`ReadChannel(name string, dst []float64) int` on the `Mixer` interface
  and `MockMixer`). The mute switch was extracted into a free
  `channelMuted(cfg, name)` helper. `Synthesizer.ReadBuffer(dst []float64) int`
  follows suit; its only caller (`haptic_capture.go`) reads into a `readBuf`
  allocated once outside the capture loop.
- **Hot-path callers pass preallocated scratch:**
  - `MixToMaster` chassis + transmission reads and `mixEngineChannelMulti`'s
    engine read reuse a callback-thread-only `mixReadScratch channelBuffer` on
    `StereoMixer`, grown on demand in `MixToMaster`. Each read fully consumes the
    scratch before the next, so a single buffer is safe; documented in the
    `MixToMaster` non-reentrancy note. (`mixEngineChannelMulti` dropped its now
    unused `length` param.)
  - `Streamer.readOutputBuffers` reads each output channel directly into its
    reusable per-channel `s.bufs[ch]` buffer and applies gain in place.
- All callers respect the returned count (`count`/`n`) rather than `len(dst)`,
  preserving underrun-truncation behaviour (zeroing the unwritten tail).
- Tests drive the same `Read(dst) int` path via a small `readN` allocate-and-
  return helper (one per test package), so prod and tests share the read
  mechanism rather than exercising a separate allocating API.
- Verified: `go build ./...`, `go test ./...`, `go test -race`, the glitch tests
  (`TestAdaptiveBuffer_RampPassThrough`, `_UnderrunInsertsZeros`,
  `_UpstreamStarvation_Scenarios`), and `golangci-lint` with a **cleared cache**
  (true baseline: 14 issues in `app/synthesizer/...`, 15 in `app/` — both
  unchanged; an earlier "14" for `app/` was a stale-cache artifact).

**Risk (high) — mitigations applied:** scratch is never shared between two live
reads (sequential within `MixToMaster`; per-channel in the Streamer) and never
held across reads. Underrun truncation is honoured via the returned count.

---

## Phase 4 — Effect copy + scale contract (high risk, do last) — ✅ COMPLETE

### [x] Item 6: remove defensive effect sample copy
- **Done:** added a **non-mutating** scaled write path and removed the
  per-`GetSample` defensive copy (`synth_effects.go`). `GetSample` now returns
  the cached `codec.PCMFloat64` directly.
- **New API:** `AdaptiveBuffer.WriteScaled(samples, magnitude, offset, overwrite)`
  (also on the `Buffer` interface) stages the magnitude-scaled copy in a reusable,
  grown-on-demand `scaleScratch` buffer guarded by `b.mu`, so the caller's slice
  is never mutated and no per-write allocation occurs. `magnitude == 1.0`
  delegates to `Write` (no scaling). `MixerChannel.WriteScaled` delegates to it.
- **Routing:** `StereoMixer.WriteChannel` now calls `MixerChannel.WriteScaled`
  (non-mutating) instead of `MixerChannel.Write`. This covers `PlayEffect` (the
  cache-corrupting path) plus `WriteBuffer`/`OverwriteBuffer`; all hand in
  buffers they own or share, and none rely on post-write mutation.
- **Contract preserved:** `MixerChannel.Write` (in-place `ScaleSamples`) is
  unchanged and still used by `MixToMaster`'s per-channel output writes
  (`synth_mixer_stereo.go:611/965/975`), all at magnitude 1.0.
- **Read-only consumers verified:** the other `GetSample` callers
  (`app_recorder.go`, `discord.go`) only `ToDCA()`-encode the sample (read-only),
  so sharing the cached slice is safe.
- **Regression test:** `synth_effects_test.go` —
  `TestWriteScaled_DoesNotMutateInput` (write path leaves input untouched) and
  `TestGetSample_StableAcrossRepeatedWrites` (cached effect sample is unchanged
  after repeated scaled writes).

### [x] Bonus: short-circuit `ScaleSamples(magnitude == 1.0)`
- **Done:** `ScaleSamples` (`synth_utils.go`) returns early when
  `magnitude == 1.0`, skipping the full-buffer multiply that `MixToMaster` and
  the calibrator would otherwise do every frame.

- Verified: `go build ./...`, `go test ./...`, `go test -race
  ./app/synthesizer/...`, the glitch tests, and `golangci-lint` with a cleared
  cache (synth baseline 14 and `app/` baseline 15 both unchanged).

**Risk (high for Item 6) — mitigations applied:** the cached sample is shared
rather than copied; the only mutating write path (`MixerChannel.Write`) is no
longer reachable from effect playback, and the non-mutating path stages scaling
in its own scratch. Tests pin effect volume constant across repeated plays.

---

## Tracking

| Phase | Item | Status |
|-------|------|--------|
| 1 | 7 — hidKeyAction map | ☑ |
| 1 | 5a — strconv.Itoa helpers | ☑ |
| 1 | 5b — precompute names (hot loops) | ☑ |
| 2 | 1 — MixToMaster scratch | ☑ |
| 2 | 2 — engine work buffer | ☑ |
| 2 | 4 — calibrator scratch | ☑ |
| 3 | 3 — Buffer.ReadInto | ☑ |
| 4 | 6 — effect copy / scale contract | ☑ |
| 4 | bonus — magnitude==1.0 short-circuit | ☑ |
