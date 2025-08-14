package app

import (
	"math"
	"strconv"
	"time"
)

// Engine haptic profiles for different engine layouts
// Smoothness: 0.0 (very rough) to 1.0 (very smooth)
// Base roughness: 0.0 (no roughness) to 1.0 (very rough)
var engineHapticProfiles = map[string]engineHapticProfile{
	"I3": {
		smoothness:    0.3,
		baseRoughness: 0.15,
	},
	"I4": {
		smoothness:    0.6,
		baseRoughness: 0.08,
	},
	"I5": {
		smoothness:    0.7,
		baseRoughness: 0.06,
	},
	"I6": {
		smoothness:    0.9,
		baseRoughness: 0.03,
	},
	"I8": {
		smoothness:    0.95,
		baseRoughness: 0.02,
	},
	"H4": {
		smoothness:    0.8,
		baseRoughness: 0.05,
	},
	"H6": {
		smoothness:    0.92,
		baseRoughness: 0.025,
	},
	"H12": {
		smoothness:    0.98,
		baseRoughness: 0.01,
	},
	"V4": {
		smoothness:    0.5,
		baseRoughness: 0.10,
	},
	"V6": {
		smoothness:    0.85,
		baseRoughness: 0.04,
	},
	"V8": {
		smoothness:    0.95,
		baseRoughness: 0.02,
	},
	"V10": {
		smoothness:    0.88,
		baseRoughness: 0.035,
	},
	"V12": {
		smoothness:    0.98,
		baseRoughness: 0.01,
	},
	"W16": {
		smoothness:    0.99,
		baseRoughness: 0.005,
	},
	"K2": {
		smoothness:    0.85,
		baseRoughness: 0.04,
	},
	"K4": {
		smoothness:    0.75,
		baseRoughness: 0.12,
	},
}

type engineHapticProfile struct {
	smoothness    float64
	baseRoughness float64
}

type engineCharacteristics struct {
	layout          string
	geometry        string
	chambers        int
	firingFrequency float64
	haptics         engineHapticProfile
}

var EngineGeometryMap = map[string]string{
	"H": "horizontally-opposed",
	"I": "inline",
	"K": "Wankel", // kreiskolbenmotor (KKM)
	"V": "V",
	"W": "W",
}

func getEngineCharacteristics(engineLayout string) (engineCharacteristics, error) {
	geometryCode := engineLayout[:1]                // Get first character for geometry
	chambers, err := strconv.Atoi(engineLayout[1:]) // Get remaining characters for chamber count
	if err != nil {
		return engineCharacteristics{}, err // Return error if conversion fails
	}

	hapticProfile, ok := engineHapticProfiles[engineLayout]
	if !ok {
		// Default to I4 haptic profile
		hapticProfile = engineHapticProfile{
			smoothness:    0.6,
			baseRoughness: 0.08,
		}
	}

	return engineCharacteristics{
		layout:          engineLayout,
		geometry:        geometryCode,
		chambers:        chambers,
		firingFrequency: getEngineFiringFrequency(geometryCode, chambers),
		haptics:         hapticProfile,
	}, nil
}

func getEngineFiringFrequency(geometry string, chambers int) float64 {
	// Wankel engines fire 3 times per rotor per revolution
	if geometry == "K" {
		return (float64(chambers) * 3.0) / 60.0
	}

	// Four stroke engines fire once per cylinder every 2 revolutions
	return (float64(chambers) / 2.0) / 60.0
}

