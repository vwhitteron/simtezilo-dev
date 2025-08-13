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

	// Normalize RPM from 0 to max (instead of idle to max)
	rpmNormalized := rpm / rpmMax

	// Clamp normalized RPM to 0-1 range
	if rpmNormalized < 0 {
		rpmNormalized = 0
	} else if rpmNormalized > 1 {
		rpmNormalized = 1
	}

	// Calculate engine firing frequency mapped to 25-60 Hz range
	// Map RPM from idle (~800) to max RPM linearly to 25-60 Hz frequency range
	minFreq := 8.0   // Increased from 15.0 Hz to 25.0 Hz
	maxFreq := 160.0 // Increased from 34.0 Hz to 60.0 Hz for higher pulse rate

	// Define typical idle RPM for scaling
	idleRPM := 800.0

	// Calculate frequency range and RPM range
	frequencyRange := maxFreq - minFreq // 35 Hz range
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

	// Generate amplitude that maintains more consistent volume from idle to max RPM
	// Use a curve that provides good feel across the RPM range with less dramatic changes
	// Start from a reasonable base amplitude at idle for consistent engine haptic output
	baseAmplitude := 0.4 + (rpmNormalized * 0.4) // Range: 0.4 to 0.8 for more consistent volume

	// Add engine roughness variation that's more "lumpy" feeling
	// Use slower, irregular variations to simulate engine firing cycles
	// Remove roughness above 2400 RPM for smoother high-RPM feel
	var engineRoughness float64
	if rpm <= 2400.0 {
		roughnessPhase := float64(a.state.current.seq) * 0.005                              // Much slower variation
		engineRoughness = math.Sin(roughnessPhase)*0.02 + math.Sin(roughnessPhase*1.7)*0.01 // Very subtle roughness
	} else {
		engineRoughness = 0.0 // No roughness above 2400 RPM
	}

	amplitude := baseAmplitude + (engineRoughness * rpmNormalized * 0.1) // Much reduced roughness impact

	// Ensure amplitude stays within bounds with more consistent range
	if amplitude < 0.2 {
		amplitude = 0.2 // Minimum amplitude for consistent baseline
	} else if amplitude > 0.9 {
		amplitude = 0.9 // Reduced maximum amplitude for consistency
	}

	// Generate engine vibration waveform for 6 frames (6/60th second at 60 FPS)
	// This reduces computational load while maintaining smooth haptic feedback
	sampleRate := float64(a.config.Synthesizer.SampleRateHz)
	samplesPerBuffer := int(sampleRate / 10.0) // 60 FPS / 6 frames = 10 Hz update rate
	engineBuffer := make([]float64, samplesPerBuffer)

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

		// Generate a smooth pulse at the beginning of each cycle
		// Allow overlapping pulses at higher RPM for more density
		pulseWidth := 0.25 + (rpmNormalized * 0.85) // Pulse takes up 25%-110% of cycle based on RPM (allows overlap)

		if pulseFraction < pulseWidth {
			// Inside the pulse - create a very smooth attack and decay
			pulsePhase := pulseFraction / pulseWidth // 0.0 to 1.0 within the pulse

			// Very smooth attack and decay using cosine curves for realistic engine "thump"
			// Use bipolar pulses that swing positive and negative for stronger feel
			if pulsePhase < 0.35 {
				// Very smooth rise (35% of pulse width) using cosine curve
				// Cosine curve provides much smoother transitions than linear
				cosinePhase := (pulsePhase / 0.35) * math.Pi / 2 // 0 to π/2
				smoothRise := math.Sin(cosinePhase)              // Smooth 0 to 1 curve
				pulseValue = (smoothRise * 2.0) - 1.0            // Range: -1.0 to +1.0
			} else {
				// Smooth exponential decay (65% of pulse width) with cosine smoothing
				decayPhase := (pulsePhase - 0.35) / 0.65
				decayValue := math.Exp(-decayPhase * 2.0) // Gentler decay from 1.0 to ~0

				// Create smooth oscillating decay for bipolar effect using cosine
				oscillationPhase := decayPhase * math.Pi * 2.5         // Fewer oscillations during decay
				oscillation := math.Cos(oscillationPhase) * decayValue // Smooth bipolar oscillating decay
				pulseValue = oscillation                               // Bipolar oscillating decay
			}

			// Add some smoothed randomness for engine roughness (only below 2400 RPM)
			if rpm <= 2400.0 {
				roughnessPhase := float64(a.state.current.seq+uint32(i)) * 0.0005
				roughness := 1.0 + (math.Sin(roughnessPhase) * 0.01) // Very subtle ±1% variation
				pulseValue *= roughness
			}
			// No per-pulse roughness above 2400 RPM for completely smooth high-RPM feel

		} else {
			// Outside the pulse - silence between pulses
			pulseValue = 0.0
		}

		// Use the pulse value directly without harmonics
		finalValue := pulseValue

		// Ensure the signal stays within standard ±1.0 bounds
		if finalValue > 1.0 {
			finalValue = 1.0
		} else if finalValue < -1.0 {
			finalValue = -1.0
		}

		engineBuffer[i] = amplitude * finalValue
	}

	a.synth.WriteBuffer("engine", engineBuffer)
}
