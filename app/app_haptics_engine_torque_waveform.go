package app

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// generateTorqueCurveWaveform creates engine haptic waveforms based on engine-specific torque curves
// and torsional excitation characteristics as documented by EPI Engineering.
// This function incorporates both primary and secondary balance configurations to model
// realistic engine torque output variations.
// TODO: remove or keep and make private. Public to stop linter error.
func (a *App) GenerateTorqueCurveWaveform(rpm float64, engineRoughness float64, engineBuffer *[]float64) {
	sampleRate := float64(a.synth.GetSampleRate())
	rpmPercent := rpm / float64(a.vehicle.revLimit)

	// Base firing frequency from engine characteristics
	baseFiringRate := rpm * a.vehicle.engine.firingFrequency * a.vehicle.engine.haptics.PulseScale

	// Calculate engine-specific torque curve characteristics
	// Primary balance affects fundamental torque ripple (main firing order)
	primaryTorqueFreq := baseFiringRate

	// Secondary balance affects higher-order torque variations
	// These occur at different multiples based on engine configuration
	var (
		secondaryTorqueFreq float64
		tertiaryTorqueFreq  float64
	)

	// Engine-specific torque curve harmonics based on layout and balance

	switch a.vehicle.engine.layout {
	case "I":
		// Inline engines: Secondary balance affects reciprocating mass imbalance
		// Creates torque variations at 2x crankshaft frequency
		secondaryTorqueFreq = primaryTorqueFreq * 2.0
		tertiaryTorqueFreq = primaryTorqueFreq * 4.0
	case "V":
		// V engines: Balance depends on bank angle and crank configuration
		// Secondary balance creates variations at different harmonics
		switch a.vehicle.engine.chambers {
		case 8:
			// V8 with typical 90° bank angle
			secondaryTorqueFreq = primaryTorqueFreq * 1.5
			tertiaryTorqueFreq = primaryTorqueFreq * 3.0
		case 6:
			// V6 configurations vary widely
			secondaryTorqueFreq = primaryTorqueFreq * 1.33
			tertiaryTorqueFreq = primaryTorqueFreq * 2.67
		default:
			// Other V configurations
			secondaryTorqueFreq = primaryTorqueFreq * 1.25
			tertiaryTorqueFreq = primaryTorqueFreq * 2.5
		}
	case "H":
		// Horizontally opposed: Generally well-balanced
		secondaryTorqueFreq = primaryTorqueFreq * 2.0
		tertiaryTorqueFreq = primaryTorqueFreq * 4.0
	default:
		// Default case for other engine types
		secondaryTorqueFreq = primaryTorqueFreq * 1.5
		tertiaryTorqueFreq = primaryTorqueFreq * 3.0
	}

	// Limit frequencies to usable range (160Hz low-pass filter)
	maxUsableFreq := 160.0
	if primaryTorqueFreq > maxUsableFreq {
		primaryTorqueFreq = maxUsableFreq
	}

	if secondaryTorqueFreq > maxUsableFreq {
		secondaryTorqueFreq = maxUsableFreq * 0.8
	}

	if tertiaryTorqueFreq > maxUsableFreq {
		tertiaryTorqueFreq = maxUsableFreq * 0.6
	}

	// Calculate amplitude components based on engine balance
	throttlePercent := float64(a.gtClient.Telemetry.ThrottleOutputPercent()) / 100
	throttlePercent, _ = signal.LimitWindow(throttlePercent, 0.0, 1.0)

	// Vehicle type adjustments
	var (
		gainOffset     float64
		amplitudeScale float64
	)

	switch a.vehicle.vehicleType {
	case vehicleTypeRace:
		gainOffset = 0.0
		amplitudeScale = 0.4
	case vehicleTypeTuned:
		gainOffset = -3.0
		amplitudeScale = 0.25
	case vehicleTypeStreet:
		fallthrough
	default:
		gainOffset = -4.75
		amplitudeScale = 0.02
	}

	// Base amplitude with torque curve characteristics
	baseAmplitude := 0.6 + (throttlePercent * amplitudeScale)

	// Apply overall gain adjustment with -1dB power ratio scaling for RPM
	rpmNormalized, _ := signal.LimitWindow(rpmPercent, 0.0, 1.0)

	// Apply -1dB power ratio scaling based on RPM
	rpmPowerRatio := synthesizer.GainToPowerRatio(-1.0 * rpmNormalized)

	adjust := synthesizer.GainToPowerRatio(a.vehicle.engine.haptics.Gain + gainOffset)

	// Primary torque component - fundamental firing frequency
	// Affected by primary balance (crankshaft and main bearing design)
	primaryAmplitude := baseAmplitude * (2.0 - a.vehicle.engine.haptics.PrimaryBalance) * rpmPowerRatio

	// Secondary torque component - reciprocating mass and secondary forces
	// Affected by secondary balance (counterweight design, balance shafts)
	secondaryAmplitude := baseAmplitude * 0.6 * (1.5 - a.vehicle.engine.haptics.SecondaryBalance) * rpmPowerRatio

	// Tertiary component - higher order harmonics from valve train, etc.
	tertiaryAmplitude := baseAmplitude * 0.3 * (2.0 - a.vehicle.engine.haptics.PrimaryBalance) *
		(1.5 - a.vehicle.engine.haptics.SecondaryBalance) * rpmPowerRatio

	// Generate torque curve waveform
	for index := range *engineBuffer {
		timePosition := float64(index) / sampleRate

		// Primary torque component (fundamental firing frequency)
		primaryPhase := 2.0 * math.Pi * primaryTorqueFreq * timePosition
		primaryComponent := primaryAmplitude * math.Sin(primaryPhase)

		// Secondary torque component (reciprocating mass imbalance)
		secondaryPhase := 2.0 * math.Pi * secondaryTorqueFreq * timePosition
		secondaryComponent := secondaryAmplitude * math.Sin(secondaryPhase)

		// Tertiary component (higher order harmonics)
		tertiaryPhase := 2.0 * math.Pi * tertiaryTorqueFreq * timePosition
		tertiaryComponent := tertiaryAmplitude * math.Sin(tertiaryPhase)

		// Combine components with phase relationships
		combinedTorque := primaryComponent + secondaryComponent + tertiaryComponent

		// Add engine-specific roughness modulation
		roughnessPhase := float64(a.state.current.sequenceNumber+uint32(index)) * 0.0003
		roughnessVariation := 1.0 + (engineRoughness * math.Sin(roughnessPhase) * 0.2)
		combinedTorque *= roughnessVariation

		// Apply RPM-dependent torque curve shaping
		// High RPM engines tend to have more pronounced torque variations
		rpmTorqueScaling := 1.0 + (rpmNormalized * 0.3)
		combinedTorque *= rpmTorqueScaling

		// Engine-specific torque curve modifications
		switch a.vehicle.engine.geometry {
		case "K":
			// Wankel rotary: Smoother torque curve with unique characteristics
			// Reduce sharp peaks and add eccentric rotor modulation
			combinedTorque *= 0.8
			eccentricPhase := 2.0 * math.Pi * primaryTorqueFreq * timePosition * 0.33
			eccentricModulation := 1.0 + (0.1 * math.Sin(eccentricPhase))
			combinedTorque *= eccentricModulation
		case "S":
			// 2-stroke: More aggressive torque pulses with port effects
			combinedTorque *= 1.2
			// Add port timing effects
			portPhase := 2.0 * math.Pi * primaryTorqueFreq * timePosition * 2.0
			portModulation := 1.0 + (0.15 * math.Sin(portPhase))
			combinedTorque *= portModulation
		}

		// Apply final amplitude scaling and limits
		finalAmplitude := combinedTorque * adjust
		finalAmplitude, _ = signal.LimitWindow(finalAmplitude, -1.0, 1.0)

		(*engineBuffer)[index] = finalAmplitude
	}
}
