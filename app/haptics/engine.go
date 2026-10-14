// engine.go holds the engine haptic generator, extracted from package app so that
// offline tooling can drive the real generator against a recorded replay without
// linking an audio backend. App now delegates to an EngineGenerator, so there is a
// single source of truth.
//
// The vehicle's engine-characteristics derivation (firing frequency, pulse overlap,
// profile lookup) stayed in package app: it builds the vehicle, and the generator only
// consumes the result.
//
// The package godoc lives in generator.go.

package haptics

import (
	"math"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics/profiles"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
)

// engineCushionFrames is the engine channel's target unread depth, in engine
// frames. Each tick tops the channel back up to this depth with freshly
// generated, phase-continuous samples; a few frames is enough to ride out a
// handful of dropped/late telemetry frames without accumulating latency. Because
// the generator is phase-continuous, no zero-crossing stitch or depth cap is
// needed — successive blocks join seamlessly.
const engineCushionFrames = 3

// EngineGenerator renders the engine haptic onto the synthesizer's engine channel.
//
// One generator serves one vehicle at a time; SetVehicle switches it. It carries the
// waveform phase across ticks, so it is not safe for concurrent use: the live app
// drives it from the main loop only, and scratch is reused across ticks.
type EngineGenerator struct {
	cfg   *config.Config
	synth *synthesizer.Synthesizer
	tel   TelemetryFunc

	// log is held for parity with the other generators, which do log. The engine path
	// emits nothing today: it runs at 30 Hz, so any per-tick line would flood.
	log zerolog.Logger

	veh vehicle.Characteristics

	// seq is the telemetry sequence number of the frame in flight. It drives the
	// cached-RPM fallback and the roughness modulation phase.
	seq uint32

	lastSeq       uint32                   // Last sequence ID for engine haptics
	lastKnownRPM  float64                  // Cache last known RPM for fallback
	lastEventTime time.Time                // Timestamp of last engine haptic event
	generator     *engineWaveformGenerator // Phase-continuous engine waveform generator

	// scratch is the reusable per-tick pulse buffer. Main-loop goroutine only.
	scratch []float64
}

// NewEngineGenerator builds a generator ready to drive the engine channel. Call
// SetVehicle before the first tick, so the firing frequency and profile match the
// vehicle.
func NewEngineGenerator(
	cfg *config.Config,
	synth *synthesizer.Synthesizer,
	tel TelemetryFunc,
	logger zerolog.Logger,
) *EngineGenerator {
	return &EngineGenerator{cfg: cfg, synth: synth, tel: tel, log: logger}
}

// SetVehicle points the generator at a new vehicle's engine characteristics. The
// carried waveform phase is deliberately kept, so a vehicle change does not step the
// waveform.
func (g *EngineGenerator) SetVehicle(characteristics vehicle.Characteristics) {
	g.veh = characteristics
}

// Reset clears the carried RPM and waveform state, so a new session does not inherit
// a stale phase or a stale cached RPM.
func (g *EngineGenerator) Reset() {
	g.lastSeq = 0
	g.lastKnownRPM = 0
	g.lastEventTime = time.Time{}
	g.generator = nil
}

// LastRPM returns the RPM the last tick used, which the capture harness reports
// alongside each engine frame.
func (g *EngineGenerator) LastRPM() float64 {
	return g.lastKnownRPM
}

// LastSeq returns the telemetry sequence number the last tick consumed. A tick whose
// sequence has not advanced past this value falls back to the cached RPM.
func (g *EngineGenerator) LastSeq() uint32 {
	return g.lastSeq
}

