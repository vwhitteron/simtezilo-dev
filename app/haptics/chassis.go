package haptics

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

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

// Chassis generates the chassis impact pulse for every routed output channel,
// writing each channel's pulse into the synth. Jerk sets the amplitude, snap the
// frequency, and ground speed the ring-down length.
func (g *Generator) Chassis() {
	if g.cfg.GetSynthChassisMute() {
		return
	}

	startTime := time.Now()

	pulseFrequencyHz := g.calculateChassisHapticPulseFrequency()

	pulseAmplitude, unclampedAmplitude := g.calculateChassisHapticPulseAmplitude()

	sampleRate := float64(g.cfg.GetSynthInternalSampleRateHz())
	minSamplesPerFrame := int(sampleRate / hapticFrameRate)

	numChannels := g.synth.NumOutputChannels()

	// Size the per-channel telemetry slices to the configured channel count.
	// Channels the chassis is not routed to keep their previous value, matching
	// the prior fixed-array behaviour.
	g.kin.Current.SynthChannelAmplitude = ensureChannelLen(g.kin.Current.SynthChannelAmplitude, numChannels)
	g.kin.Current.SynthChannelFrequency = ensureChannelLen(g.kin.Current.SynthChannelFrequency, numChannels)

	// Generate pulse buffers for each channel
	for channel := range numChannels {
		// Skip channels the chassis source is not routed to; no point generating
		// a per-channel buffer the mixer will not consume.
		if !g.cfg.GetSynthRouteEnabled(synthesizer.ChannelChassis, channel) {
			continue
		}

		channelFreqHz, channelAmplitude, drxActive := g.applyDRX(
			pulseFrequencyHz, pulseAmplitude, unclampedAmplitude, channel,
		)

		channelPulseWidth := math.Round(sampleRate / (2 * channelFreqHz))

		if !drxActive {
			channelAmplitude = signal.Equalize(channelAmplitude, channelPulseWidth, channel, g.cfg)
		}

		// Store per-channel output for telemetry charting (absolute amplitude for graphing)
		g.kin.Current.SynthChannelAmplitude[channel] = signal.Abs(channelAmplitude)
		g.kin.Current.SynthChannelFrequency[channel] = channelFreqHz

		pulseBuffer := g.pulseWaveform(channelAmplitude, channelPulseWidth, minSamplesPerFrame)

		// Write to the channel-specific chassis buffer
		g.synth.WriteBuffer(synthesizer.ChassisChannelName(channel), pulseBuffer, 0)
	}

	// Calculate shared pulse metrics for peak hold using the base frequency
	pulseWidth := math.Round(sampleRate / (2 * pulseFrequencyHz))
	pulseLength := int(pulseWidth * 2)
	pulseDuration := time.Duration(float64(pulseLength)/sampleRate*1000) * time.Millisecond
	g.jerkPeakHoldDuration = pulseDuration

	// log large amplitude values
	if pulseAmplitude > 1.0 || pulseAmplitude < -1.0 {
		g.log.Debug().
			Float64("jerk", g.kin.Current.SixDOFTranslationCalc.Jerk).
			Float64("snap", g.kin.Current.SixDOFTranslationCalc.Snap).
			Str("process_time", time.Since(startTime).String()).
			Uint32("sequence_id", g.kin.Current.SequenceID).
			Msg("Bump inputs")
		g.log.Debug().
			Float64("amplitude", pulseAmplitude).
			Float64("pulseWidth", pulseWidth).
			Msg("Bump outputs")
	}
}

// chassisScratch returns the reusable per-tick pulse buffer sized to length,
// zero-filled. The generation loops below write only a prefix of it, so the tail
// must be cleared explicitly rather than relying on a fresh allocation.
func (g *Generator) chassisScratch(length int) []float64 {
	if cap(g.chassisPulseScratch) < length {
		g.chassisPulseScratch = make([]float64, length)
	} else {
		g.chassisPulseScratch = g.chassisPulseScratch[:length]
	}

	for index := range g.chassisPulseScratch {
		g.chassisPulseScratch[index] = 0
	}

	return g.chassisPulseScratch
}

