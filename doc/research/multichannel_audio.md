# Multichannel Audio Migration Study

## Goals
- Enable more than two haptic output channels (USB DACs, multi-channel amplifiers).
- Route pit radio audio independently (e.g., Bluetooth headsets).
- Allow configurable audio hardware selection from the Web UI.
- Maintain portable deployment across macOS, Linux (desktop and Raspberry Pi OS), and Windows.

## Current State (beep)
| Capability | Status |
|------------|--------|
| Output channels | Hard-coded stereo via `[2]float64` samples |
| Device selection | Not supported; single default device only |
| Sample format | Float64 pairs (non-interleaved) |
| Resampling | Built-in `beep.Resample` |
| Dependencies | Pure Go |

## PortAudio Overview
| Feature | Notes |
|---------|-------|
| Channel count | Arbitrary channel counts per stream |
| Device selection | Enumerate, choose, and test individual devices |
| Multiple streams | Independent streams per device (different sample rates/latencies) |
| Callback model | Low-latency callbacks with interleaved float32 buffers |
| Platform support | macOS (CoreAudio), Linux (ALSA/JACK/OSS/Pulse via plugins), Windows (WASAPI/DirectSound/ASIO/MME) |
| Raspberry Pi | Works via ALSA; PulseAudio/PipeWire needed for Bluetooth |
| Build requirements | Requires native PortAudio library + CGO |

### Windows Support
- PortAudio officially supports Windows. The Go bindings work with:
  - PortAudio installed via MSYS2 (`pacman -S mingw-w64-x86_64-portaudio`).
  - PortAudio installed via vcpkg (`vcpkg install portaudio`).
- Requires CGO with MinGW-w64 or MSVC toolchain.

### Linux Audio Backends
| Backend | Description | Notes |
|---------|-------------|-------|
| ALSA | Default kernel audio stack | Best for USB DAC haptics; low latency |
| JACK | Pro-audio routing daemon | Optional; niche |
| OSS | Legacy interface | Rarely used |
| PulseAudio | Desktop audio mixer | Needed for Bluetooth A2DP |
| PipeWire | Modern Pulse+JACK fusion | Increasingly default (Fedora, new Ubuntu) |

## Multi-Device Output
PortAudio allows separate streams per device:
- **Haptics** → USB DAC (multi-channel, low latency).
- **Pit Radio** → Bluetooth headset (stereo, higher latency acceptable).

Example flow:
1. Enumerate devices, identify DAC or headset by name/backend.
2. Open stream A on DAC with 4 channels at 48 kHz, 10 ms buffer.
3. Open stream B on headset with 2 channels at 48 kHz, 20 ms buffer.
4. Start both streams; feed data independently.

## Proposed Audio Architecture
```
app/audio/
  manager.go          // PortAudio device enumeration, stream management
  system_linux.go     // ALSA & PulseAudio helpers (aplay, pactl, config files)
  test.go             // Play test tones on selected device/channel

app/synthesizer/
  streamer.go         // Multi-channel interleaved float32 output
  resampler.go        // Replacement for beep.Resample

app/config/
  audio.go            // Typed getters/setters for audio config
```

### Audio Manager Responsibilities
- Initialize/terminate PortAudio.
- Expose `ListDevices` returning:
  - Name/ID, max channels, backend, default sample rate, latency hints.
- Support test-tone playback per device/channel.
- Manage persistent streams (start/stop) for haptics and pit radio.
- Detect PulseAudio/ALSA availability, surface status to UI.

### Linux Integration Helpers
- Parse `aplay -l` for ALSA cards/devices.
- Parse `pactl list short sinks` for PulseAudio sinks.
- Set default PulseAudio sink (`pactl set-default-sink`).
- Generate `.asoundrc` or `/etc/asound.conf` snippets for dedicated ALSA PCM definitions.

## Configuration Model
```yaml
audio:
  backend: portaudio
  haptics:
    device: "USB Audio DAC"
    channels: 4
    sample_rate: 48000
    latency_ms: 10
  pit_radio:
    device: "Bluetooth Headset"
    sample_rate: 48000
```

Provide defaults with graceful fallback to the system default device when a configured target is unavailable.

## Web UI Support
### REST Endpoints
- `GET /api/audio/devices` → Enumerated devices, backend availability, feature flags.
- `GET /api/audio/config` → Current persisted audio configuration.
- `PUT /api/audio/config` → Update configuration and restart relevant streams.
- `POST /api/audio/test` → Play a short test tone.

### Front-End Concepts
- Device drop-downs showing name, backend, channel count.
- Channel count selector for haptics (only show supported counts).
- Latency slider (maps to frames per buffer).
- Test buttons per device/channel.
- Status indicators (connected/disconnected, backend running, fallback in use).

## Migration Plan
1. **Abstractions**
   - Replace beep `Streamer` with interleaved float32 buffer streaming interface.
   - Introduce `AudioOutput` interface to decouple synth from PortAudio specifics.
2. **Resampling**
   - Implement or adopt standalone resampler (e.g., linear interpolation or `github.com/zaf/resample`).
3. **MP3 Decoding**
   - Replace `beep/mp3` with `github.com/hajimehoshi/go-mp3` (already a dependency).
4. **Synth Update**
   - Make `NumOutputChannels` dynamic, driven by configuration.
5. **Stream Management**
   - Implement stream lifecycle (start/stop/restart) tied to config changes.
6. **Web UI**
   - Add new audio configuration panel using existing API patterns.
7. **Platform Scripts (pi-gen)**
   - Ensure PortAudio library installation in build stages.
   - Optionally install PulseAudio/BlueZ components for Bluetooth support.
8. **Testing**
   - Unit tests for configuration parsing and stream setup.
   - Integration tests on Raspberry Pi hardware for multi-channel output.
   - Manual verification for Bluetooth headset routing.

## Risks and Mitigations
| Risk | Impact | Mitigation |
|------|--------|------------|
| CGO dependency increases build complexity | Medium | Provide cross-compilation instructions, pre-built binaries, or use build containers |
| Device hot-plug | Medium | Implement polling or udev notifications; show status in UI |
| Bluetooth reliability on Pi | Medium | Document required packages/services; add watchdog to restart PulseAudio if needed |
| Latency tuning | Low | Expose buffer size controls; provide sane defaults per backend |
| Resampler accuracy | Low | Validate with unit tests and audio inspection |

## Alternative: Oto
`github.com/ebitengine/oto` is a pure Go alternative. Benefits: no CGO on Windows. Limitations:
- Fewer backend controls.
- Multi-device support requires separate context per device; not as feature-rich as PortAudio.
- Still viable fallback if PortAudio builds become too heavy.

## Next Steps
1. Prototype `audio.Manager` with device enumeration and test-tone playback.
2. Replace `beep` usage in synthesizer with new interleaved streamer.
3. Implement dual-stream output (haptics + pit radio) using PortAudio.
4. Hook configuration into Web UI and config files.
5. Update pi-gen build scripts to include PortAudio and PulseAudio components.
