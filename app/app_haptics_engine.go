package app

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

const maxPulseRate float64 = 300.0 // Max pulse rate for engine haptics

type engineState struct {
	lastSeq       uint32    // Last sequence ID for engine haptics
	lastKnownRPM  float64   // Cache last known RPM for fallback
	lastEventTime time.Time // Timestamp of last engine haptic event
	pulsePolarity bool      // Alternating polarity for engine pulses
}

type engineCharacteristics struct {
	layout          string
	dbEntry         string
	geometry        string
	chambers        int
	revLimit        uint16
	firingFrequency float64
	pulseOverlap    float64 // Calculated overlap factor based on cylinder/crank alignment
	haptics         *haptics.EngineProfile
}

// generateEngineHaptic creates a wavform to simulate engine vibrations
func (a *App) generateEngineHaptic() {
	// Engine haptics are silenced
	if a.config.GetEngineGain() <= config.MinimumGain {
		return
	}

	// No engine haptics configured
	if a.vehicle.engine.firingFrequency == 0 {
		return
	}

	if !a.state.telemetryActive {
		return
	}

	rpm := float64(a.gtClient.Telemetry.EngineRPM())

	// Cache last known RPM and timestamp for fallback when telemetry is unavailable
	currentTime := time.Now()
	if a.state.current.sequenceNumber > a.state.engine.lastSeq {
		a.state.engine.lastKnownRPM = rpm
		a.state.engine.lastEventTime = currentTime
		a.state.engine.lastSeq = a.state.current.sequenceNumber
	} else if currentTime.Sub(a.state.engine.lastEventTime) > 1000*time.Millisecond {
		// stop engine haptics of no telemetry received for 1 second or more
		return
	} else {
		// Use cached RPM if telemetry is unavailable
		rpm = a.state.engine.lastKnownRPM
	}

	// Generate engine vibration waveform for 2 frames worth of samples
	// This provides a small buffer to prevent underruns while keeping latency low
	sampleRate := float64(a.synth.GetSampleRate())
	samplesPerFrame := int(sampleRate / engineHapticFrameRate)
	bufferSamples := samplesPerFrame * 2
	engineBuffer := make([]float64, bufferSamples)

	offset := 0
	lastPolarity := 0
	lookback := 20

	// Stitch the new engine samples smoothly with the current buffer contents
	inspectBuffer := a.synth.InspectChannelBuffer("engine", samplesPerFrame+lookback, -lookback)
	if inspectBuffer != nil && len(inspectBuffer) >= samplesPerFrame {
		offset, lastPolarity = synthesizer.FindSampleZeroCrossing(inspectBuffer[lookback:samplesPerFrame])
	}

	// No haptics when engine is not running
	if rpm == 0 {
		a.synth.OverwriteBuffer("engine", engineBuffer, offset)

		return
	}

	engineRoughness := a.calculateEngineRoughness(rpm)

	a.generatePulseWaveform(rpm, engineRoughness, &engineBuffer)

	// Alternative experimental waveform generation - comment out above line and uncomment below to test
	// a.generateBalancedWaveform(rpm, engineRoughness, &engineBuffer)

	// Engine-specific torque curve waveform - uncomment below to use torque curve based haptics
	// a.generateTorqueCurveWaveform(rpm, engineRoughness, &engineBuffer)

	// Adjust engine buffer polarity to be the inverse of the last pulse
	currentPolarity := synthesizer.SamplePolarity(engineBuffer[0:lookback])
	if currentPolarity == float64(lastPolarity) {
		synthesizer.InvertSamplePolarity(&engineBuffer)
	}

	a.synth.OverwriteBuffer("engine", engineBuffer, offset)
}