// Generate tops the engine channel back up to its cushion with freshly generated,
// phase-continuous waveform. Each tick it appends only as many samples as the channel
// has drained since the last tick, so the buffered depth stays at the cushion (bounded
// latency) and successive blocks join seamlessly (no stitch, no stale-gap click).
//
// seq is the telemetry sequence number in flight. The caller gates this call on the
// haptics-enabled and calibration states, which the app owns.
func (g *EngineGenerator) Generate(seq uint32) {
	g.seq = seq

	if !g.shouldGenerate() {
		return
	}

	rpm := g.currentRPM()
	if rpm < 0 {
		return // RPM unavailable due to telemetry timeout
	}

	need := g.refillSamples()
	if need <= 0 {
		return // channel already at cushion depth
	}

	// Reuse the scratch buffer across ticks, growing it on demand.
	if cap(g.scratch) < need {
		g.scratch = make([]float64, need)
	} else {
		g.scratch = g.scratch[:need]
	}

	engineBuffer := g.scratch

	for index := range engineBuffer {
		engineBuffer[index] = 0
	}

	// No haptics when the engine is not running: feed silence to keep the channel
	// fed and hold the generator phase so it restarts in phase.
	if rpm == 0 {
		g.appendBuffer(engineBuffer)

		return
	}

	engineRoughness := g.roughness(rpm)
	params := g.waveformParams(rpm, engineRoughness)

	g.waveform().Generate(engineBuffer, engineGenParams{
		amplitude:      params.amplitude,
		pulseRate:      params.pulseRate,
		pulseDutyCycle: params.pulseDutyCycle,
		rpmPercent:     params.rpmPercent,
		revLimit:       float64(g.veh.RevLimit),
	})

	g.appendBuffer(engineBuffer)
}

// engineRefillSamples returns how many samples to generate this tick to restore
// the channel to its cushion depth, clamped to at most two frames so a long stall
// cannot produce an oversized block.
func (g *EngineGenerator) refillSamples() int {
	samplesPerFrame := g.synth.GetSampleRate() / engineHapticFrameRate

	need := max(samplesPerFrame*engineCushionFrames-g.synth.ChannelDepth(synthesizer.ChannelEngine), 0)

	if maxBlock := samplesPerFrame * 2; need > maxBlock {
		need = maxBlock
	}

	return need
}

// appendEngineBuffer appends the generated samples contiguously at the channel's
// write cursor (offset 0, no overwrite of unread audio), so the new block joins
// the previous one without a seam.
func (g *EngineGenerator) appendBuffer(engineBuffer []float64) {
	g.synth.OverwriteBuffer(synthesizer.ChannelEngine, engineBuffer, 0)
}

// engineGenerator returns the engine waveform generator, creating it on first use
// and recreating it if the sample rate changes. The current geometry and profile
// are refreshed each tick so a vehicle change takes effect without resetting the
// carried phase.
func (g *EngineGenerator) waveform() *engineWaveformGenerator {
	rate := g.synth.GetSampleRate()

	gen := g.generator
	if gen == nil || gen.sampleRate != float64(rate) {
		gen = newEngineWaveformGenerator(rate, g.veh.Engine.Geometry, g.veh.Engine.Haptics)
		g.generator = gen

		return gen
	}

	gen.geometry = g.veh.Engine.Geometry
	gen.engine = g.veh.Engine.Haptics

	return gen
}

// shouldGenerate reports whether this vehicle and configuration produce an engine
// haptic at all.
//
// The calibration and haptics-enabled gates are deliberately absent: they are app
// session state, so the caller applies them before it calls Generate, exactly as it
// does for the chassis layer.
func (g *EngineGenerator) shouldGenerate() bool {
	if g.cfg.GetSynthEngineMute() {
		return false
	}

	// No engine haptics configured
	if g.veh.Engine.FiringFrequency == 0 {
		return false
	}

	return true
}

// getCurrentRPM gets the current RPM, managing cache for telemetry fallback.
func (g *EngineGenerator) currentRPM() float64 {
	rpm := float64(g.tel().EngineRPM())
	currentTime := time.Now()

	switch {
	case g.seq > g.lastSeq:
		g.lastKnownRPM = rpm
		g.lastEventTime = currentTime
		g.lastSeq = g.seq

		return rpm
	case currentTime.Sub(g.lastEventTime) > 1000*time.Millisecond:
		// stop engine haptics if no telemetry received for 1 second or more
		return -1
	default:
		// Use cached RPM if telemetry is unavailable
		return g.lastKnownRPM
	}
}

