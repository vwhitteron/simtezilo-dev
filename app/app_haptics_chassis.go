package app

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

func (a *App) generateChassisHaptic() {
	if a.config.GetChassisGain() <= config.MinimumGain {
		return
	}

	startTime := time.Now()

	pulseFrequencyHz := a.calculateChassisHapticPulseFrequency()

	pulseWidth := math.Round(float64(a.config.GetInternalSampleRateHz()) / (2 * pulseFrequencyHz))

	pulseAmplitude := a.calculateChassisHapticPulseAmplitude()

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	// Calculate buffer size based on the actual pulse waveform length
	// The pulse needs pulseWidth * 2 samples for a complete cycle
	// Low frequency pulses (8Hz) need ~1000 samples (125ms)
	// High frequency pulses (60Hz) need ~134 samples (17ms)
	pulseLength := int(pulseWidth * 2)

	// Calculate the duration of this pulse for peak hold
	// This ensures peak hold matches the impact duration
	pulseDuration := time.Duration(float64(pulseLength)/float64(a.config.GetInternalSampleRateHz())*1000) * time.Millisecond
	a.jerkPeakHoldDuration = pulseDuration

	// Ensure minimum buffer size for very high frequency pulses
	sampleRate := float64(a.config.GetInternalSampleRateHz())
	minSamplesPerFrame := int(sampleRate / hapticFrameRate)

	// Use the larger of: complete pulse length or minimum frame size
	// This allows low-frequency pulses to be generated completely
	bufferSize := max(pulseLength, minSamplesPerFrame)

	pulseBuffer := make([]float64, bufferSize)

	// bufferLen := a.synth.GetBufferLength()
	// pulseBuffer := make([]float64, bufferLen)

	// Generate the complete pulse waveform
	// for i := range int(pulseWidth * 2) {
	for index := range bufferSize {
		if index > pulseLength {
			break
		}

		phase := waveSamplePeriod * (float64(index) - waveOffset)
		pulseBuffer[index] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
	}

	a.synth.WriteBuffer("chassis", pulseBuffer, 0)

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
	jerk := signal.LargestMagnitude(
		a.kinematics.Current.SixDOFTranslationCalc.Jerk,
		a.kinematics.Current.SixDOFRotation.Jerk,
	)

	// Process the signal normally first
	pulseAmplitude := signal.Exponent(jerk, a.config.GetJerkCurve()/1000)
	pulseAmplitude = signal.Scale(pulseAmplitude, a.config.GetJerkScale())

	p1 := pulseAmplitude

	pulseAmplitude, wasLimited := signal.LimitMax(pulseAmplitude, a.config.GetPulseMaxAmplitude())
	if wasLimited {
		a.log.Debug().Float64("pulse", p1).Msg("limiter")
	}

	// Apply peak hold to the processed amplitude to prevent cancellation from inverse jerks
	pulseAmplitude = a.applyJerkPeakHold(jerk, pulseAmplitude)

	a.kinematics.Last.SynthOutputAmplitude = a.kinematics.Current.SynthOutputAmplitude
	a.kinematics.Current.SynthOutputAmplitude = pulseAmplitude

	return pulseAmplitude
}

// applyJerkPeakHold implements peak hold with decay to prevent waveform cancellation.
// When a large jerk occurs (e.g., >2000), it's often followed by an inverse jerk that would
// cancel out the haptic feedback. This function detects inverse jerk patterns and holds the
// amplitude to maintain the impact sensation.
func (a *App) applyJerkPeakHold(rawJerk, processedAmplitude float64) float64 {
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
func (a *App) detectInverseJerk(currentJerk float64) bool {
	lastJerk := a.kinematics.Last.SixDOFTranslationCalc.Jerk

	return (lastJerk > 0 && currentJerk < 0) || (lastJerk < 0 && currentJerk > 0)
}

func (a *App) calculateChassisHapticPulseFrequency() float64 {
	snap := signal.LargestMagnitude(
		a.kinematics.Current.SixDOFTranslationCalc.Snap,
		a.kinematics.Current.SixDOFRotation.Snap,
	)

	pulseFrequencyScaler := signal.Abs(signal.Exponent(snap, a.config.GetSnapCurve()/1000))
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
