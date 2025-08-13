package app

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
)

// Engine configuration helper functions
func getEngineCylinderCount(engineLayout string) int {
	switch engineLayout {
	case "I3":
		return 3
	case "I4", "H4", "V4", "K4":
		return 4
	case "I5":
		return 5
	case "I6", "H6", "V6":
		return 6
	case "I8", "V8":
		return 8
	case "V10":
		return 10
	case "V12", "H12":
		return 12
	case "W16":
		return 16
	case "K2":
		return 2 // Wankel rotors, treat as 2 cylinders equivalent
	default:
		return 4 // Default to 4-cylinder if unknown
	}
}

func getEngineFiringFrequency(rpm float64, engineLayout string) float64 {
	cylinders := getEngineCylinderCount(engineLayout)

	// Wankel engines fire differently - each rotor fires 3 times per revolution
	if engineLayout == "K2" || engineLayout == "K4" {
		rotors := cylinders                         // K2 = 2 rotors, K4 = 4 rotors
		return (rpm * float64(rotors) * 3.0) / 60.0 // 3 combustions per rotor per revolution
	}

	// Most engines fire once per cylinder every 2 revolutions (4-stroke)
	// But for haptic purposes, we consider the power stroke frequency
	firingEventsPerRevolution := float64(cylinders) / 2.0

	return (rpm * firingEventsPerRevolution) / 60.0
}

func getEngineCharacteristics(engineLayout string) (smoothness float64, baseRoughness float64) {
	switch engineLayout {
	case "I3":
		return 0.3, 0.15 // Very rough, unbalanced
	case "I4":
		return 0.6, 0.08 // Moderately smooth
	case "I5":
		return 0.7, 0.06 // Smoother than I4, unique firing pattern
	case "I6":
		return 0.9, 0.03 // Very smooth, naturally balanced
	case "I8":
		return 0.95, 0.02 // Extremely smooth
	case "H4":
		return 0.8, 0.05 // Boxer engine - smooth but distinctive
	case "H6":
		return 0.92, 0.025 // Very smooth boxer
	case "H12":
		return 0.98, 0.01 // Extremely smooth
	case "V4":
		return 0.5, 0.10 // Compact but can be rough
	case "V6":
		return 0.85, 0.04 // Good balance
	case "V8":
		return 0.95, 0.02 // Very smooth, even firing
	case "V10":
		return 0.88, 0.035 // Smooth but distinctive sound
	case "V12":
		return 0.98, 0.01 // Extremely smooth
	case "W16":
		return 0.99, 0.005 // Incredibly smooth
	case "K2":
		return 0.85, 0.04 // Wankel - smooth but distinctive
	case "K4":
		return 0.92, 0.02 // Multi-rotor Wankel - very smooth
	default:
		return 0.6, 0.08 // Default to I4 characteristics
	}
}

func (a *App) hapticEvents() {
	startTime := time.Now()

	a.updateState()

	if !a.sequenceHasAdvanced() {
		return
	}

	if a.vehicleHasChanged() {
		a.resetState()
		a.disableHaptics("vehicle changed")

		a.updateVehicle()

		return
	}

	// Do nothing if telemetry is not indicating an active state
	if !a.telemetryIsActive() {
		a.state.telemetryActive = false

		if a.state.hapticsEnabled {
			a.resetState()
			a.disableHaptics("not live")
		}

		return
	}

	a.state.telemetryActive = true

	// The loading flag typically means the session has restarted
	if a.sessionHasReset() {
		a.resetState()
		a.disableHaptics("session reset")

		return
	}

	// Initialise the gear if it hasn't been set yet
	if a.state.last.gear == kinematics.NullGear {
		a.resetState()
		a.disableHaptics("initialising gear")

		return
	}

	if a.timeOfDayHasReset() {
		a.resetState()
		a.disableHaptics("time of day reset")

		a.log.Debug().
			Uint32("sequence_id", a.state.current.seq).
			Str("current_time_of_day", a.state.current.timeOfDay.String()).
			Str("last_time_of_day", a.state.last.timeOfDay.String()).
			Msg("time of day reset")

		return
	}

	// if !a.kinematics.VehicleIsInMotion() {
	// 	return
	// }

	if !a.state.hapticsEnabled {
		a.enableHaptics()
	}

	// a.ui.SetLive(true)

	a.kinematics.Current.SequenceID = a.state.current.seq

	// no haptics if telemetry packets dropped/missed
	// if a.telemetryPacketsDropped() > 1 {
	// 	return
	// }

	windowSeconds := (float64(a.state.current.seqDelta) / frameRate)

	a.kinematics.Update(windowSeconds, a.gtClient)

	if a.gearHasChanged() {
		a.playGearChangeHaptic()
	}

	a.generateChassisHaptic()
	a.generateEngineHaptic()

	a.state.last = a.state.current
	a.kinematics.Current.ComputeTime = time.Since(startTime)
	a.kinematics.Last = a.kinematics.Current

	if a.kinematics.Current.ComputeTime.Microseconds() > 16000 {
		a.log.Warn().Float64("ms", float64(a.kinematics.Current.ComputeTime.Milliseconds())).Msg("slow compute")
	}

}