// calculateEngineRoughness calculates a roughness value based on the engine geometry and a given RPM.
func (g *EngineGenerator) roughness(rpm float64) float64 {
	var engineRoughness float64

	switch g.veh.Engine.Geometry {
	case "K":
		roughnessPhase := float64(g.seq) * 0.003
		apexSealRoughness := (1.0 - g.veh.Engine.Haptics.PrimaryBalance) * 0.08
		housingEccentricity := (1.0 - g.veh.Engine.Haptics.SecondaryBalance) * 0.05
		roughnessIntensity := apexSealRoughness + housingEccentricity*0.7

		// Wankels get smoother at higher RPM due to improved sealing
		rpmSmoothingFactor := math.Min(rpm/4000.0, 1.0)
		engineRoughness = math.Sin(roughnessPhase) * roughnessIntensity * (1.0 - rpmSmoothingFactor*0.8)

		// Add characteristic Wankel "chatter" at low RPM
		if rpm < 2000.0 {
			chatterPhase := float64(g.seq) * 0.008
			chatterIntensity := (1.0 - g.veh.Engine.Haptics.SecondaryBalance) * 0.03
			engineRoughness += math.Sin(chatterPhase) * chatterIntensity * (1.0 - rpm/2000.0)
		}
	case "S":
		// 2-stroke engines have characteristic roughness due to scavenging process
		roughnessPhase := float64(g.seq) * 0.007
		scavenging := (1.0 - g.veh.Engine.Haptics.PrimaryBalance) * 0.20
		exhaustBlowdown := (1.0 - g.veh.Engine.Haptics.SecondaryBalance) * 0.12
		intakeExhaustoverlap := 0.6
		baseRoughness := scavenging + exhaustBlowdown*intakeExhaustoverlap

		// Add characteristic 2-stroke "buzz" - more intense at mid RPM
		rpmFactor := math.Min(rpm/6000.0, 1.0)
		buzzIntensity := baseRoughness * (0.5 + rpmFactor*0.8)

		engineRoughness = math.Sin(roughnessPhase)*buzzIntensity + math.Sin(roughnessPhase*2.3)*buzzIntensity*0.4

		// Add port timing irregularities at low RPM
		if rpm < 3000.0 {
			portPhase := float64(g.seq) * 0.012
			portIrregularity := (1.0 - g.veh.Engine.Haptics.SecondaryBalance) * 0.08
			engineRoughness += math.Sin(portPhase) * portIrregularity * (1.0 - rpm/3000.0)
		}

		// 2-strokes get slightly smoother at very high RPM due to better scavenging
		if rpm > 4000.0 {
			smoothing := math.Min((rpm-4000.0)/4000.0, 0.3)
			engineRoughness *= (1.0 - smoothing)
		}
	default:
		// Default to 4-stroke engine characteristics
		if rpm <= 2400.0 {
			roughnessPhase := float64(g.seq) * 0.005
			// Poor primary balance creates more low-frequency roughness
			primaryRoughness := (1.0 - g.veh.Engine.Haptics.PrimaryBalance) * 0.15
			// Poor secondary balance creates more high-frequency roughness
			secondaryRoughness := (1.0 - g.veh.Engine.Haptics.SecondaryBalance) * 0.08
			roughnessIntensity := primaryRoughness + secondaryRoughness*0.5
			engineRoughness = math.Sin(roughnessPhase)*roughnessIntensity + math.Sin(roughnessPhase*1.7)*roughnessIntensity*0.5

			// Smooth out roughness as RPM increases
			rpmSmoothingFactor := rpm / 2400.0
			engineRoughness *= (1.0 - rpmSmoothingFactor*g.veh.Engine.Haptics.PrimaryBalance)
		} else {
			// High RPM: roughness based on engine balance characteristics
			if g.veh.Engine.Haptics.PrimaryBalance < 0.9 {
				roughnessPhase := float64(g.seq) * 0.002
				highRpmRoughness := (1.0 - g.veh.Engine.Haptics.PrimaryBalance) * 0.02 // Poor primary balance creates roughness
				engineRoughness = math.Sin(roughnessPhase) * highRpmRoughness
			} else {
				engineRoughness = 0.0 // Well-balanced engines are smooth at high RPM
			}
		}
	}

	return engineRoughness
}