// getEngineCharacteristics retrieves engine characteristics based on a given engien geometry and speed
func (a *App) getEngineCharacteristics(engineLayout string, cylinderAngle float32, crankPlaneAngle float32, revLimit uint16) (engineCharacteristics, error) {
	if engineLayout == "" {
		return engineCharacteristics{
			haptics: &haptics.EngineProfile{},
		}, fmt.Errorf("engine layout not provided")
	}

	geometryCode := engineLayout[:1]                // Get first character for geometry type
	chambers, err := strconv.Atoi(engineLayout[1:]) // Get remaining characters for chamber count
	if err != nil {
		return engineCharacteristics{}, err // Return error if conversion fails
	}

	characteristics := engineCharacteristics{
		layout:          engineLayout,
		dbEntry:         "",
		geometry:        geometryCode,
		chambers:        chambers,
		revLimit:        revLimit,
		firingFrequency: getEngineFiringFrequency(geometryCode, chambers),
		pulseOverlap:    0.5 - calculatePulseOverlap(cylinderAngle, crankPlaneAngle, chambers, geometryCode),
		haptics: &haptics.EngineProfile{
			PrimaryBalance:   1.0,
			SecondaryBalance: 1.0,
			Gain:             config.MinimumGain,
			PulseScale:       1.0,
		},
	}

	revRange := "std"
	switch {
	case revLimit > 13000:
		revRange = "high"
	case revLimit > 9000:
		revRange = "med"
	case revLimit < 6000:
		revRange = "low"
	}

	layoutVariations := []string{
		engineLayout + "_b" + strconv.FormatFloat(float64(cylinderAngle), 'f', 0, 32) + "_c" + strconv.FormatFloat(float64(crankPlaneAngle), 'f', 0, 32) + "_r" + revRange,
		engineLayout + "_b" + strconv.FormatFloat(float64(cylinderAngle), 'f', 0, 32) + "_c" + strconv.FormatFloat(float64(crankPlaneAngle), 'f', 0, 32),
		engineLayout + "_c" + strconv.FormatFloat(float64(crankPlaneAngle), 'f', 0, 32) + "_r" + revRange,
		engineLayout + "_c" + strconv.FormatFloat(float64(crankPlaneAngle), 'f', 0, 32),
		engineLayout + "_b" + strconv.FormatFloat(float64(cylinderAngle), 'f', 0, 32) + "_r" + revRange,
		engineLayout + "_b" + strconv.FormatFloat(float64(cylinderAngle), 'f', 0, 32),
		engineLayout + "_r" + revRange,
		engineLayout,
	}

	for _, variation := range layoutVariations {
		profile := a.config.GetEngineProfile(variation)
		if profile == nil {
			continue
		}

		characteristics.dbEntry = variation
		characteristics.haptics = profile

		break
	}

	return characteristics, nil
}

// getEngineFiringFrequency calculates the firing frequency based on engine geometry and chamber count
func getEngineFiringFrequency(geometry string, chambers int) float64 {
	switch geometry {
	case "":
		return 0.0 // No engine haptics
	case "K": // Wankel rotary engines fire 3 times per rotor per revolution
		return (float64(chambers) * 3.0) / 60.0
	case "S": // Two stroke engines fire once per cylinder every revolution
		return float64(chambers) / 60.0
	default: // Four stroke engines fire once per cylinder every 2 revolutions
		return (float64(chambers) / 2.0) / 60.0
	}
}

