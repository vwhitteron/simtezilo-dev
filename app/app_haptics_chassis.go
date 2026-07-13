package app

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// channelValueAt returns values[index], or 0 when index is out of range.
// Per-channel telemetry slices may be empty (e.g. chassis muted, so the write
// path never sized them).
func channelValueAt(values []float64, index int) float64 {
	if index < 0 || index >= len(values) {
		return 0
	}

	return values[index]
}

// ensureChannelLen returns a slice of length count, preserving existing values
// and reusing the backing array when its capacity already suffices. It grows the
// slice on demand as the configured output channel count changes.
func ensureChannelLen(values []float64, count int) []float64 {
	if cap(values) >= count {
		return values[:count]
	}

	grown := make([]float64, count)
	copy(grown, values)

	return grown
}

func (a *App) generateChassisHaptic() {
	if a.config.GetSynthChassisMute() {
		return
	}

	startTime := time.Now()

	pulseFrequencyHz := a.calculateChassisHapticPulseFrequency()

	pulseAmplitude, unclampedAmplitude := a.calculateChassisHapticPulseAmplitude()

	sampleRate := float64(a.config.GetSynthInternalSampleRateHz())
	minSamplesPerFrame := int(sampleRate / hapticFrameRate)

	numChannels := a.synth.NumOutputChannels()

	// Size the per-channel telemetry slices to the configured channel count.
	// Channels the chassis is not routed to keep their previous value, matching
	// the prior fixed-array behaviour.
	a.kinematics.Current.SynthChannelAmplitude = ensureChannelLen(a.kinematics.Current.SynthChannelAmplitude, numChannels)
	a.kinematics.Current.SynthChannelFrequency = ensureChannelLen(a.kinematics.Current.SynthChannelFrequency, numChannels)

	// Generate pulse buffers for each channel
	for channel := range numChannels {
		// Skip channels the chassis source is not routed to; no point generating
		// a per-channel buffer the mixer will not consume.
		if !a.config.GetSynthRouteEnabled(synthesizer.ChannelChassis, channel) {
			continue
		}

		channelFreqHz, channelAmplitude, drxActive := a.applyDRX(
			pulseFrequencyHz, pulseAmplitude, unclampedAmplitude, channel,
		)

		channelPulseWidth := math.Round(sampleRate / (2 * channelFreqHz))

		if !drxActive {
			channelAmplitude = signal.Equalize(channelAmplitude, channelPulseWidth, channel, a.config)
		}

		// Store per-channel output for telemetry charting (absolute amplitude for graphing)
		a.kinematics.Current.SynthChannelAmplitude[channel] = signal.Abs(channelAmplitude)
		a.kinematics.Current.SynthChannelFrequency[channel] = channelFreqHz

		channelPulseLength := int(channelPulseWidth * 2)
		bufferSize := max(channelPulseLength, minSamplesPerFrame)

		waveOffset := channelPulseWidth / 2
		waveSamplePeriod := math.Pi / channelPulseWidth

		// Reuse the scratch buffer across ticks, growing it on demand. The generation
		// loop below breaks early once index > channelPulseLength, leaving a tail
		// that must be zero-filled since it is no longer guaranteed by make.
		if cap(a.chassisPulseScratch) < bufferSize {
			a.chassisPulseScratch = make([]float64, bufferSize)
		} else {
			a.chassisPulseScratch = a.chassisPulseScratch[:bufferSize]
		}

		pulseBuffer := a.chassisPulseScratch

		for index := range pulseBuffer {
			pulseBuffer[index] = 0
		}

		// Generate the complete pulse waveform
		for index := range bufferSize {
			if index > channelPulseLength {
				break
			}

			phase := waveSamplePeriod * (float64(index) - waveOffset)
			pulseBuffer[index] = ((channelAmplitude * math.Sin(phase)) + channelAmplitude) / 2
		}

		// Write to the channel-specific chassis buffer
		a.synth.WriteBuffer(synthesizer.ChassisChannelName(channel), pulseBuffer, 0)
	}

	// Calculate shared pulse metrics for peak hold using the base frequency
	pulseWidth := math.Round(sampleRate / (2 * pulseFrequencyHz))
	pulseLength := int(pulseWidth * 2)
	pulseDuration := time.Duration(float64(pulseLength)/sampleRate*1000) * time.Millisecond
	a.jerkPeakHoldDuration = pulseDuration

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
			Float64("pulseWidth", pulseWidth).
			Msg("Bump outputs")
	}
}

// applyDRX checks for DRX activation on the given channel, shifting frequency into
// EQ-attenuated ranges for high impact events.
// Returns the shifted frequency, amplitude, and whether DRX was activated.
// When DRX is not active the original frequency and amplitude are returned with active=false.
func (a *App) applyDRX(
	pulseFrequencyHz, pulseAmplitude, unclampedAmplitude float64,
	channel int,
) (freqHz, amplitude float64, active bool) {
	drxFreq, drxAmp, drxBucketRatio, active := signal.DRXShift(
		pulseFrequencyHz, unclampedAmplitude, channel, a.config,
	)
	if !active {
		return pulseFrequencyHz, pulseAmplitude, false
	}

	eqAttenDB := signal.AmplitudeToDecibels(drxBucketRatio)
	desiredBoostDB := signal.AmplitudeToDecibels(unclampedAmplitude / a.config.GetHapticsPulseMaxAmplitude())

	a.log.Debug().
		Float64("original_freq", pulseFrequencyHz).
		Float64("drx_freq", drxFreq).
		Float64("eq_atten_db", eqAttenDB).
		Float64("desired_boost_db", desiredBoostDB).
		Float64("drx_amplitude", drxAmp).
		Float64("jerk", a.kinematics.Current.SixDOFTranslationCalc.Jerk).
		Int("channel", channel).
		Msg("DRX activated for chassis haptic")

	return drxFreq, drxAmp, true
}