func (a *App) generateChassisHaptic() {
	startTime := time.Now()

	pulseFrequencyHz := a.calculateChassisHapticPulseFrequency()

	pulseWidth := math.Round(float64(a.config.Synthesizer.SampleRateHz) / (2 * pulseFrequencyHz))

	pulseAmplitude := a.calculateChassisHapticPulseAmplitude()

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	// Use frame-based buffer size for consistency (1/60th second at 60 FPS)
	sampleRate := float64(a.config.Synthesizer.SampleRateHz)
	samplesPerFrame := int(sampleRate / 60.0)
	pulseBuffer := make([]float64, samplesPerFrame)

	// Only generate pulse samples up to the buffer size or pulse width, whichever is smaller
	maxSamples := int(math.Min(float64(samplesPerFrame), pulseWidth*2))

	for i := range maxSamples {
		phase := waveSamplePeriod * (float64(i) - waveOffset)
		pulseBuffer[i] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
	}

	a.synth.WriteBuffer("chassis", pulseBuffer)

	// log large amplitude values
	if pulseAmplitude > 1.0 || pulseAmplitude < -1.0 {
		a.log.Debug().
			Float64("jerk", a.kinematics.Current.SixDOFTranslationCalc.Jerk).
			Float64("snap", a.kinematics.Current.SixDOFTranslationCalc.Snap).
			Str("process_time", time.Since(startTime).String()).
			Uint32("sequence_id", a.kinematics.Current.SequenceID).
			Msg("Bump inputs")
		a.log.Debug().
			Float64("amplitude", pulseAmplitude).
			Float64("samplePeriod", waveSamplePeriod).
			Float64("pulseWidth", pulseWidth).
			Msg("Bump outputs")
	}
}

func (a *App) calculateChassisHapticPulseAmplitude() float64 {
	sig := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Jerk, (a.kinematics.Current.SixDOFRotation.Jerk * snapMultiplier))
	pulseAmplitude := signal.Exponent(sig, a.config.GetJerkCurve())
	pulseAmplitude = signal.Scale(pulseAmplitude, a.config.GetJerkScale())

	p1 := pulseAmplitude
	pulseAmplitude, wasLimited := signal.LimitMax(pulseAmplitude, a.config.Synthesizer.PulseMaxAmplitude)
	if wasLimited {
		a.log.Debug().Float64("pulse", p1).Msg("limiter")
	}

	a.kinematics.Last.SynthOutputAmplitude = a.kinematics.Current.SynthOutputAmplitude
	a.kinematics.Current.SynthOutputAmplitude = pulseAmplitude

	return pulseAmplitude
}