// pulseWaveform builds the chassis waveform: a single unipolar raised sine
// spanning one period of the pulse frequency, with no ring-down. Note that it
// is mostly DC by energy — the offset is two thirds of the pulse's power.
func (g *Generator) pulseWaveform(amplitude, pulseWidth float64, minSamplesPerFrame int) []float64 {
	pulseLength := int(pulseWidth * 2)
	buffer := g.chassisScratch(max(pulseLength, minSamplesPerFrame))

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	for index := range buffer {
		if index > pulseLength {
			break
		}

		phase := waveSamplePeriod * (float64(index) - waveOffset)
		buffer[index] = ((amplitude * math.Sin(phase)) + amplitude) / 2
	}

	return buffer
}

// applyDRX checks for DRX activation on the given channel, shifting frequency into
// EQ-attenuated ranges for high impact events.
// Returns the shifted frequency, amplitude, and whether DRX was activated.
// When DRX is not active the original frequency and amplitude are returned with active=false.
func (g *Generator) applyDRX(
	pulseFrequencyHz, pulseAmplitude, unclampedAmplitude float64,
	channel int,
) (freqHz, amplitude float64, active bool) {
	drxFreq, drxAmp, drxBucketRatio, active := signal.DRXShift(
		pulseFrequencyHz, unclampedAmplitude, channel, g.cfg,
	)
	if !active {
		return pulseFrequencyHz, pulseAmplitude, false
	}

	eqAttenDB := signal.AmplitudeToDecibels(drxBucketRatio)
	desiredBoostDB := signal.AmplitudeToDecibels(unclampedAmplitude / g.cfg.GetHapticsPulseMaxAmplitude())

	g.log.Debug().
		Float64("original_freq", pulseFrequencyHz).
		Float64("drx_freq", drxFreq).
		Float64("eq_atten_db", eqAttenDB).
		Float64("desired_boost_db", desiredBoostDB).
		Float64("drx_amplitude", drxAmp).
		Float64("jerk", g.kin.Current.SixDOFTranslationCalc.Jerk).
		Int("channel", channel).
		Msg("DRX activated for chassis haptic")

	return drxFreq, drxAmp, true
}

// calculateChassisHapticPulseAmplitude returns the clamped pulse amplitude and the
// unclamped amplitude DRX uses to decide whether to exploit device resonance.
func (g *Generator) calculateChassisHapticPulseAmplitude() (
	pulseAmplitude float64, unclampedAmplitude float64,
) {
	jerk := signal.LargestMagnitude(
		g.kin.Current.ResolvedTransJerk,
		g.kin.Current.ResolvedRotJerk,
	)

	// The amplitude keeps the jerk's sign. Both waveforms lead with the same unipolar
	// bump, so the alternating polarity is what stops a train of them accumulating a
	// large DC offset — driving them from the magnitude instead makes every bump push
	// the same way and buries the output under DC. The sign costs some sustained
	// energy where overlapping bumps of opposite polarity subtract, but that is the
	// cheaper trade: the bump's low-frequency weight is what gives the pulse its body.
	pulseAmplitude = signal.Exponent(jerk, g.cfg.GethapticsJerkCurve()/1000)
	pulseAmplitude = signal.Scale(pulseAmplitude, g.cfg.GetHapticsJerkScale())

	unclampedAmplitude = signal.Abs(pulseAmplitude)

	pulseAmplitude, _ = signal.LimitMax(pulseAmplitude, g.cfg.GetHapticsPulseMaxAmplitude())

	// Apply peak hold to the processed amplitude to prevent cancellation from inverse jerks
	// pulseAmplitude = g.applyJerkPeakHold(jerk, pulseAmplitude)

	return pulseAmplitude, unclampedAmplitude
}

