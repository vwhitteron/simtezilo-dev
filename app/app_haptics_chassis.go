package app

import (
	"math"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

func (a *App) generateChassisHaptic() {
	startTime := time.Now()

	pulseFrequencyHz := a.calculateChassisHapticPulseFrequency()

	pulseWidth := math.Round(float64(a.config.Synthesizer.SampleRateHz) / (2 * pulseFrequencyHz))

	pulseAmplitude := a.calculateChassisHapticPulseAmplitude()

	waveOffset := pulseWidth / 2
	waveSamplePeriod := math.Pi / pulseWidth

	bufferLen := a.synth.GetBufferLength()
	pulseBuffer := make([]float64, bufferLen)

	for i := range int(pulseWidth * 2) {
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