// calculatePulseOverlap calculates pulse overlap based on alignment between crank plane angle and cylinder bank angle
// Returns overlap factor from 0.0 (no overlap/perfect alignment) to 1.0 (maximum overlap/misalignment)
// This value is currently used for pulse width overlap. Timing clustering is disabled pending better implementation.
// - Low values (0.0-0.2): Well-aligned engines (e.g., 60° V6 with 120° crank)
// - High values (0.3-0.8): Misaligned engines (e.g., 90° V6 with 120° crank)
func calculatePulseOverlap(cylinderAngle, crankPlaneAngle float32, chambers int, geometry string) float64 {
	// Wankel overlap based on rotor count and housing design
	// Multiple rotors create natural overlap due to phase offset
	if geometry == "K" {
		if chambers > 1 {
			return 0.15 + (float64(chambers-1) * 0.05) // 15-25% overlap for multi-rotor
		}

		return 0.05 // Single rotor has minimal overlap
	}

	// Single cylinder engines have no overlap
	if chambers <= 1 {
		return 0.0
	}

	// Calculate angle difference between cylinder bank and crank plane
	firingOffset := math.Abs(float64(cylinderAngle - crankPlaneAngle))

	// Normalize to 0-180 degree range (angles are symmetric)
	if firingOffset > 180.0 {
		firingOffset = 360.0 - firingOffset
	}

	var baseOverlap float64
	var alignmentFactor float64

	switch geometry {
	case "K": // Wankel engines have unique firing characteristics
		baseOverlap = 0.2 // Lower base overlap due to rotor design

		// Cylinder count affects overlap potential
		cylinderFactor := math.Min(float64(chambers)/4.0, 1.0)
		baseOverlap *= (0.5 + cylinderFactor*0.5)
	case "S": // 2-strokes fire every revolution, creating more natural overlap
		baseOverlap = 0.3 // Higher base overlap due to rapid firing

		// Perfect alignment (0°) or perpendicular (90°) affects overlap differently
		if firingOffset <= 15.0 {
			// Near-perfect alignment: minimal overlap due to synchronized firing
			alignmentFactor = 0.3
		} else if firingOffset >= 75.0 && firingOffset <= 105.0 {
			// Perpendicular arrangement: maximum overlap
			alignmentFactor = 1.0
		} else {
			// Progressive alignment: interpolate between min and max
			if firingOffset < 45.0 {
				alignmentFactor = 0.3 + ((firingOffset-15.0)/30.0)*0.4 // 0.3 to 0.7
			} else {
				alignmentFactor = 0.7 + ((75.0-firingOffset)/30.0)*0.3 // 0.7 to 1.0
			}
		}
	default: // 4-strokes fire every other revolution, creating different overlap characteristics
		baseOverlap = 0.2 // Lower base overlap due to spaced firing intervals

		// Cylinder count affects overlap potential
		cylinderFactor := math.Min(float64(chambers)/8.0, 1.0) // Normalize to 8-cylinder reference
		baseOverlap *= (0.5 + cylinderFactor*0.5)              // Scale: 50% to 100% of base

		if firingOffset <= 10.0 {
			// Near-perfect alignment: synchronized banks, minimal overlap
			alignmentFactor = 0.2
		} else if firingOffset >= 80.0 && firingOffset <= 100.0 {
			// Near-perpendicular: optimal staggered firing, maximum overlap
			alignmentFactor = 1.0
		} else if firingOffset >= 170.0 {
			// Near-opposite: boxer-style layout, minimal overlap
			alignmentFactor = 0.1
		} else {
			// Progressive alignment based on angle
			if firingOffset < 45.0 {
				// Moving away from alignment toward staggered
				alignmentFactor = 0.2 + ((firingOffset-10.0)/35.0)*0.5 // 0.2 to 0.7
			} else if firingOffset < 90.0 {
				// Approaching optimal stagger
				alignmentFactor = 0.7 + ((firingOffset-45.0)/35.0)*0.3 // 0.7 to 1.0
			} else if firingOffset < 135.0 {
				// Moving past optimal toward opposite
				alignmentFactor = 1.0 - ((firingOffset-90.0)/45.0)*0.6 // 1.0 to 0.4
			} else {
				// Approaching opposite layout
				alignmentFactor = 0.4 - ((firingOffset-135.0)/35.0)*0.3 // 0.4 to 0.1
			}
		}
	}

	// Apply chamber count scaling for engines with many cylinders
	chamberScale := 1.0
	if chambers >= 8 {
		// More cylinders = more potential for overlap
		chamberScale = 1.0 + (float64(chambers-8) * 0.05) // +5% per cylinder above 8
	} else if chambers <= 4 && geometry != "S" {
		// Fewer cylinders = less overlap potential (except 2-strokes)
		chamberScale = 0.6 + (float64(chambers-2) * 0.2) // 60% for 2-cyl, 80% for 3-cyl, 100% for 4-cyl
	}

	finalOverlap := baseOverlap * alignmentFactor * chamberScale

	// Ensure overlap stays within reasonable bounds
	if finalOverlap > 0.8 {
		finalOverlap = 0.8 // Maximum 80% overlap
	} else if finalOverlap < 0.0 {
		finalOverlap = 0.0 // No negative overlap
	}

	return finalOverlap
}

