package app

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
)

// generateBalancedWaveform creates engine haptic waveforms based on primary and secondary balance characteristics
// with consideration for the 160Hz low-pass filter of the output device
func (a *App) generateBalancedWaveform(rpm float64, engineRoughness float64, engineBuffer *[]float64) {
	sampleRate := float64(a.synth.GetSampleRate())
	rpmPercent := rpm / float64(a.vehicle.revLimit)

	// Calculate base firing frequency and harmonics
	baseFiringRate := rpm * a.vehicle.engine.firingFrequency * a.vehicle.engine.haptics.PulseScale

	// Primary balance affects fundamental firing frequency
	primaryFreq := baseFiringRate

	// Secondary balance creates vibrations at intervals related to primary balance
	// Secondary imbalance creates vibrations at a slightly higher frequency (not double)
	// This represents reciprocating mass imbalance vibrations
	secondaryFreq := baseFiringRate * 1.3 // More subtle secondary frequency

	// Account for 160Hz low-pass filter - avoid generating significant content above this
	maxUsableFreq := 160.0
	if primaryFreq > maxUsableFreq {
		primaryFreq = maxUsableFreq
	}
	if secondaryFreq > maxUsableFreq {
		secondaryFreq = maxUsableFreq * 0.8 // Keep secondary below filter cutoff
	}

	throttlePercent := float64(a.gtClient.Telemetry.ThrottleOutputPercent()) / 100
	throttlePercent, _ = signal.LimitWindow(throttlePercent, 0.0, 1.0)

	var gainOffset float64
	var amplitudeScale float64
	switch a.vehicle.vehicleType {
	case "race":
		gainOffset = 0.0
		amplitudeScale = 0.3
	case "tuned":
		gainOffset = -3.0
		amplitudeScale = 0.2
	default: // "street" or other types
		gainOffset = -4.75
		amplitudeScale = 0.01
	}

	// Generate amplitude aiming for signal max but respecting gain settings
	baseAmplitude := 0.9 + (throttlePercent * amplitudeScale)
	rpmNormalized, _ := signal.LimitWindow(rpmPercent, 0.0, 1.0)

	// Apply full gain control - this should be able to reduce volume significantly
	gainAdjust := synth.GainToPowerRatio(a.vehicle.engine.haptics.Gain + gainOffset)

	// Calculate boost to reach signal max at 0dB gain, but scaled for proper gain response
	// At 0dB gain (gainAdjust = 1.0), we want amplitude near 1.0
	// At -3dB gain (gainAdjust ≈ 0.71), we want amplitude around 0.5
	// The boost should be calculated to achieve signal max without clipping
	targetMaxAmplitude := 0.95 // Slightly below 1.0 to avoid clipping
	amplitudeBoost := targetMaxAmplitude / (baseAmplitude + (engineRoughness * rpmNormalized * 0.2))

	amplitude := (baseAmplitude + (engineRoughness * rpmNormalized * 0.2)) * gainAdjust * amplitudeBoost
	amplitude, _ = signal.LimitWindow(amplitude, 0, 1)

	// Balance contribution factors based on RPM
	// Secondary balance dominates at low RPM, primary balance dominates at high RPM
	lowRpmThreshold := 0.3  // 30% of rev limit
	highRpmThreshold := 0.7 // 70% of rev limit

	var secondaryContribution, primaryContribution float64
	if rpmNormalized < lowRpmThreshold {
		// Low RPM: secondary balance dominates (much stronger contribution)
		secondaryContribution = 1.0
		primaryContribution = 0.8
	} else if rpmNormalized > highRpmThreshold {
		// High RPM: primary balance dominates (much stronger contribution)
		secondaryContribution = 0.6
		primaryContribution = 1.0
	} else {
		// Mid RPM: transition between secondary and primary dominance
		transitionFactor := (rpmNormalized - lowRpmThreshold) / (highRpmThreshold - lowRpmThreshold)
		secondaryContribution = 1.0 - (transitionFactor * 0.4) // 1.0 to 0.6
		primaryContribution = 0.8 + (transitionFactor * 0.2)   // 0.8 to 1.0
	}

	// Calculate imbalance factors
	primaryImbalance := 1.0 - a.vehicle.engine.haptics.PrimaryBalance
	secondaryImbalance := 1.0 - a.vehicle.engine.haptics.SecondaryBalance

	// Calculate throttle-based vibration scaling
	// Higher throttle = more engine load = stronger vibrations
	// Lower throttle/idle = less engine load = weaker vibrations
	throttleScale := 0.3 + (throttlePercent * 0.7) // 30% at idle, 100% at full throttle

	// Generate waveform samples
	for i := range *engineBuffer {
		timeOffset := float64(i) / sampleRate

		var waveformValue float64

		// Primary balance component - fundamental firing frequency
		if primaryFreq > 0 && primaryImbalance > 0.01 {
			primaryPhase := 2.0 * math.Pi * primaryFreq * timeOffset

			// Engine-specific primary waveform characteristics
			var primaryWave float64
			switch a.vehicle.engine.geometry {
			case "K": // Wankel
				// Triangular rotor creates smoother primary vibrations
				primaryWave = math.Sin(primaryPhase) * 0.8
				// Add rotor eccentricity harmonics
				primaryWave += math.Sin(primaryPhase*3.0) * 0.2 * primaryImbalance
			case "S": // 2-stroke
				// Sharp, aggressive primary pulses
				primaryWave = math.Sin(primaryPhase)
				// Add higher harmonic content for 2-stroke character
				if primaryFreq*2.0 < maxUsableFreq {
					primaryWave += math.Sin(primaryPhase*2.0) * 0.3 * primaryImbalance
				}
			default: // 4-stroke
				// Standard sinusoidal primary vibrations
				primaryWave = math.Sin(primaryPhase)
			}

			waveformValue += primaryWave * (0.7 + primaryImbalance*0.3) * primaryContribution
		}

		// Secondary balance component - creates vibrations at interval of primary balance
		if secondaryFreq > 0 && secondaryImbalance > 0.01 {
			secondaryPhase := 2.0 * math.Pi * secondaryFreq * timeOffset

			// Engine-specific secondary waveform characteristics
			var secondaryWave float64
			switch a.vehicle.engine.geometry {
			case "K": // Wankel
				// Rotor housing vibrations - more complex waveform
				secondaryWave = math.Sin(secondaryPhase) * 0.6
				secondaryWave += math.Sin(secondaryPhase*1.5) * 0.4 * secondaryImbalance
			case "S": // 2-stroke
				// Port scavenging creates irregular secondary vibrations
				secondaryWave = math.Sin(secondaryPhase) * 0.8
				secondaryWave += math.Sin(secondaryPhase*1.3) * 0.3 * secondaryImbalance
			default: // 4-stroke
				// Standard secondary vibrations from reciprocating mass imbalance
				secondaryWave = math.Sin(secondaryPhase)
			}

			waveformValue += secondaryWave * (0.6 + secondaryImbalance*0.4) * secondaryContribution
		}

		// Add engine roughness similar to original implementation
		if engineRoughness > 0.01 {
			roughnessPhase := float64(a.state.current.sequenceNumber+uint32(i)) * 0.001
			roughnessContribution := math.Sin(roughnessPhase) * engineRoughness * 0.1
			waveformValue += roughnessContribution
		}

		// Apply throttle-based scaling - more throttle = stronger vibrations (engine under load)
		waveformValue *= throttleScale

		// Ensure the magnitude stays within bounds
		waveformValue, _ = signal.LimitWindow(waveformValue, -1.0, 1.0)

		(*engineBuffer)[i] = amplitude * waveformValue
	}
}