// pulseWaveformParams holds all calculated parameters for pulse waveform generation.
type pulseWaveformParams struct {
	rpmPercent      float64
	throttlePercent float64
	amplitude       float64
	sampleRate      float64
	pulseRate       float64
	pulseDutyCycle  float64
}

// calculatePulseWaveformParams calculates all parameters needed for pulse waveform generation.
func (g *EngineGenerator) waveformParams(rpm, engineRoughness float64) pulseWaveformParams {
	rpmPercent := rpm / float64(g.veh.RevLimit)
	rpmPercent, _ = signal.LimitWindow(rpmPercent, 0.0, 1.0)

	throttlePercent := float64(g.tel().ThrottleOutputPercent()) / 100
	throttlePercent, _ = signal.LimitWindow(throttlePercent, 0.0, 1.0)

	vehicleTypeGain := g.vehicleTypeGainOffset()
	amplitude := g.pulseAmplitude(throttlePercent, engineRoughness, rpmPercent, vehicleTypeGain)

	sampleRate := float64(g.synth.GetSampleRate())
	pulseRate := rpm * g.veh.Engine.FiringFrequency * g.veh.Engine.Haptics.PulseScale
	pulseDutyCycle := g.veh.Engine.PulseOverlap + (rpmPercent * g.veh.Engine.PulseOverlap * 2)

	return pulseWaveformParams{
		rpmPercent:      rpmPercent,
		throttlePercent: throttlePercent,
		amplitude:       amplitude,
		sampleRate:      sampleRate,
		pulseRate:       pulseRate,
		pulseDutyCycle:  pulseDutyCycle,
	}
}

// getVehicleTypeGainOffset returns the gain offset based on vehicle type.
func (g *EngineGenerator) vehicleTypeGainOffset() float64 {
	switch g.veh.VehicleType {
	case vehicle.TypeRace:
		return 0.0
	case vehicle.TypeTuned:
		return -3.0
	case vehicle.TypeStreet:
		fallthrough
	default:
		return -4.75
	}
}

// calculatePulseAmplitude calculates the pulse amplitude based on engine characteristics.
func (g *EngineGenerator) pulseAmplitude(throttlePercent, engineRoughness, rpmPercent, vehicleTypeGain float64) float64 {
	engineLoadGainIncrease := 1.0
	engineLoadGain := (1 - throttlePercent) * engineLoadGainIncrease
	roughness := 1.0 - (engineRoughness * rpmPercent * 0.1)
	gain := g.veh.Engine.Haptics.Gain + vehicleTypeGain + engineLoadGain
	amplitude := signal.GainToPowerRatio(gain) * roughness
	amplitude, _ = signal.LimitWindow(amplitude, 0, 1)

	return amplitude
}

// generatePulswWankel creates a single pulse value for a Wankel engine based on a given phase value
// and engine geometry.
func generatePulseWankel(phase float64, engine *profiles.EngineProfile) (pulse float64) {
	if phase < 0.3 {
		// Quick attack (30% of pulse width)
		// Adjust attack characteristics based on engine balance
		// Poor balance = sharper attack, good balance = smoother attack
		attackSharpness := 1.0 - (engine.PrimaryBalance * 0.6) // 0.4 to 1.0 range
		attackPhase := phase / 0.3

		// Wankels have unique triangular rotor pulses - more gradual attack
		attackSharpness *= 0.7 // Reduce sharpness for Wankel characteristic
		pulse = math.Sin(attackPhase*math.Pi/2) * attackSharpness

		// Add slight rotor eccentricity modulation
		eccentricityFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.1*math.Sin(attackPhase*math.Pi*3)
		pulse *= eccentricityFactor
	} else {
		// Quick decay (70% of pulse width)
		// Adjust decay based on both primary and secondary balance
		decayPhase := (phase - 0.3) / 0.7

		// Wankel decay characteristics - smoother, more gradual
		primaryDecayRate := 3.0 + (1.0-engine.PrimaryBalance)*1.5
		secondaryDecayFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.3
		combinedDecayRate := primaryDecayRate * secondaryDecayFactor

		// Wankels have more gradual decay due to chamber expansion characteristics
		pulse = math.Exp(-decayPhase*combinedDecayRate) * (1.0 - decayPhase*0.1)
	}

	return pulse
}

