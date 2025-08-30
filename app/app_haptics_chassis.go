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

	// Ensure minimum buffer size for very high frequency pulses
	sampleRate := float64(a.config.GetInternalSampleRateHz())
	minSamplesPerFrame := int(sampleRate / hapticFrameRate)

	// Use the larger of: complete pulse length or minimum frame size
	// This allows low-frequency pulses to be generated completely
	bufferSize := pulseLength
	if bufferSize < minSamplesPerFrame {
		bufferSize = minSamplesPerFrame
	}

	pulseBuffer := make([]float64, bufferSize)

	// bufferLen := a.synth.GetBufferLength()
	// pulseBuffer := make([]float64, bufferLen)

	// Generate the complete pulse waveform
	// for i := range int(pulseWidth * 2) {
	for i := range bufferSize {
		if i > pulseLength {
			break
		}
		phase := waveSamplePeriod * (float64(i) - waveOffset)
		pulseBuffer[i] = ((pulseAmplitude * math.Sin(phase)) + pulseAmplitude) / 2
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
	sig := signal.LargestMagnitude(a.kinematics.Current.SixDOFTranslationCalc.Jerk, (a.kinematics.Current.SixDOFRotation.Jerk * snapMultiplier))
	pulseAmplitude := signal.Exponent(sig, a.config.GetJerkCurve())
	pulseAmplitude = signal.Scale(pulseAmplitude, a.config.GetJerkScale())

	p1 := pulseAmplitude
	pulseAmplitude, wasLimited := signal.LimitMax(pulseAmplitude, a.config.GetPulseMaxAmplitude())
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
