package app

import (
	"math"
	"strconv"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
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
	firingFrequency float64
	haptics         *haptics.EngineProfile
}

// getEngineCharacteristics retrieves engine characteristics based on layout and angles
func (a *App) getEngineCharacteristics(engineLayout string, cylinderAngle float32, crankPlaneAngle float32) (engineCharacteristics, error) {
	if engineLayout == "" {
		return engineCharacteristics{}, nil
	}

	geometryCode := engineLayout[:1]                // Get first character for geometry
	chambers, err := strconv.Atoi(engineLayout[1:]) // Get remaining characters for chamber count
	if err != nil {
		return engineCharacteristics{}, err // Return error if conversion fails
	}

	layoutVariations := []string{
		engineLayout + ".B" + strconv.FormatFloat(float64(cylinderAngle), 'f', 0, 32) + ".C" + strconv.FormatFloat(float64(crankPlaneAngle), 'f', 0, 32),
		engineLayout + ".C" + strconv.FormatFloat(float64(crankPlaneAngle), 'f', 0, 32),
		engineLayout + ".B" + strconv.FormatFloat(float64(cylinderAngle), 'f', 0, 32),
		engineLayout,
	}

	hapticProfile := &haptics.EngineProfile{
		PrimaryBalance:   1.0,
		SecondaryBalance: 1.0,
		Gain:             0.0,
		PulseScale:       1.0,
	}
	dbEntry := ""
	for _, variation := range layoutVariations {
		profile := a.config.GetEngineProfile(variation)
		if profile == nil {
			continue
		}

		hapticProfile = profile
		dbEntry = variation

		break
	}

	return engineCharacteristics{
		layout:          engineLayout,
		dbEntry:         dbEntry,
		geometry:        geometryCode,
		chambers:        chambers,
		firingFrequency: getEngineFiringFrequency(geometryCode, chambers),
		haptics:         hapticProfile,
	}, nil
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
	throttlePercent := float64(a.gtClient.Telemetry.ThrottleOutputPercent())
	if throttlePercent > 0 {
		throttlePercent /= 100.0
	} else {
		throttlePercent = 0.0
	}

	// Cache last known RPM and timestamp for fallback when telemetry is unavailable
	currentTime := time.Now()
	if a.state.current.seq > a.state.engine.lastSeq {
		// Update last known good RPM and timestamp
		a.state.engine.lastKnownRPM = rpm
		a.state.engine.lastEventTime = currentTime
		a.state.engine.lastSeq = a.state.current.seq
	} else if currentTime.Sub(a.state.engine.lastEventTime) > 1000*time.Millisecond {
		// stop engine haptics of no telemetry received for 1 second or more
		return
	} else {
		// Use cached RPM for up to 2 seconds if telemetry is unavailable
		rpm = a.state.engine.lastKnownRPM
	}

	// Generate engine vibration waveform for double the engine haptic frame rate
	// This overfills the engine haptics buffer with the current rpm waveform in case the buffer
	// reader is too fast. This does mean the rpm waveform on the tail end of the buffer is not
	// correct when these events occur. The buffer is overwritten every interval so for typical
	// events where the reader is not fetching too far it will get the correct waveform for the
	// current rpm value.
	sampleRate := float64(a.synth.GetSampleRate())
	samplesPerBuffer := int(sampleRate/engineHapticFrameRate) * 2
	engineBuffer := make([]float64, samplesPerBuffer)

	// No haptics when engine is not running
	if rpm == 0 {
		a.synth.WriteBuffer("engine", engineBuffer)

		return
	}

	// Use the vehicle's actual rev limiter maximum RPM from telemetry
	revLimit := float64(a.gtClient.Telemetry.EngineRPMLight().Max)

	var pulseRate float64
	// For high-firing engines like Wankels, use a completely different approach
	// Map RPM to a much lower frequency range for perceptible intervals
	if a.vehicle.engine.geometry == "K" {
		// pulseRate = rpm * a.vehicle.engine.firingFrequency * a.vehicle.pulseScale
		pulseRate = rpm * a.vehicle.engine.firingFrequency * a.vehicle.engine.haptics.PulseScale
	} else {
		// pulseRate = rpm * a.vehicle.engine.firingFrequency * a.vehicle.pulseScale
		pulseRate = rpm * a.vehicle.engine.firingFrequency * a.vehicle.engine.haptics.PulseScale
	}

	// Calculate RPM proportion relative to rev limit
	rpmPercent := rpm / revLimit

	// Normalize RPM from 0 to max
	// Is this even necessary? rpm should never really be above revLimit
	rpmNormalized, _ := signal.LimitWindow(rpmPercent, 0.0, 1.0)

	// Generate amplitude with louder idle feedback and linear 30% volume reduction at max RPM
	// Start with higher base amplitude for idle/low RPM feedback
	// baseAmplitude := 0.7 + (rpmNormalized * 0.1) // Range: 0.7 to 0.8
	baseAmplitude := 0.7 + (throttlePercent * 0.3) // Range: 0.6 to 0.9

	// Apply 30% linear volume reduction from idle to max RPM
	// At idle (0 RPM): 1.0 factor (full volume)
	// At max RPM: 0.7 factor (30% reduction)
	// volumeReductionFactor := 1.0 - (rpmNormalized * 0.3)
	// baseAmplitude *= volumeReductionFactor

	// Add engine-specific roughness variation
	// Roughness based on primary and secondary balance characteristics
	var engineRoughness float64
	if a.vehicle.engine.geometry == "K" {
		// Wankel engines have unique roughness characteristics
		roughnessPhase := float64(a.state.current.seq) * 0.003
		// Wankel apex seal roughness varies with rotor balance
		apexSealRoughness := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.08
		// Rotor housing eccentricity affects secondary roughness
		housingRoughness := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.05
		roughnessIntensity := apexSealRoughness + housingRoughness*0.7

		// Wankels get smoother at higher RPM due to improved sealing
		rpmSmoothingFactor := math.Min(rpm/4000.0, 1.0) // Smooth out by 4000 RPM
		engineRoughness = math.Sin(roughnessPhase) * roughnessIntensity * (1.0 - rpmSmoothingFactor*0.8)

		// Add characteristic Wankel "chatter" at low RPM
		if rpm < 2000.0 {
			chatterPhase := float64(a.state.current.seq) * 0.008
			chatterIntensity := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.03
			engineRoughness += math.Sin(chatterPhase) * chatterIntensity * (1.0 - rpm/2000.0)
		}
	} else if a.vehicle.engine.geometry == "S" {
		// 2-stroke engines have characteristic roughness due to scavenging process
		roughnessPhase := float64(a.state.current.seq) * 0.007

		// Scavenging port roughness - more pronounced at lower RPM
		scavengingRoughness := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.20
		// Exhaust port blowdown creates secondary roughness
		exhaustRoughness := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.12

		// 2-strokes are inherently rougher due to overlapping combustion/exhaust cycles
		baseRoughness := scavengingRoughness + exhaustRoughness*0.6

		// Add characteristic 2-stroke "buzz" - more intense at mid RPM
		rpmFactor := math.Min(rpm/6000.0, 1.0)
		buzzIntensity := baseRoughness * (0.5 + rpmFactor*0.8) // Peak intensity around mid-RPM

		engineRoughness = math.Sin(roughnessPhase)*buzzIntensity + math.Sin(roughnessPhase*2.3)*buzzIntensity*0.4

		// Add port timing irregularities at low RPM
		if rpm < 3000.0 {
			portPhase := float64(a.state.current.seq) * 0.012
			portIrregularity := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.08
			engineRoughness += math.Sin(portPhase) * portIrregularity * (1.0 - rpm/3000.0)
		}

		// 2-strokes get slightly smoother at very high RPM due to better scavenging
		if rpm > 4000.0 {
			smoothingFactor := math.Min((rpm-4000.0)/4000.0, 0.3) // Max 30% smoothing
			engineRoughness *= (1.0 - smoothingFactor)
		}
	} else if rpm <= 2400.0 {
		// Low RPM roughness varies by engine type
		roughnessPhase := float64(a.state.current.seq) * 0.005
		// Poor primary balance creates more low-frequency roughness
		primaryRoughness := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.15
		// Poor secondary balance creates more high-frequency roughness
		secondaryRoughness := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.08
		roughnessIntensity := primaryRoughness + secondaryRoughness*0.5
		engineRoughness = math.Sin(roughnessPhase)*roughnessIntensity + math.Sin(roughnessPhase*1.7)*roughnessIntensity*0.5

		// Reduce roughness as RPM increases (engines smooth out)
		rpmSmoothingFactor := rpm / 2400.0
		engineRoughness *= (1.0 - rpmSmoothingFactor*a.vehicle.engine.haptics.PrimaryBalance)
	} else {
		// High RPM: roughness based on engine balance characteristics
		if a.vehicle.engine.haptics.PrimaryBalance < 0.9 {
			roughnessPhase := float64(a.state.current.seq) * 0.002
			highRpmRoughness := (1.0 - a.vehicle.engine.haptics.PrimaryBalance) * 0.02 // Poor primary balance creates roughness
			engineRoughness = math.Sin(roughnessPhase) * highRpmRoughness
		} else {
			engineRoughness = 0.0 // Well-balanced engines have no roughness at high RPM
		}
	}

	amplitude := (baseAmplitude + (engineRoughness * rpmNormalized * 0.1)) * synth.GainToPowerRatio(a.vehicle.engine.haptics.Gain)

	// Ensure amplitude stays within bounds
	amplitude, _ = signal.LimitWindow(amplitude, 0, 1)

	// Create pulses with increasing width and overlap at higher RPM
	// Pulse width gets much wider at higher RPM, allowing up to 25% overlap
	pulseDutyCycle := 0.25 + (rpmPercent * 0.50) // 25% width at idle, 75% width at high RPM (allows 25% overlap)

	// Normal engine pulse generation (rev limiter already checked above)
	for i := range engineBuffer {
		// Use realistic firing frequency for pulse timing
		samplesPerPulse := sampleRate / pulseRate

		// Create pulses based on engine firing events
		pulsePosition := float64(i) / samplesPerPulse

		// Detect pulse trigger point (beginning of each cycle)
		currentPulseIndex := int(math.Floor(pulsePosition))
		lastPulseIndex := int(math.Floor((float64(i - 1)) / samplesPerPulse))

		// Check if we've crossed into a new pulse cycle
		pulseTriggered := (i > 0) && (currentPulseIndex != lastPulseIndex)

		// Toggle polarity when a new pulse is triggered
		if pulseTriggered {
			a.state.engine.pulsePolarity = !a.state.engine.pulsePolarity
		}

		var pulseValue float64
		pulsePhase := pulsePosition - math.Floor(pulsePosition) // 0.0 to 1.0 within each pulse cycle
		if pulsePhase < pulseDutyCycle {
			// Inside the pulse - create a sharp, distinct pulse
			pulsePhaseNormalized := pulsePhase / pulseDutyCycle // 0.0 to 1.0 within pulse width

			if pulsePhaseNormalized < 0.3 {
				// Quick attack (30% of pulse width)
				// Adjust attack characteristics based on engine balance
				// Poor balance = sharper attack, good balance = smoother attack
				attackSharpness := 1.0 - (a.vehicle.engine.haptics.PrimaryBalance * 0.6) // 0.4 to 1.0 range
				attackPhase := pulsePhaseNormalized / 0.3

				// Special handling for Wankel engines
				switch a.vehicle.engine.geometry {
				case "K":
					// Wankels have unique triangular rotor pulses - more gradual attack
					attackSharpness *= 0.7 // Reduce sharpness for Wankel characteristic
					pulseValue = math.Sin(attackPhase*math.Pi/2) * attackSharpness

					// Add slight rotor eccentricity modulation
					eccentricityFactor := 1.0 + (1.0-a.vehicle.engine.haptics.SecondaryBalance)*0.1*math.Sin(attackPhase*math.Pi*3)
					pulseValue *= eccentricityFactor
				case "S":
					// 2-stroke engines have very sharp, aggressive attack due to rapid combustion
					attackSharpness *= 1.3 // Increase sharpness for 2-stroke characteristic

					// 2-strokes have a more explosive attack than 4-strokes
					pulseValue = math.Pow(math.Sin(attackPhase*math.Pi/2), attackSharpness*0.6)

					// Add port opening/closing noise during attack
					portNoise := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.15
					portPhase := attackPhase * math.Pi * 4 // Higher frequency port effects
					pulseValue += math.Sin(portPhase) * portNoise * attackPhase
				default:
					// Use different attack curves based on engine balance
					if a.vehicle.engine.haptics.PrimaryBalance < 0.7 {
						// Sharp, aggressive attack for poorly balanced engines
						pulseValue = math.Pow(math.Sin(attackPhase*math.Pi/2), attackSharpness)
					} else {
						// Smoother attack for well-balanced engines
						pulseValue = math.Sin(attackPhase*math.Pi/2) * attackSharpness
					}
				}
			} else {
				// Quick decay (70% of pulse width)
				// Adjust decay based on both primary and secondary balance
				decayPhase := (pulsePhaseNormalized - 0.3) / 0.7

				switch a.vehicle.engine.geometry {
				case "K":
					// Wankel decay characteristics - smoother, more gradual
					primaryDecayRate := 3.0 + (1.0-a.vehicle.engine.haptics.PrimaryBalance)*1.5
					secondaryDecayFactor := 1.0 + (1.0-a.vehicle.engine.haptics.SecondaryBalance)*0.3
					combinedDecayRate := primaryDecayRate * secondaryDecayFactor

					// Wankels have more gradual decay due to chamber expansion characteristics
					pulseValue = math.Exp(-decayPhase*combinedDecayRate) * (1.0 - decayPhase*0.1)
				case "S":
					// 2-stroke decay characteristics - rapid but irregular due to exhaust blowdown
					primaryDecayRate := 5.0 + (1.0-a.vehicle.engine.haptics.PrimaryBalance)*3.0
					secondaryDecayFactor := 1.0 + (1.0-a.vehicle.engine.haptics.SecondaryBalance)*0.8

					combinedDecayRate := primaryDecayRate * secondaryDecayFactor

					// 2-strokes have rapid decay with exhaust port effects
					baseDecay := math.Exp(-decayPhase * combinedDecayRate)

					// Add exhaust blowdown characteristics - creates a "ragged" decay
					blowdownIntensity := (1.0 - a.vehicle.engine.haptics.SecondaryBalance) * 0.25
					blowdownPhase := decayPhase * math.Pi * 3 // Higher frequency for port effects
					blowdownEffect := math.Sin(blowdownPhase) * blowdownIntensity * (1.0 - decayPhase)

					pulseValue = baseDecay + blowdownEffect
				default:
					// Primary balance affects base decay rate
					primaryDecayRate := 4.0 + (1.0-a.vehicle.engine.haptics.PrimaryBalance)*2.0

					// Secondary balance affects decay smoothness
					secondaryDecayFactor := 1.0 + (1.0-a.vehicle.engine.haptics.SecondaryBalance)*0.5

					combinedDecayRate := primaryDecayRate * secondaryDecayFactor
					pulseValue = math.Exp(-decayPhase * combinedDecayRate)
				}
			}

			// Apply polarity - alternating positive and negative pulses
			if !a.state.engine.pulsePolarity {
				pulseValue = -pulseValue
			}

			// Add per-pulse roughness variation based on engine characteristics
			secondaryImbalance := 1.0 - a.vehicle.engine.haptics.SecondaryBalance
			if rpm <= 2400.0 && secondaryImbalance > 0.02 {
				roughnessPhase := float64(a.state.current.seq+uint32(i)) * 0.0005
				roughnessVariation := 1.0 + (math.Sin(roughnessPhase) * secondaryImbalance * 0.3)
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

	a.synth.OverwriteBuffer("engine", engineBuffer)
}
