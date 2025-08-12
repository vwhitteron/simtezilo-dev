# Engine Vibration Haptics

This update adds engine vibration haptics to Simtezilo based on vehicle RPM data from Gran Turismo 7.

## Features

- **RPM-based vibration**: Engine vibration frequency and amplitude scale with RPM
- **Simple activation**: Engine vibrations are active whenever RPM > 0 (engine running)
- **Realistic engine simulation**: Uses multiple harmonics and slight irregularities to simulate engine feel
- **Independent volume control**: Separate gain control for engine vibrations

## Configuration

The following configuration options have been added to the `[synthesizer]` section:

```toml
[synthesizer]
# Engine vibration settings
engineGain = -8.0              # Volume of engine vibration (-60 to 0 dB)
engineFrequencyMin = 15.0      # Low-frequency vibration (Hz) at idle RPM
engineFrequencyMax = 45.0      # High-frequency vibration (Hz) at redline RPM
engineAmplitudeScale = 0.3     # Overall strength of engine vibration (0.0-1.0)
```

### Configuration Parameters

- **`engineGain`**: Controls the volume of engine vibrations in decibels (-60 to 0 dB). Lower values = quieter.
- **`engineFrequencyMin`**: The vibration frequency (in Hz) at idle RPM (~800 RPM). Typically 15-20 Hz for realistic engine feel.
- **`engineFrequencyMax`**: The vibration frequency (in Hz) at redline RPM (~8000 RPM). Typically 40-50 Hz for high-rev engine feel.
- **`engineAmplitudeScale`**: Overall strength multiplier for engine vibrations (0.0 to 1.0). Adjust for comfort.

## How It Works

1. **RPM Monitoring**: The system continuously reads engine RPM from Gran Turismo 7 telemetry
2. **Simple Activation**: Engine vibrations are active whenever RPM > 0 (engine is running)
3. **Frequency Mapping**: RPM is mapped linearly to a frequency range using a typical RPM range (800-8000 RPM)
4. **Amplitude Scaling**: Vibration strength increases with RPM, modified by the amplitude scale setting
5. **Waveform Generation**: Multiple sine waves (fundamental + harmonics) create a complex engine-like vibration
6. **Mixing**: Engine vibrations are mixed with existing chassis and transmission haptics

## Integration

The engine vibration system integrates seamlessly with existing haptics:

- **Engine channel**: Uses a dedicated "engine" mixer channel
- **Real-time processing**: Updates at 60 FPS alongside existing haptics
- **Independent control**: Can be adjusted independently of chassis and transmission haptics
- **Web monitoring**: Engine RPM and vibration status are exposed to the web UI

## Tuning Tips

- Start with `engineGain = -8.0` and adjust to taste
- For street cars, use lower amplitude scale (0.2-0.3)
- For race cars, you can use higher amplitude scale (0.3-0.5)
- Adjust frequency range based on your haptic device capabilities
- Monitor the web UI to see engine vibration activation in real-time

## Technical Details

- Engine vibrations use multiple harmonics (fundamental, 2nd harmonic, sub-harmonic)
- Slight randomization adds realistic engine roughness
- Vibrations automatically stop when RPM = 0 (engine off)
- RPM normalization uses a typical range (800-8000 RPM) for frequency scaling
- Compatible with all existing mixer algorithms and EQ settings