// generatePulswTwoStroke creates a single pulse value for a 2-strok engine based on a given phase value
// and engine geometry.
func generatePulseTwoStroke(phase float64, engine *profiles.EngineProfile) (pulse float64) {
	if phase < 0.3 {
		// Quick attack (30% of pulse width)
		// Adjust attack characteristics based on engine balance
		// Poor balance = sharper attack, good balance = smoother attack
		attackSharpness := 1.0 - (engine.PrimaryBalance * 0.6) // 0.4 to 1.0 range
		attackPhase := phase / 0.3

		// 2-stroke engines have very sharp, aggressive attack due to rapid combustion
		attackSharpness *= 1.3 // Increase sharpness for 2-stroke characteristic

		// 2-strokes have a more explosive attack than 4-strokes
		pulse = math.Pow(math.Sin(attackPhase*math.Pi/2), attackSharpness*0.6)

		// Add port opening/closing noise during attack
		portNoise := (1.0 - engine.SecondaryBalance) * 0.15
		portPhase := attackPhase * math.Pi * 4 // Higher frequency port effects
		pulse += math.Sin(portPhase) * portNoise * attackPhase
	} else {
		// Quick decay (70% of pulse width)
		// Adjust decay based on both primary and secondary balance
		decayPhase := (phase - 0.3) / 0.7

		// 2-stroke decay characteristics - rapid but irregular due to exhaust blowdown
		primaryDecayRate := 5.0 + (1.0-engine.PrimaryBalance)*3.0
		secondaryDecayFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.8

		combinedDecayRate := primaryDecayRate * secondaryDecayFactor

		// 2-strokes have rapid decay with exhaust port effects
		baseDecay := math.Exp(-decayPhase * combinedDecayRate)

		// Add exhaust blowdown characteristics - creates a "ragged" decay
		blowdownIntensity := (1.0 - engine.SecondaryBalance) * 0.25
		blowdownPhase := decayPhase * math.Pi * 3 // Higher frequency for port effects
		blowdownEffect := math.Sin(blowdownPhase) * blowdownIntensity * (1.0 - decayPhase)

		pulse = baseDecay + blowdownEffect
	}

	return pulse
}

// generatePulswFourStroke creates a single pulse value for a 4-stroke engine based on a given phase value
// and engine geometry.
func generatePulseFourStroke(phase float64, engine *profiles.EngineProfile) (pulse float64) {
	if phase < 0.3 {
		// Quick attack (30% of pulse width)
		// Adjust attack characteristics based on engine balance
		// Poor balance = sharper attack, good balance = smoother attack
		attackSharpness := 1.0 - (engine.PrimaryBalance * 0.6) // 0.4 to 1.0 range
		attackPhase := phase / 0.3

		pulse = math.Sin(attackPhase * math.Pi / 2)

		// Use different attack curves based on engine balance
		if engine.PrimaryBalance < 0.7 {
			// Sharp, aggressive attack for poorly balanced engines
			pulse = math.Pow(pulse, attackSharpness)
		} else {
			// Smoother attack for well-balanced engines
			pulse *= attackSharpness
		}
	} else {
		// Quick decay (70% of pulse width)
		// Adjust decay based on both primary and secondary balance
		decayPhase := (phase - 0.3) / 0.7

		// Primary balance affects base decay rate
		primaryDecayRate := 4.0 + (1.0-engine.PrimaryBalance)*2.0

		// Secondary balance affects decay smoothness
		secondaryDecayFactor := 1.0 + (1.0-engine.SecondaryBalance)*0.5

		combinedDecayRate := primaryDecayRate * secondaryDecayFactor
		pulse = math.Exp(-decayPhase * combinedDecayRate)
	}

	return pulse
}