// calculateEngineRoughness calculates a roughness value based on the engine geometry and a given RPM
func (a *App) calculateEngineRoughness(rpm float64) float64 {
	var engineRoughness float64
	switch a.vehicle.engine.geometry {
	case "K":
		roughnessPhase := float64(a.state.current.sequenceNumber) * 0.003
		apexSealRoughness := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.08
		housingEccentricity := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.05
		roughnessIntensity := apexSealRoughness + housingEccentricity*0.7

		// Wankels get smoother at higher RPM due to improved sealing
		rpmSmoothingFactor := math.Min(rpm/4000.0, 1.0)
		engineRoughness = math.Sin(roughnessPhase) * roughnessIntensity * (1.0 - rpmSmoothingFactor*0.8)

		// Add characteristic Wankel "chatter" at low RPM
		if rpm < 2000.0 {
			chatterPhase := float64(a.state.current.sequenceNumber) * 0.008
			chatterIntensity := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.03
			engineRoughness += math.Sin(chatterPhase) * chatterIntensity * (1.0 - rpm/2000.0)
		}
	case "S":
		// 2-stroke engines have characteristic roughness due to scavenging process
		roughnessPhase := float64(a.state.current.sequenceNumber) * 0.007
		scavenging := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.20
		exhaustBlowdown := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.12
		intakeExhaustoverlap := 0.6
		baseRoughness := scavenging + exhaustBlowdown*intakeExhaustoverlap

		// Add characteristic 2-stroke "buzz" - more intense at mid RPM
		rpmFactor := math.Min(rpm/6000.0, 1.0)
		buzzIntensity := baseRoughness * (0.5 + rpmFactor*0.8)

		engineRoughness = math.Sin(roughnessPhase)*buzzIntensity + math.Sin(roughnessPhase*2.3)*buzzIntensity*0.4

		// Add port timing irregularities at low RPM
		if rpm < 3000.0 {
			portPhase := float64(a.state.current.sequenceNumber) * 0.012
			portIrregularity := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.08
			engineRoughness += math.Sin(portPhase) * portIrregularity * (1.0 - rpm/3000.0)
		}

		// 2-strokes get slightly smoother at very high RPM due to better scavenging
		if rpm > 4000.0 {
			smoothing := math.Min((rpm-4000.0)/4000.0, 0.3)
			engineRoughness *= (1.0 - smoothing)
		}
	default:
		// Default to 4-stroke engine characteristics
		if rpm <= 2400.0 {
			roughnessPhase := float64(a.state.current.sequenceNumber) * 0.005
			// Poor primary balance creates more low-frequency roughness
			primaryRoughness := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.15
			// Poor secondary balance creates more high-frequency roughness
			secondaryRoughness := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.08
			roughnessIntensity := primaryRoughness + secondaryRoughness*0.5
			engineRoughness = math.Sin(roughnessPhase)*roughnessIntensity + math.Sin(roughnessPhase*1.7)*roughnessIntensity*0.5

			// Smooth out roughness as RPM increases
			rpmSmoothingFactor := rpm / 2400.0
			engineRoughness *= (1.0 - rpmSmoothingFactor*a.vehicle.engine.haptics.PrimaryBalance)
		} else {
			// High RPM: roughness based on engine balance characteristics
			if a.vehicle.engine.haptics.PrimaryBalance < 0.9 {
				roughnessPhase := float64(a.state.current.sequenceNumber) * 0.002
				highRpmRoughness := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.02 // Poor primary balance creates roughness
				engineRoughness = math.Sin(roughnessPhase) * highRpmRoughness
			} else {
				engineRoughness = 0.0 // Well-balanced engines are smooth at high RPM
			}
		}
	}

	return engineRoughness
}