func (a *App) calculateChassisHapticPulseFrequency() float64 {
	snap := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Snap, (a.kinematics.Current.SixDOFRotation.Snap * snapMultiplier))

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, a.config.GetSnapCurve()))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, a.config.GetSnapScale())
	pulseFrequencyHz := a.config.GetFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < a.config.GetMinHz() {
		pulseFrequencyHz = a.config.GetMinHz()
	} else if pulseFrequencyHz > a.config.GetMaxHz() {
		pulseFrequencyHz = a.config.GetMaxHz()
	}

	a.kinematics.Last.SynthOutputFrequency = a.kinematics.Current.SynthOutputFrequency
	a.kinematics.Current.SynthOutputFrequency = int(pulseFrequencyHz)

	return pulseFrequencyHz
}

func (a *App) disableHaptics(reason string) {
	// speaker.Suspend()
	a.synth.Silence()
	a.synth.ClearBuffer()
	a.state.hapticsEnabled = false

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Str("reason", reason).Msg("haptics state change")
}

func (a *App) enableHaptics() {
	// speaker.Resume()
	a.synth.FadeIn(config.FadeInDuration)
	a.state.hapticsEnabled = true

	a.log.Debug().Bool("haptics enabled", a.state.hapticsEnabled).Msg("haptics state change")
}

func (a *App) playGearChangeHaptic() {
	magnitude := a.determineGearChangeMagnitude()

	a.synth.PlayEffectWithMagnitude("transmission", magnitude)
	a.log.Debug().
		Int("sequence_id", int(a.state.current.seq)).
		Float64("magnitude", magnitude).
		Float64("gforce", a.kinematics.GetSurgeGforce()).
		Int("gear", a.kinematics.Current.TransmissionGear).
		Msg("gear change")
}

func (a *App) determineGearChangeMagnitude() float64 {
	magnitude, _ := a.synth.GetChannelMagnitude("transmission")

	if !a.config.DynamicTransmissionFeedbackEnabled() {
		return magnitude
	}

	gForce := a.kinematics.GetSurgeGforce()
	gforceMax := a.config.GetTransmissionGforceMax()
	volumeCurve := a.config.GetTransmissionCurve()

	magnitudeMin := synth.GainToPowerRatio(a.transmissionGainMin)

	gearChangeMagnitude := math.Pow((gForce/gforceMax), volumeCurve) * magnitude
	gearChangeMagnitude, _ = signal.LimitWindow(gearChangeMagnitude, magnitudeMin, magnitude)

	return gearChangeMagnitude
}