func (a *App) generateEngineHaptic() {
	if a.config.Synthesizer.EngineGain == -60 {
		return
	}

	rpm := float64(a.gtClient.Telemetry.EngineRPM())

	// Cache last known RPM and timestamp for fallback when telemetry is unavailable
	currentTime := time.Now()
	if rpm > 0 {
		// Update last known good RPM and timestamp
		a.state.lastKnownRPM = rpm
		a.state.lastRPMTime = currentTime
	} else if a.state.lastKnownRPM > 0 && currentTime.Sub(a.state.lastRPMTime) < 1000*time.Millisecond {
		// Use cached RPM for up to 1 second if telemetry is unavailable
		rpm = a.state.lastKnownRPM
	}

	// Generate haptics every 6 frames to reduce computational load
	if a.state.current.seq%6 != 0 {
		return
	}

	// No haptics when engine is not running
	if rpm == 0 {
		// Use 6 frames worth of buffer size for consistency
		// sampleRate := float64(a.config.Synthesizer.SampleRateHz)
		// samplesPerBuffer := int(sampleRate / 10.0) // 60 FPS / 6 frames = 10 Hz update rate
		// a.synth.WriteBuffer("engine", make([]float64, samplesPerBuffer))

		return
	}

	// Use the vehicle's actual rev limiter maximum RPM from telemetry
	revLimit := float64(a.gtClient.Telemetry.EngineRPMLight().Max)

	// If rev limiter data is not available, fall back to a reasonable default
	if revLimit <= 0 {
		revLimit = 8000.0
	}

	var firingFrequency float64
	// For high-firing engines like Wankels, use a completely different approach
	// Map RPM to a much lower frequency range for perceptible intervals
	if a.vehicle.engine.geometry == "K" {
		// Create a dramatic RPM-dependent frequency curve for Wankels
		// Idle: ~8Hz (125ms intervals), High RPM: ~80Hz (12.5ms intervals)
		rpmFactor := rpm / revLimit
		firingFrequency = 8.0 + (rpmFactor * 72.0) // 8Hz to 80Hz range
	} else {
		// For regular engines, also reduce frequency range for better perception
		firingFrequency = (rpm * a.vehicle.engine.firingFrequency) / 2.0 // Halve frequency for all engines
	}

	// Clamp firing frequency to wider range for more pulses at high RPM
	if firingFrequency < 6.0 {
		firingFrequency = 6.0 // 167ms intervals at minimum
	} else if firingFrequency > 150.0 {
		firingFrequency = 150.0 // 6.67ms intervals at maximum (increased from 100Hz)
	}

	// Normalize RPM from 0 to max
	rpmNormalized := rpm / revLimit
	if rpmNormalized < 0 {
		rpmNormalized = 0
	} else if rpmNormalized > 1 {
		rpmNormalized = 1
	}

	// Generate amplitude with louder idle feedback and linear 30% volume reduction at max RPM
	// Start with higher base amplitude for idle/low RPM feedback
	baseAmplitude := 0.7 + (rpmNormalized * 0.1) // Range: 0.7 to 0.8

	// Apply 30% linear volume reduction from idle to max RPM
	// At idle (0 RPM): 1.0 factor (full volume)
	// At max RPM: 0.7 factor (30% reduction)
	volumeReductionFactor := 1.0 - (rpmNormalized * 0.3)
	baseAmplitude *= volumeReductionFactor

	// Add engine-specific roughness variation
	// Roughness decreases with RPM and smoothness characteristic
	var engineRoughness float64
	if rpm <= 2400.0 {
		// Low RPM roughness varies by engine type
		roughnessPhase := float64(a.state.current.seq) * 0.005
		roughnessIntensity := a.vehicle.engine.haptics.baseRoughness * (1.0 - a.vehicle.engine.haptics.smoothness*0.5) // Less smooth engines are rougher
		engineRoughness = math.Sin(roughnessPhase)*roughnessIntensity + math.Sin(roughnessPhase*1.7)*roughnessIntensity*0.5

		// Reduce roughness as RPM increases (engines smooth out)
		rpmSmoothingFactor := rpm / 2400.0
		engineRoughness *= (1.0 - rpmSmoothingFactor*a.vehicle.engine.haptics.smoothness)
	} else {
		// High RPM: roughness based on engine smoothness characteristic
		if a.vehicle.engine.haptics.smoothness < 0.9 {
			roughnessPhase := float64(a.state.current.seq) * 0.002
			highRpmRoughness := (1.0 - a.vehicle.engine.haptics.smoothness) * 0.02 // Less smooth engines retain some roughness
			engineRoughness = math.Sin(roughnessPhase) * highRpmRoughness
		} else {
			engineRoughness = 0.0 // Very smooth engines have no roughness at high RPM
		}
	}

	amplitude := baseAmplitude + (engineRoughness * rpmNormalized * 0.1)

	// Ensure amplitude stays within bounds
	if amplitude < 0.2 {
		amplitude = 0.2
	} else if amplitude > 0.9 {
		amplitude = 0.9
	}

	// Generate engine vibration waveform for 6 frames
	sampleRate := float64(a.config.Synthesizer.SampleRateHz)
	samplesPerBuffer := int(sampleRate / 10.0) // 60 FPS / 6 frames = 10 Hz update rate
	engineBuffer := make([]float64, samplesPerBuffer)

	// Normal engine pulse generation (rev limiter already checked above)
	for i := range engineBuffer {
		// Use realistic firing frequency for pulse timing
		pulsesPerSecond := firingFrequency
		samplesPerPulse := sampleRate / pulsesPerSecond

		// Create pulses based on engine firing events
		pulsePosition := float64(i) / samplesPerPulse
		pulseFraction := pulsePosition - math.Floor(pulsePosition) // 0.0 to 1.0 within each pulse cycle

		var pulseValue float64

		// Detect pulse trigger point (beginning of each cycle)
		currentPulseIndex := int(math.Floor(pulsePosition))
		lastPulseIndex := int(math.Floor((float64(i - 1)) / samplesPerPulse))

		// Check if we've crossed into a new pulse cycle
		pulseTriggered := (i > 0) && (currentPulseIndex != lastPulseIndex)

		// Toggle polarity when a new pulse is triggered
		if pulseTriggered {
			a.state.enginePulsePolarity = !a.state.enginePulsePolarity
		}

		// Always generate pulse value based on position within current pulse cycle
		pulsePhase := pulseFraction // 0.0 to 1.0 within the pulse cycle

		// Create pulses with increasing width and overlap at higher RPM
		// Pulse width gets much wider at higher RPM, allowing up to 25% overlap
		rpmFactor := rpm / revLimit
		pulseWidth := 0.25 + (rpmFactor * 0.50) // 25% width at idle, 75% width at high RPM (allows 25% overlap)

		if pulsePhase < pulseWidth {
			// Inside the pulse - create a sharp, distinct pulse
			pulsePhaseNormalized := pulsePhase / pulseWidth // 0.0 to 1.0 within pulse width

			if pulsePhaseNormalized < 0.3 {
				// Quick attack (30% of pulse width)
				attackPhase := pulsePhaseNormalized / 0.3
				pulseValue = math.Sin(attackPhase * math.Pi / 2)
			} else {
				// Quick decay (70% of pulse width)
				decayPhase := (pulsePhaseNormalized - 0.3) / 0.7
				decayRate := 4.0 + (1.0-a.vehicle.engine.haptics.smoothness)*2.0 // Sharp decay for distinctness
				pulseValue = math.Exp(-decayPhase * decayRate)
			}

			// Apply polarity - alternating positive and negative pulses
			if !a.state.enginePulsePolarity {
				pulseValue = -pulseValue
			}

			// Add per-pulse roughness variation based on engine characteristics
			if rpm <= 2400.0 && a.vehicle.engine.haptics.baseRoughness > 0.02 {
				roughnessPhase := float64(a.state.current.seq+uint32(i)) * 0.0005
				roughnessVariation := 1.0 + (math.Sin(roughnessPhase) * a.vehicle.engine.haptics.baseRoughness * 0.3)
				pulseValue *= roughnessVariation
			}
		} else {
			// Outside the pulse - complete silence for clear gaps
			pulseValue = 0.0
		}

		// Ensure the signal stays within bounds
		if pulseValue > 1.0 {
			pulseValue = 1.0
		} else if pulseValue < -1.0 {
			pulseValue = -1.0
		}

		engineBuffer[i] = amplitude * pulseValue
	}

	a.synth.WriteBuffer("engine", engineBuffer)
}