// generatePulseWaveform creates a vibration pulse waveform based on engine RPM and roughness characteristics
func (a *App) generatePulseWaveform(rpm float64, engineRoughness float64, engineBuffer *[]float64) {
	rpmPercent := rpm / float64(a.vehicle.revLimit)
	rpmPercent, _ = signal.LimitWindow(rpmPercent, 0.0, 1.0)

	throttlePercent := float64(a.gtClient.Telemetry.ThrottleOutputPercent()) / 100
	throttlePercent, _ = signal.LimitWindow(throttlePercent, 0.0, 1.0)

	var vehicleTypeGain float64
	switch a.vehicle.vehicleType {
	case "race":
		vehicleTypeGain = 0.0
	case "tuned":
		vehicleTypeGain = -3.0
	default: // "street" or other types
		vehicleTypeGain = -4.75
	}

	// Determine amplitude by engine type and load characteristics
	engineLoadGainIncrease := 1.0
	engineLoadGain := (1 - throttlePercent) * engineLoadGainIncrease
	roughness := 1.0 - (engineRoughness * rpmPercent * 0.1)
	gain := a.vehicle.engine.haptics.Gain + vehicleTypeGain + engineLoadGain
	amplitude := synthesizer.GainToPowerRatio(gain) * roughness
	amplitude, _ = signal.LimitWindow(amplitude, 0, 1)

	sampleRate := float64(a.synth.GetSampleRate())
	pulseRate := rpm * a.vehicle.engine.firingFrequency * a.vehicle.engine.haptics.PulseScale

	// Create pulses with increasing width and overlap at higher RPM
	// Base overlap at idle, up to 2x overlap at high RPM
	pulseDutyCycle := a.vehicle.engine.pulseOverlap + (rpmPercent * a.vehicle.engine.pulseOverlap * 2)

	// Normal engine pulse generation (rev limiter already checked above)
	for i := range *engineBuffer {
		samplesPerPulse := sampleRate / pulseRate
		pulsePosition := float64(i) / samplesPerPulse

		// Detect pulse trigger point (beginning of each cycle)
		currentPulseIndex := int(math.Floor(pulsePosition))
		var lastPulseIndex int
		if i > 0 {
			lastPulsePosition := float64(i-1) / samplesPerPulse
			lastPulseIndex = int(math.Floor(lastPulsePosition))
		} else {
			lastPulseIndex = -1
		}

		// Check if the wavedorm has crossed into a new pulse cycle
		pulseTriggered := (i > 0) && (currentPulseIndex != lastPulseIndex)

		// Alternate polarity for each pulse
		if pulseTriggered {
			a.state.engine.pulsePolarity = !a.state.engine.pulsePolarity
		}

		var pulseValue float64 = 0.0
		pulsePhase := pulsePosition - math.Floor(pulsePosition) // 0.0 to 1.0 within each pulse cycle
		if pulsePhase < pulseDutyCycle {
			// Inside the pulse - create a sharp, distinct pulse
			pulsePhaseNormalized := pulsePhase / pulseDutyCycle // 0.0 to 1.0 within pulse width

			switch a.vehicle.engine.geometry {
			case "K":
				pulseValue = generatePulseWankel(pulsePhaseNormalized, a.vehicle.engine.haptics)
			case "S":
				pulseValue = generatePulseTwoStroke(pulsePhaseNormalized, a.vehicle.engine.haptics)
			default:
				pulseValue = generatePulseFourStroke(pulsePhaseNormalized, a.vehicle.engine.haptics)
			}

			// Apply polarity - alternating positive and negative pulses
			if !a.state.engine.pulsePolarity {
				pulseValue = -pulseValue
			}

			// Add per-pulse roughness variation based on engine characteristics
			secondaryImbalance := 1.0 - a.vehicle.engine.haptics.SecondaryBalance
			if rpm <= 2400.0 && secondaryImbalance > 0.02 {
				roughnessPhase := float64(a.state.current.sequenceNumber+uint32(i)) * 0.0005
				roughnessVariation := 1.0 + (math.Sin(roughnessPhase) * secondaryImbalance * 0.3)
				pulseValue *= roughnessVariation
			}
		}

		// Ensure the magnitude stays within bounds
		pulseValue, _ = signal.LimitWindow(pulseValue, -1.0, 1.0)

		(*engineBuffer)[i] = amplitude * pulseValue
	}
}

// generatePulswWankel creates a single pulse value for a Wankel engine based on a given phase value
// and engine gemoetry
func generatePulseWankel(phase float64, engine *haptics.EngineProfile) (pulse float64) {
	if phase < 0.3 {
		// Quick attack (30% of pulse width)
		// Adjust attack characteristics based on engine balance
		// Poor balance = sharper attack, good balance = smoother attack
		attackSharpness := 1.0 - (engine.PrimaryBalance * 0.6) // 0.4 to 1.0 range
		attackPhase := phase / 0.3

		// Wankels have unique triangular rotor pulses - more gradual attack
		attackSharpness *= 0.7 // Reduce sharpness for Wankel characteristic
		pulse = math.Sin(attackPhase*math.Pi/2) * attackSharpness

		// Add slight rotor eccentricity modulation
		eccentricityFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.1*math.Sin(attackPhase*math.Pi*3)
		pulse *= eccentricityFactor
	} else {
		// Quick decay (70% of pulse width)
		// Adjust decay based on both primary and secondary balance
		decayPhase := (phase - 0.3) / 0.7

		// Wankel decay characteristics - smoother, more gradual
		primaryDecayRate := 3.0 + (1.0-engine.PrimaryBalance)*1.5
		secondaryDecayFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.3
		combinedDecayRate := primaryDecayRate * secondaryDecayFactor

		// Wankels have more gradual decay due to chamber expansion characteristics
		pulse = math.Exp(-decayPhase*combinedDecayRate) * (1.0 - decayPhase*0.1)
	}

	return pulse
}