func (a *App) generateEngineHaptic() {
	rpm := float64(a.gtClient.Telemetry.EngineRPM())

	// Cache last known RPM and timestamp for fallback when telemetry is unavailable
	currentTime := time.Now()
	if rpm > 0 {
		// Update last known good RPM and timestamp
		a.state.lastKnownRPM = rpm
		a.state.lastRPMTime = currentTime
	} else if a.state.lastKnownRPM > 0 && currentTime.Sub(a.state.lastRPMTime) < 1000*time.Millisecond {
		// Use cached RPM if it's less than 500ms old
		rpm = a.state.lastKnownRPM
	}

	// Generate haptics every 6 frames to reduce computational load
	if a.state.current.seq%6 != 0 {
		return
	}

	// Skip if engine is off (RPM is zero)
	if rpm == 0 {
		// Use 6 frames worth of buffer size for consistency
		sampleRate := float64(a.config.Synthesizer.SampleRateHz)
		samplesPerBuffer := int(sampleRate / 10.0) // 60 FPS / 6 frames = 10 Hz update rate
		a.synth.WriteBuffer("engine", make([]float64, samplesPerBuffer))
		return
	}

	// Use the vehicle's actual rev limiter maximum RPM from telemetry
	rpmMax := float64(a.gtClient.Telemetry.EngineRPMLight().Max)

	// If rev limiter data is not available, fall back to a reasonable default
	if rpmMax <= 0 {
		rpmMax = 8000.0
	}

	// Get engine layout (fallback to I4 if not available)
	engineLayout := a.state.current.vehicle.engineLayout
	if engineLayout == "" {
		engineLayout = "I4" // Default to inline-4 if no data
	}

	// Calculate realistic engine firing frequency based on engine configuration
	engineFiringFrequency := getEngineFiringFrequency(rpm, engineLayout)

	// Clamp firing frequency to reasonable haptic range (8-160 Hz)
	if engineFiringFrequency < 8.0 {
		engineFiringFrequency = 8.0
	} else if engineFiringFrequency > 160.0 {
		engineFiringFrequency = 160.0
	}

	// Get engine-specific characteristics
	smoothness, baseRoughness := getEngineCharacteristics(engineLayout)

	// Normalize RPM from 0 to max
	rpmNormalized := rpm / rpmMax
	if rpmNormalized < 0 {
		rpmNormalized = 0
	} else if rpmNormalized > 1 {
		rpmNormalized = 1
	}

	// Generate amplitude that maintains consistent volume from idle to max RPM
	baseAmplitude := 0.4 + (rpmNormalized * 0.4) // Range: 0.4 to 0.8

	// Add engine-specific roughness variation
	// Roughness decreases with RPM and smoothness characteristic
	var engineRoughness float64
	if rpm <= 2400.0 {
		// Low RPM roughness varies by engine type
		roughnessPhase := float64(a.state.current.seq) * 0.005
		roughnessIntensity := baseRoughness * (1.0 - smoothness*0.5) // Less smooth engines are rougher
		engineRoughness = math.Sin(roughnessPhase)*roughnessIntensity + math.Sin(roughnessPhase*1.7)*roughnessIntensity*0.5

		// Reduce roughness as RPM increases (engines smooth out)
		rpmSmoothingFactor := rpm / 2400.0
		engineRoughness *= (1.0 - rpmSmoothingFactor*smoothness)
	} else {
		// High RPM: roughness based on engine smoothness characteristic
		if smoothness < 0.9 {
			roughnessPhase := float64(a.state.current.seq) * 0.002
			highRpmRoughness := (1.0 - smoothness) * 0.02 // Less smooth engines retain some roughness
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

	for i := range engineBuffer {
		// Use realistic firing frequency for pulse timing
		pulsesPerSecond := engineFiringFrequency
		samplesPerPulse := sampleRate / pulsesPerSecond

		// Create pulses based on engine firing events
		pulsePosition := float64(i) / samplesPerPulse
		pulseFraction := pulsePosition - math.Floor(pulsePosition) // 0.0 to 1.0 within each pulse cycle

		var pulseValue float64

		// Pulse width varies with RPM and engine characteristics
		// Smoother engines have wider, more overlapping pulses at high RPM
		basePulseWidth := 0.25 + (rpmNormalized * 0.85 * smoothness) // 25% to 110% based on smoothness

		if pulseFraction < basePulseWidth {
			// Inside the pulse - create smooth attack and decay
			pulsePhase := pulseFraction / basePulseWidth // 0.0 to 1.0 within the pulse

			// Engine-specific pulse shaping
			if pulsePhase < 0.35 {
				// Attack phase - varies by engine type
				cosinePhase := (pulsePhase / 0.35) * math.Pi / 2
				smoothRise := math.Sin(cosinePhase)

				// Less smooth engines have sharper attacks
				attackSharpness := 1.0 + (1.0-smoothness)*0.5
				pulseValue = math.Pow(smoothRise, attackSharpness)*2.0 - 1.0
			} else {
				// Decay phase
				decayPhase := (pulsePhase - 0.35) / 0.65
				decayValue := math.Exp(-decayPhase * 2.0)

				// Engine-specific decay characteristics
				oscillationRate := 2.5 + (1.0-smoothness)*1.0 // Rougher engines oscillate more
				oscillationPhase := decayPhase * math.Pi * oscillationRate
				oscillation := math.Cos(oscillationPhase) * decayValue
				pulseValue = oscillation
			}

			// Add per-pulse roughness variation based on engine characteristics
			if rpm <= 2400.0 && baseRoughness > 0.02 {
				roughnessPhase := float64(a.state.current.seq+uint32(i)) * 0.0005
				roughnessVariation := 1.0 + (math.Sin(roughnessPhase) * baseRoughness * 0.5)
				pulseValue *= roughnessVariation
			}

		} else {
			// Outside the pulse - silence between pulses
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
