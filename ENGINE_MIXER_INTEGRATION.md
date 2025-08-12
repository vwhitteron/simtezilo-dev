# Engine Haptics Mixer Channel Integration

## Current Implementation

The engine haptics are already properly integrated with the mixer channel system. Here's how it works:

### Architecture Flow

1. **Engine Haptic Generation** (`generateEngineHaptic()`)
   - Reads RPM from telemetry
   - Maps RPM to frequency and base amplitude
   - Generates complex waveform with harmonics
   - Writes to "engine" channel via `a.synth.WriteBuffer("engine", engineBuffer)`

2. **Mixer Channel Processing** (`Buffer.Write()`)
   - Receives engine samples for the "engine" channel
   - Gets engine channel gain from mixer: `mixer.GetChannelPowerRatio("engine")`
   - Applies channel gain as magnitude: `inputSample := inSamples[i] * magnitude`
   - Mixes with other channels using the selected algorithm

3. **Real-time Gain Control**
   - Engine gain setting (`config.EngineGain`) is linked to "engine" mixer channel
   - `Mixer.watchForConfigChanges()` monitors config changes
   - GUI changes to engine volume immediately update the mixer channel gain
   - No restart required - changes take effect in real-time

### Channel Configuration

The engine channel is set up in `synth.go`:
```go
_ = mixer.AddChannel("engine", &opts.Config.EngineGain)
```

This creates a direct link between:
- Configuration setting: `config.Synthesizer.EngineGain`
- Mixer channel: "engine"
- GUI control: "eVol" menu option

### Separation of Concerns

- **`EngineAmplitudeScale`**: Controls waveform generation strength (0.0-1.0 multiplier)
- **`EngineGain`**: Controls final output volume (-60 to 0 dB via mixer channel)

This separation allows:
- Waveform characteristics to be tuned independently
- Volume control to use standard audio gain/decibel scaling
- Consistent behavior with other haptic channels (chassis, transmission)

### Benefits

✅ **Real-time control**: Engine volume adjusts immediately via GUI
✅ **Consistent scaling**: Uses same decibel system as other channels  
✅ **Proper mixing**: Engine haptics mix correctly with chassis/transmission
✅ **Auto gain control**: Built-in limiting prevents clipping
✅ **Config persistence**: Settings automatically saved and restored

## Verification

The system is working correctly as implemented. The engine haptics:
- Generate RPM-based waveforms with proper frequency mapping
- Send audio through dedicated "engine" mixer channel
- Apply engine gain setting through mixer channel system
- Mix with other haptic channels using selected algorithm
- Respond to real-time GUI volume changes

No additional changes are needed - the engine haptics are already properly routed through the mixer channel system.