// applyJerkPeakHold implements peak hold with decay to prevent waveform cancellation.
// When a large jerk occurs (e.g., >2000), it's often followed by an inverse jerk that would
// cancel out the haptic feedback. This function detects inverse jerk patterns and holds the
// amplitude to maintain the impact sensation.
func (g *Generator) applyJerkPeakHold(rawJerk, processedAmplitude float64) float64 { //nolint:unused // peak-hold for planned inverse-jerk detection; deliberately kept
	const jerkThreshold = 2000.0

	const minAmplitudeThreshold = 0.3

	// Use the dynamic peak hold duration based on current pulse length
	peakHoldDuration := g.jerkPeakHoldDuration
	if peakHoldDuration == 0 {
		// Fallback to 50ms if not set (shouldn't happen in normal operation)
		peakHoldDuration = 50 * time.Millisecond
	}

	now := time.Now()
	absJerk := signal.Abs(rawJerk)
	absAmplitude := signal.Abs(processedAmplitude)

	// Activate peak hold when large jerk occurs with significant amplitude
	if absJerk > jerkThreshold && absAmplitude > minAmplitudeThreshold {
		g.jerkPeakHold = absAmplitude
		g.jerkPeakHoldTime = now

		return processedAmplitude
	}

	// Check if peak hold is active
	if g.jerkPeakHoldTime.IsZero() {
		return processedAmplitude
	}

	timeSinceHold := now.Sub(g.jerkPeakHoldTime)

	// Peak hold expired, reset and return current amplitude
	if timeSinceHold > peakHoldDuration {
		g.jerkPeakHold = 0
		g.jerkPeakHoldTime = time.Time{}

		return processedAmplitude
	}

	// Calculate blend factor based on time progression through hold duration
	// Start at 1.0 (100% peak hold) and decay to 0.0 (100% current amplitude)
	// This allows gradual mix-in of other haptics rather than reducing amplitude
	blendProgress := float64(timeSinceHold) / float64(peakHoldDuration)
	peakHoldWeight := 1.0 - blendProgress
	currentWeight := blendProgress

	// Within hold duration: detect inverse jerk pattern
	jerkSignChanged := g.detectInverseJerk(rawJerk)

	// Blend between peak hold and current amplitude if inverse jerk detected
	if jerkSignChanged && absAmplitude < g.jerkPeakHold {
		// Mix the peak hold with the current amplitude based on decay progress
		blendedAmplitude := (g.jerkPeakHold * peakHoldWeight) + (absAmplitude * currentWeight)

		return blendedAmplitude
	}

	// Update peak if current amplitude is higher
	if absAmplitude > g.jerkPeakHold {
		g.jerkPeakHold = absAmplitude
		g.jerkPeakHoldTime = now
	}

	return processedAmplitude
}

// detectInverseJerk checks if the jerk sign has changed from last frame.
func (g *Generator) detectInverseJerk(currentJerk float64) bool { //nolint:unused // peak-hold for planned inverse-jerk detection; deliberately kept
	lastJerk := g.kin.Last.SixDOFTranslationCalc.Jerk

	return (lastJerk > 0 && currentJerk < 0) || (lastJerk < 0 && currentJerk > 0)
}

func (g *Generator) calculateChassisHapticPulseFrequency() float64 {
	snap := signal.LargestMagnitude(
		g.kin.Current.ResolvedTransSnap,
		g.kin.Current.ResolvedRotSnap,
	)

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, g.cfg.GetHapticsSnapCurve()/1000))
	pulseFrequencyScaler = signal.Scale(pulseFrequencyScaler, g.cfg.GetHapticsSnapScale())
	pulseFrequencyHz := g.cfg.GetHapticePulseFrequencyHzRange() * pulseFrequencyScaler

	if pulseFrequencyHz < g.cfg.GetHapticsPulseMinHz() {
		pulseFrequencyHz = g.cfg.GetHapticsPulseMinHz()
	} else if pulseFrequencyHz > g.cfg.GetHapticsPulseMaxHz() {
		pulseFrequencyHz = g.cfg.GetHapticsPulseMaxHz()
	}

	return pulseFrequencyHz
}
