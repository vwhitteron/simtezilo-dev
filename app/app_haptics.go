package app

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synth"
)

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

	// Skip if engine is off (RPM is zero)
	if rpm == 0 {
		// Use frame-based buffer size for consistency
		sampleRate := float64(a.config.Synthesizer.SampleRateHz)
		samplesPerFrame := int(sampleRate / 60.0) // 60 FPS telemetry rate
		a.synth.WriteBuffer("engine", make([]float64, samplesPerFrame))
		return
	}

	// Use the vehicle's actual rev limiter maximum RPM from telemetry
	rpmMax := float64(a.gtClient.Telemetry.EngineRPMLight().Max)

	// If rev limiter data is not available, fall back to a reasonable default
	if rpmMax <= 0 {
		rpmMax = 8000.0
	}

	// Normalize RPM from 0 to max (instead of idle to max)
	rpmNormalized := rpm / rpmMax

	// Clamp normalized RPM to 0-1 range
	if rpmNormalized < 0 {
		rpmNormalized = 0
	} else if rpmNormalized > 1 {
		rpmNormalized = 1
	}

	// Calculate engine firing frequency mapped to 15-60 Hz range
	// Map RPM from idle (~800) to max RPM linearly to 15-60 Hz frequency range
	minFreq := 15.0 // Increased from 8.0 Hz to 15.0 Hz
	maxFreq := 60.0 // Reduced from 160.0 Hz to 60.0 Hz for better tactile feel

	// Define typical idle RPM for scaling
	idleRPM := 800.0

	// Calculate frequency range and RPM range
	frequencyRange := maxFreq - minFreq // 45 Hz range
	rpmRange := rpmMax - idleRPM

	// Map current RPM to frequency range
	var engineFrequency float64
	if rpm <= idleRPM {
		engineFrequency = minFreq // At or below idle = minimum frequency
	} else {
		// Linear mapping from idle to max RPM -> min to max frequency
		rpmAboveIdle := rpm - idleRPM
		frequencyRatio := rpmAboveIdle / rpmRange
		engineFrequency = minFreq + (frequencyRatio * frequencyRange)

		// Clamp to the desired range
		if engineFrequency < minFreq {
			engineFrequency = minFreq
		} else if engineFrequency > maxFreq {
			engineFrequency = maxFreq
		}
	}

	// Generate amplitude that scales from 0 at 0 RPM to 1.0 at max RPM
	// Use a curve that gives good feel across the RPM range
	// Start from zero amplitude at 0 RPM for no engine haptic output when engine is off
	baseAmplitude := rpmNormalized * (0.8 + (rpmNormalized * 0.2)) // Range: 0.0 to 1.0

	// Add engine roughness variation that's more "lumpy" feeling
	// Use slower, irregular variations to simulate engine firing cycles
	roughnessPhase := float64(a.state.current.seq) * 0.05                               // Slower variation
	engineRoughness := math.Sin(roughnessPhase)*0.2 + math.Sin(roughnessPhase*1.7)*0.15 // Strong roughness

	amplitude := baseAmplitude + (engineRoughness * rpmNormalized) // Scale roughness by RPM

	// Ensure amplitude stays within bounds
	if amplitude < 0 {
		amplitude = 0 // No negative amplitude
	} else if amplitude > 1.0 {
		amplitude = 1.0 // Standard maximum amplitude
	}

	// Generate engine vibration waveform for one frame only (1/60th second at 60 FPS)
	// This prevents echo and overlapping from multiple telemetry frames
	sampleRate := float64(a.config.Synthesizer.SampleRateHz)
	samplesPerFrame := int(sampleRate / 60.0) // 60 FPS telemetry rate
	engineBuffer := make([]float64, samplesPerFrame)

	for i := range engineBuffer {
		// Calculate pulse timing based on RPM
		// Higher RPM = more frequent pulses (shorter spacing)
		// Use engine frequency to determine pulse rate
		pulsesPerSecond := engineFrequency
		samplesPerPulse := sampleRate / pulsesPerSecond

		// Create short, sharp pulses instead of continuous sine waves
		pulsePosition := float64(i) / samplesPerPulse
		pulseFraction := pulsePosition - math.Floor(pulsePosition) // 0.0 to 1.0 within each pulse cycle

		var pulseValue float64

		// Generate a short, sharp pulse at the beginning of each cycle
		pulseWidth := 0.25 // Pulse takes up 25% of the cycle (wider for better feel)

		if pulseFraction < pulseWidth {
			// Inside the pulse - create a sharp attack and quick decay
			pulsePhase := pulseFraction / pulseWidth // 0.0 to 1.0 within the pulse

			// Sharp attack, exponential decay for realistic engine "thump"
			// Use bipolar pulses that swing positive and negative for stronger feel
			if pulsePhase < 0.05 {
				// Very quick rise (5% of pulse width) for sharp impact
				// Start negative, swing to positive for bipolar effect
				pulseValue = (pulsePhase/0.05)*2.0 - 1.0 // Range: -1.0 to +1.0
			} else {
				// Exponential decay (95% of pulse width)
				decayPhase := (pulsePhase - 0.05) / 0.95
				decayValue := math.Exp(-decayPhase * 2.5) // Decay from 1.0 to ~0

				// Create oscillating decay for bipolar effect
				oscillation := math.Sin(decayPhase * math.Pi * 3) // 3 oscillations during decay
				pulseValue = decayValue * oscillation             // Bipolar oscillating decay
			}

			// Add some randomness for engine roughness
			roughnessPhase := float64(a.state.current.seq+uint32(i)) * 0.001
			roughness := 1.0 + (math.Sin(roughnessPhase) * 0.2) // ±20% variation
			pulseValue *= roughness

		} else {
			// Outside the pulse - silence between pulses
			pulseValue = 0.0
		}

		// Add harmonics to make high RPM vibrations more perceptible
		// When engine frequency gets too high for effective haptics (>60 Hz),
		// add lower frequency harmonics that stay within the 15-60 Hz range
		harmonicValue := 0.0
		if engineFrequency > 60.0 {
			// Calculate harmonic frequencies that stay within the desired range
			// Use subharmonics (1/2, 1/3, 1/4, etc.) until we find frequencies within 15-60 Hz
			minFreq := 15.0 // Increased from 8.0 Hz to 15.0 Hz
			maxFreq := 60.0 // Reduced from 160.0 Hz to 60.0 Hz

			var validHarmonics []float64

			// Generate more subharmonics to handle up to 20,000 RPM scenarios
			// At 20,000 RPM with 4-cylinder engine: (20000/60)*2 = 667 Hz
			// Need harmonics up to divisor 44 to get 667/44 ≈ 15 Hz
			for divisor := 2.0; divisor <= 50.0; divisor += 1.0 {
				harmonicFreq := engineFrequency / divisor
				if harmonicFreq >= minFreq && harmonicFreq <= maxFreq {
					validHarmonics = append(validHarmonics, harmonicFreq)
				}
				// Stop if we have enough harmonics (max 12 for performance)
				if len(validHarmonics) >= 12 {
					break
				}
			}

			// Generate harmonic pulses with different phases and amplitudes
			timeSeconds := float64(i) / sampleRate

			// Combine valid harmonics with decreasing amplitudes
			for j, harmonicFreq := range validHarmonics {
				// Decrease amplitude for higher order harmonics - boosted primary harmonic
				var amplitude float64
				if j == 0 {
					// Primary harmonic gets extra boost for stronger presence
					amplitude = 2.0 // Increased from 1.2 to 2.0 for primary harmonic
				} else {
					// Other harmonics remain at current levels
					amplitude = 1.2 / (float64(j) + 1.0) // 0.6, 0.4, 0.3, etc.
				}
				harmonicComponent := math.Sin(2*math.Pi*harmonicFreq*timeSeconds) * amplitude
				harmonicValue += harmonicComponent
			}

			// Scale harmonic contribution based on how high the main frequency is
			// Much stronger harmonic strength for better high-RPM feel
			harmonicStrength := math.Min((engineFrequency-60.0)/80.0, 1.0) // Max 100% harmonic (was 90%)
			harmonicValue *= harmonicStrength
		}

		// Combine main pulse with harmonics
		finalValue := pulseValue + harmonicValue

		// Ensure the combined signal stays within standard ±1.0 bounds
		if finalValue > 1.0 {
			finalValue = 1.0
		} else if finalValue < -1.0 {
			finalValue = -1.0
		}

		engineBuffer[i] = amplitude * finalValue
	}

	a.synth.WriteBuffer("engine", engineBuffer)
}