func (a *App) calculateChassisHapticPulseAmplitude() (pulseAmplitude float64, unclampedAmplitude float64) {
	jerk := signal.LargestMagnitude(
		a.kinematics.Current.ResolvedTransJerk,
		a.kinematics.Current.ResolvedRotJerk,
	)

	// Process the signal normally first
	pulseAmplitude = signal.Exponent(jerk, a.config.GethapticsJerkCurve()/1000)
	pulseAmplitude = signal.Scale(pulseAmplitude, a.config.GetHapticsJerkScale())

	unclampedAmplitude = signal.Abs(pulseAmplitude)

	pulseAmplitude, _ = signal.LimitMax(pulseAmplitude, a.config.GetHapticsPulseMaxAmplitude())

	// Apply peak hold to the processed amplitude to prevent cancellation from inverse jerks
	// pulseAmplitude = a.applyJerkPeakHold(jerk, pulseAmplitude)

	return pulseAmplitude, unclampedAmplitude
}

// applyJerkPeakHold implements peak hold with decay to prevent waveform cancellation.
// When a large jerk occurs (e.g., >2000), it's often followed by an inverse jerk that would
// cancel out the haptic feedback. This function detects inverse jerk patterns and holds the
// amplitude to maintain the impact sensation.
func (a *App) applyJerkPeakHold(rawJerk, processedAmplitude float64) float64 { //nolint:unused // peak-hold for planned inverse-jerk detection; deliberately kept
	const jerkThreshold = 2000.0

	const minAmplitudeThreshold = 0.3

	// Use the dynamic peak hold duration based on current pulse length
	peakHoldDuration := a.jerkPeakHoldDuration
	if peakHoldDuration == 0 {
		// Fallback to 50ms if not set (shouldn't happen in normal operation)
		peakHoldDuration = 50 * time.Millisecond
	}

	now := time.Now()
	absJerk := signal.Abs(rawJerk)
	absAmplitude := signal.Abs(processedAmplitude)

	// Activate peak hold when large jerk occurs with significant amplitude
	if absJerk > jerkThreshold && absAmplitude > minAmplitudeThreshold {
		a.jerkPeakHold = absAmplitude
		a.jerkPeakHoldTime = now

		return processedAmplitude
	}

	// Check if peak hold is active
	if a.jerkPeakHoldTime.IsZero() {
		return processedAmplitude
	}

	timeSinceHold := now.Sub(a.jerkPeakHoldTime)

	// Peak hold expired, reset and return current amplitude
	if timeSinceHold > peakHoldDuration {
		a.jerkPeakHold = 0
		a.jerkPeakHoldTime = time.Time{}

		return processedAmplitude
	}

	// Calculate blend factor based on time progression through hold duration
	// Start at 1.0 (100% peak hold) and decay to 0.0 (100% current amplitude)
	// This allows gradual mix-in of other haptics rather than reducing amplitude
	blendProgress := float64(timeSinceHold) / float64(peakHoldDuration)
	peakHoldWeight := 1.0 - blendProgress
	currentWeight := blendProgress

	// Within hold duration: detect inverse jerk pattern
	jerkSignChanged := a.detectInverseJerk(rawJerk)

	// Blend between peak hold and current amplitude if inverse jerk detected
	if jerkSignChanged && absAmplitude < a.jerkPeakHold {
		// Mix the peak hold with the current amplitude based on decay progress
		blendedAmplitude := (a.jerkPeakHold * peakHoldWeight) + (absAmplitude * currentWeight)

		return blendedAmplitude
	}

	// Update peak if current amplitude is higher
	if absAmplitude > a.jerkPeakHold {
		a.jerkPeakHold = absAmplitude
		a.jerkPeakHoldTime = now
	}

	return processedAmplitude
}

// detectInverseJerk checks if the jerk sign has changed from last frame.
func (a *App) detectInverseJerk(currentJerk float64) bool { //nolint:unused // peak-hold for planned inverse-jerk detection; deliberately kept
	lastJerk := a.kinematics.Last.SixDOFTranslationCalc.Jerk

	return (lastJerk > 0 && currentJerk < 0) || (lastJerk < 0 && currentJerk > 0)
}

func (a *App) calculateChassisHapticPulseFrequency() float64 {
	snap := signal.LargestMagnitude(
		a.kinematics.Current.ResolvedTransSnap,
		a.kinematics.Current.ResolvedRotSnap,
	)

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, a.config.GetHapticsSnapCurve()/1000))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, a.config.GetHapticsSnapScale())
	pulseFrequencyHz := a.config.GetHapticePulseFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < a.config.GetHapticsPulseMinHz() {
		pulseFrequencyHz = a.config.GetHapticsPulseMinHz()
	} else if pulseFrequencyHz > a.config.GetHapticsPulseMaxHz() {
		pulseFrequencyHz = a.config.GetHapticsPulseMaxHz()
	}

	return pulseFrequencyHz
}