// generatePulswTwoStroke creates a single pulse value for a 2-strok engine based on a given phase value
// and engine gemoetry
func generatePulseTwoStroke(phase float64, engine *haptics.EngineProfile) (pulse float64) {
	if phase < 0.3 {
		// Quick attack (30% of pulse width)
		// Adjust attack characteristics based on engine balance
		// Poor balance = sharper attack, good balance = smoother attack
		attackSharpness := 1.0 - (engine.PrimaryBalance * 0.6) // 0.4 to 1.0 range
		attackPhase := phase / 0.3

		// 2-stroke engines have very sharp, aggressive attack due to rapid combustion
		attackSharpness *= 1.3 // Increase sharpness for 2-stroke characteristic

		// 2-strokes have a more explosive attack than 4-strokes
		pulse = math.Pow(math.Sin(attackPhase*math.Pi/2), attackSharpness*0.6)

		// Add port opening/closing noise during attack
		portNoise := (1.0 - engine.SecondaryBalance) * 0.15
		portPhase := attackPhase * math.Pi * 4 // Higher frequency port effects
		pulse += math.Sin(portPhase) * portNoise * attackPhase
	} else {
		// Quick decay (70% of pulse width)
		// Adjust decay based on both primary and secondary balance
		decayPhase := (phase - 0.3) / 0.7

		// 2-stroke decay characteristics - rapid but irregular due to exhaust blowdown
		primaryDecayRate := 5.0 + (1.0-engine.PrimaryBalance)*3.0
		secondaryDecayFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.8

		combinedDecayRate := primaryDecayRate * secondaryDecayFactor

		// 2-strokes have rapid decay with exhaust port effects
		baseDecay := math.Exp(-decayPhase * combinedDecayRate)

		// Add exhaust blowdown characteristics - creates a "ragged" decay
		blowdownIntensity := (1.0 - engine.SecondaryBalance) * 0.25
		blowdownPhase := decayPhase * math.Pi * 3 // Higher frequency for port effects
		blowdownEffect := math.Sin(blowdownPhase) * blowdownIntensity * (1.0 - decayPhase)

		pulse = baseDecay + blowdownEffect
	}

	return pulse
}

// generatePulswFourStroke creates a single pulse value for a 4-stroke engine based on a given phase value
// and engine gemoetry
func generatePulseFourStroke(phase float64, engine *haptics.EngineProfile) (pulse float64) {
	if phase < 0.3 {
		// Quick attack (30% of pulse width)
		// Adjust attack characteristics based on engine balance
		// Poor balance = sharper attack, good balance = smoother attack
		attackSharpness := 1.0 - (engine.PrimaryBalance * 0.6) // 0.4 to 1.0 range
		attackPhase := phase / 0.3

		pulse = math.Sin(attackPhase * math.Pi / 2)

		// Use different attack curves based on engine balance
		if engine.PrimaryBalance < 0.7 {
			// Sharp, aggressive attack for poorly balanced engines
			pulse = math.Pow(pulse, attackSharpness)
		} else {
			// Smoother attack for well-balanced engines
			pulse = pulse * attackSharpness
		}
	} else {
		// Quick decay (70% of pulse width)
		// Adjust decay based on both primary and secondary balance
		decayPhase := (phase - 0.3) / 0.7

		// Primary balance affects base decay rate
		primaryDecayRate := 4.0 + (1.0-engine.PrimaryBalance)*2.0

		// Secondary balance affects decay smoothness
		secondaryDecayFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.5

		combinedDecayRate := primaryDecayRate * secondaryDecayFactor
		pulse = math.Exp(-decayPhase * combinedDecayRate)
	}

	return pulse
}
