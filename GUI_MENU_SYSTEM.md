# GUI Menu System Settings

This document describes the GUI menu system settings available in Simtezilo.

## Menu Navigation

The GUI uses a page-based menu system where users can navigate through different settings using buttons or controls. The menu pages cycle through the following settings:

## Available Settings

| Menu Code | Display Name | Description | Value Format |
|-----------|--------------|-------------|--------------|
| `vol` | Master Gain | Overall volume/gain control | `-XX.XX dB` |
| `cVol` | Chassis Vol | Chassis haptics volume | `-XX.XX` |
| `tVol` | Trans Vol | Transmission haptics volume | `-XX.XX` |
| `eVol` | Engine Vol | Engine vibration volume | `-XX.XX` |
| `vCurve` | V Curve | Jerk response curve | `XXX` (0-1000) |
| `vSat` | V Sat | Jerk saturation/max value | `XX` (1-100) |
| `fCurve` | F Curve | Snap response curve | `XXX` (0-1000) |
| `fSat` | F Sat | Snap saturation/max value | `XX` (1-100) |
| `fMin` | F Min | Minimum frequency (Hz) | `XX` |
| `fMax` | F Max | Maximum frequency (Hz) | `XX` |
| `tCurve` | T Curve | Transmission response curve | `XXX` (0-1000) |
| `tSat` | T Sat | Transmission G-force saturation | `X.X` |
| `mix` | Mix | Mixer algorithm | `sum` or `rss` |
| `lang` | Language | UI language | `en` or `jp` |

## Engine Volume (eVol) - New Feature

The Engine Volume setting controls the volume of the new engine vibration haptics:

- **Range**: -60.00 dB to 0.00 dB
- **Default**: -8.00 dB
- **Function**: Controls the amplitude of RPM-based engine vibrations
- **Adjustment**: Incremental changes using config.GainIncrement (0.25 dB steps)

### Usage

- **Increase**: Raise engine vibration volume
- **Decrease**: Lower engine vibration volume
- **Display**: Shows current value in decibels with 2 decimal places

The engine volume setting works independently of other volume controls, allowing users to fine-tune the engine haptic feedback to their preference while maintaining other haptic settings.

## Localization

The menu system supports multiple languages:

- **English**: "Engine Vol"
- **Japanese**: "エンジン音量" (Engine Volume)

## Technical Implementation

- Settings are stored in the main configuration and automatically saved
- Changes take effect immediately through the mixer's config watch system
- All volume controls use decibel scaling
- Engine volume integrates with the existing haptics mixer architecture
