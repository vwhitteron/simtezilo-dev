package app

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

// engineGenParams carries the per-tick inputs to the engine waveform generator.
// Frequency enters as pulseRate (converted to a per-sample phase increment) and
// level as amplitude (ramped from the previous block), so both can change every
// tick without introducing a seam.
type engineGenParams struct {
	amplitude      float64 // target peak amplitude for this block
	pulseRate      float64 // pulses per second
	pulseDutyCycle float64 // fraction of each pulse cycle the pulse occupies (0..1)
	rpmPercent     float64 // rpm / revLimit, for roughness scaling
	revLimit       float64 // engine rev limit, for reconstructing rpm in roughness
}

// engineWaveformGenerator produces the engine haptic waveform as a pure function
// of a carried, monotonic phase, so consecutive generated blocks join without a
// discontinuity. It replaces the previous per-tick "regenerate from pulse-phase 0
// then splice with a zero-crossing stitch" design, whose stitch — combined with
// the channel depth cap — left a stale-gap discontinuity at every tick boundary,
// audible as pops/clicks that worsened with RPM.
type engineWaveformGenerator struct {
	sampleRate float64
	geometry   string
	engine     *haptics.EngineProfile

	pulsePhase    float64 // accumulated pulse cycles (fractional)
	pulsePolarity bool    // current pulse polarity; flips at each pulse boundary
	lastCycle     int64   // integer cycle index of the previous sample
	hasCycle      bool    // false before the first sample (suppresses a startup flip)
	lastAmplitude float64 // amplitude at the end of the previous block, for ramping
	primed        bool    // false until the first block (suppresses the startup ramp)
	samplePos     uint64  // monotonic output-sample counter, for roughness modulation
}

// newEngineWaveformGenerator returns a generator for the given output sample rate,
// engine geometry code ("K" Wankel, "S" two-stroke, anything else four-stroke)
// and engine profile.
func newEngineWaveformGenerator(sampleRate int, geometry string, engine *haptics.EngineProfile) *engineWaveformGenerator {
	return &engineWaveformGenerator{
		sampleRate: float64(sampleRate),
		geometry:   geometry,
		engine:     engine,
	}
}

// Generate fills dst with the next len(dst) samples of the engine waveform,
// advancing the carried phase, polarity, sample position and amplitude. Amplitude
// is ramped linearly from the previous block's amplitude to params.amplitude across
// dst so a per-tick level change does not step at the seam; the first block uses
// params.amplitude flat. Because pulseRate enters as a phase increment, a per-tick
// frequency change stays phase-continuous.
func (g *engineWaveformGenerator) Generate(dst []float64, params engineGenParams) {
	if len(dst) == 0 {
		return
	}

	startAmp := g.lastAmplitude
	if !g.primed {
		startAmp = params.amplitude
		g.primed = true
	}

	phaseInc := 0.0
	if g.sampleRate > 0 {
		phaseInc = params.pulseRate / g.sampleRate
	}

	n := float64(len(dst))

	for i := range dst {
		amp := startAmp + (params.amplitude-startAmp)*(float64(i)/n)
		dst[i] = amp * g.pulseValue(params, phaseInc)
		g.samplePos++
	}

	g.lastAmplitude = params.amplitude
}

// pulseValue returns the unscaled waveform value at the current phase, updates the
// alternating polarity at pulse-cycle boundaries, then advances the phase by one
// sample. It mirrors the old calculatePulseValueAtIndex but reads the carried
// phase accumulator instead of a per-tick sample index.
func (g *engineWaveformGenerator) pulseValue(params engineGenParams, phaseInc float64) float64 {
	cycle := int64(math.Floor(g.pulsePhase))
	if g.hasCycle && cycle != g.lastCycle {
		g.pulsePolarity = !g.pulsePolarity
	}

	g.lastCycle = cycle
	g.hasCycle = true

	phaseInPulse := g.pulsePhase - math.Floor(g.pulsePhase) // 0..1 within the cycle

	g.pulsePhase += phaseInc

	if phaseInPulse >= params.pulseDutyCycle {
		return 0.0 // in the gap between pulses
	}

	value := pulseShapeByGeometry(g.geometry, phaseInPulse/params.pulseDutyCycle, g.engine)

	if !g.pulsePolarity {
		value = -value
	}

	value = g.applyRoughness(value, params)

	value, _ = signal.LimitWindow(value, -1.0, 1.0)

	return value
}

// applyRoughness reproduces applyPulseRoughnessVariation, keyed off the monotonic
// sample position rather than the old telemetry-sequence + per-tick index so the
// modulation stays continuous across blocks.
func (g *engineWaveformGenerator) applyRoughness(value float64, params engineGenParams) float64 {
	secondaryImbalance := 1.0 - g.engine.SecondaryBalance
	rpm := params.rpmPercent * params.revLimit

	if rpm <= 2400.0 && secondaryImbalance > 0.02 {
		roughnessPhase := float64(g.samplePos) * 0.0005
		variation := 1.0 + (math.Sin(roughnessPhase) * secondaryImbalance * 0.3)
		value *= variation
	}

	return value
}

// pulseShapeByGeometry selects the per-pulse shape for the engine geometry. It is
// the free-function form of the old Apparams.generatePulseValueByGeometry, reusing the
// same per-geometry pulse functions.
func pulseShapeByGeometry(geometry string, phaseNormalized float64, engine *haptics.EngineProfile) float64 {
	switch geometry {
	case "K":
		return generatePulseWankel(phaseNormalized, engine)
	case "S":
		return generatePulseTwoStroke(phaseNormalized, engine)
	default:
		return generatePulseFourStroke(phaseNormalized, engine)
	}
}
