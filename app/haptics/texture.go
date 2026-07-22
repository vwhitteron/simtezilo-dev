package haptics

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// Road-texture layer render tuning. The rumble is driven by ground speed and road
// surface type — not by suspension movement — so it reads as a distinct, continuous
// background layer rather than tracking the same bumps/kerbs the chassis impact pulse
// fires on.
//
// The carrier is band-limited NOISE, not a tone: real road texture is broadband,
// irregular vibration, so a pure sine reads as an artificial buzz. The noise amplitude
// is set by the surface (quiet fine grain on tarmac, louder coarse rumble on
// grass/dirt/sand) scaled by a speed curve (silent at a standstill, rising with speed,
// softly plateauing at high speed as the tyre/suspension absorb the highest
// frequencies), and low-pass filtered to a cutoff that rises with speed and drops on
// coarser surfaces, so the grain follows the road feel a real floorpan transmits.
const (
	// textureCushionFrames is the carrier's target unread depth on each texture
	// channel, in telemetry frames. The carrier is topped up to this depth every
	// frame; a couple of frames rides out frame jitter without perceptible latency.
	textureCushionFrames = 2

	// textureMaxAmplitude caps the modulation amplitude (≈ output RMS, since the noise
	// is RMS-normalised). It is set near full scale so the rumble is loud at unity
	// channel gain; the default texture gain ships below 0 dB (see TextureGain in
	// config_default.go) to leave headroom, and the downstream soft-knee limiter absorbs
	// the noise crest above RMS.
	textureMaxAmplitude = 1.0

	// textureMinSpeedMps is the standstill gate: the rumble is silent at or below this
	// speed so a parked/creeping car does not buzz. Above it the speed curve ramps in.
	textureMinSpeedMps = 2.0

	// textureAmplitudeSpeedRefMps is the ground speed by which the speed-amplitude curve
	// has effectively saturated (soft cap). Set to a moderate speed so the rumble is
	// well established across normal driving and does not keep growing at high speed —
	// road feel is present from low speed and then plateaus, rather than being faint in
	// town and overwhelming on a straight.
	textureAmplitudeSpeedRefMps = 20.0

	// textureSpeedCurveK is the curvature of the soft-saturating speed-amplitude curve
	// (factor = 1 - exp(-k·x)). Larger k lifts the low-speed end and reaches the plateau
	// sooner. At k=4 the curve is ~86% of full at half the reference speed and ~98% at
	// the reference speed.
	textureSpeedCurveK = 4.0

	// textureLowSpeedFloor compresses the speed-amplitude curve into [floor, 1] once the
	// layer has faded in, so the texture is essentially constant once moving and only lifts
	// subtly with speed. The floor sets the loud-to-soft ratio: 0.84 ≈ 10^(-1.5/20), i.e.
	// about 1.5 dB between the low-speed (soft) and high-speed (loud) ends. Without it the
	// level falls toward zero near the gate, so a channel gain loud enough to feel in town
	// would be excessive on a straight.
	textureLowSpeedFloor = 0.84

	// textureOnsetSpeedMps is the width of the fade-in window just above the standstill
	// gate over which the amplitude eases from silence up to the floor, so the layer slides
	// in smoothly as the car pulls away rather than snapping on at the gate.
	textureOnsetSpeedMps = 1.5

	// textureCutoffSpeedRefMps is the ground speed at which the noise low-pass cutoff
	// reaches the top of its configured band; the texture brightens with speed because
	// road features sweep under the tyres faster.
	textureCutoffSpeedRefMps = 70.0

	// textureHighpassHz is the fixed lower edge of the noise band. A one-pole high-pass
	// at this corner removes the sub-bass content that, below the tactile flutter-fusion
	// threshold (~30-50 Hz), is felt as distinct individual kicks a few times a second
	// rather than a continuous rumble. It also keeps the layer clear of the whole-body
	// ride band (~1-30 Hz); the tactile road feel through the pedals and floorpan that
	// this layer models is a higher-register phenomenon (studies put pedal/floor feel
	// mainly at 20-100 Hz, with structure-borne tyre/suspension roughness up to
	// ~160-300 Hz). The peaky low-frequency impacts are left to the chassis pulse layer.
	textureHighpassHz = 65.0

	// textureDeviceCeilingHz is the upper bound the speed-driven cutoff is clamped to,
	// just under the 160 Hz roll-off of the physical haptic transducer (the same limit the
	// engine waveform generators respect). Content above this is attenuated by the device
	// and not shaped by the 5-160 Hz master EQ, so brightening past it is wasted; the clamp
	// also stops a surface coarseness multiplier (>1) from pushing the cutoff into the
	// stopband.
	textureDeviceCeilingHz = 155.0

	// textureTilt is the mix amount of an extra one-pole low-pass of the band added back in
	// to give the texture a gentle downward spectral tilt (≈ mild pinking). Real road
	// roughness has a PSD that falls with frequency; the raw band-pass of white noise is
	// roughly flat, so a little low-end emphasis reads as a more natural surface. The RMS
	// follower normalises power afterwards, so this changes spectral balance, not loudness.
	textureTilt = 0.35

	// textureRmsTrack is the per-sample coefficient of the running mean-square follower the
	// noise is divided by. At a ~80 ms time constant it evens out the slow patch-to-patch
	// power swings while leaving the short-term Rayleigh crest intact, so the grain keeps a
	// realistic peak-to-RMS liveliness rather than collapsing into a flat, even fuzz.
	textureRmsTrack = 0.0015

	// textureDrive is the input gain into the tanh saturator applied to the level-
	// normalised (≈ unit-variance Gaussian) noise. The saturator's output ceiling is
	// 1/tanh(drive), which fixes the peak-to-RMS crest, so drive is the main crest control.
	// Kept low so the tanh is near-linear through the normal ±2σ range — preserving a
	// realistic ~2.5× crest and lively grain — and only soft-limits the rare extreme tail so
	// a stray peak cannot spike the transducer; the mixer peak limiter is the hard backstop.
	textureDrive = 0.5
)

// textureChannelState holds the per-output-channel noise generator and filter state
// for the road-texture layer. Each channel runs an independent PRNG so the grain is
// decorrelated across channels (spacious, like real road), plus a two-pole low-pass
// and a slow low-pass whose output is subtracted to form the high-pass (band-pass)
// edge, a tilt low-pass mixed back in for a downward spectral slope, a running
// mean-square for RMS normalisation, and the previous target amplitude for per-block
// interpolation.
type textureChannelState struct {
	rng     uint64
	lp1     float64
	lp2     float64
	hpLow   float64
	tiltLp  float64
	meanSq  float64
	prevAmp float64
}

// Texture renders the continuous road-texture layer: low-level, band-limited noise
// per routed texture channel whose amplitude tracks the suspension-activity envelope
// and whose brightness (low-pass cutoff) rises with speed. Texture is its own synth
// source (independent mute/gain/routing); each block is appended at the channel write
// cursor and the filter state persists across blocks, so successive blocks join
// without a click.
func (g *Generator) Texture() {
	if g.cfg.GetSynthTextureMute() {
		return
	}

	speed := g.kin.Current.GroundSpeed
	surfaceLevel, surfaceCoarseness := aggregateSurface(g.kin.Current.SurfaceType)

	amplitude := surfaceLevel * textureSpeedAmplitude(speed)
	amplitude = min(amplitude, textureMaxAmplitude)

	cutoffHz := textureCutoffHz(
		speed,
		g.cfg.GetHapticsTextureMinHz(),
		g.cfg.GetHapticsTextureMaxHz(),
	) * surfaceCoarseness

	// Clamp to just under the transducer roll-off so a bright surface (coarseness > 1) or a
	// high configured band cannot push the cutoff into the device stopband.
	cutoffHz = min(cutoffHz, textureDeviceCeilingHz)

	sampleRate := float64(g.cfg.GetSynthInternalSampleRateHz())
	if sampleRate <= 0 || cutoffHz <= 0 {
		return
	}

	numChannels := g.synth.NumOutputChannels()
	g.textureState = ensureTextureStateLen(g.textureState, numChannels)

	// Per-channel EQ correction: reuse the pulse path's frequency-bucket lookup by
	// expressing the noise cutoff as an equivalent pulse width.
	pulseWidth := sampleRate / (2 * cutoffHz)

	// Two-pole low-pass coefficient for the noise. The cutoff sets the upper (bright)
	// edge of the texture band; the noise carries the grain.
	lpAlpha := 1 - math.Exp(-2*math.Pi*cutoffHz/sampleRate)

	// One-pole high-pass corner (subtracted below) sets the lower edge, removing the
	// sub-bass slow swells that read as distinct kicks. Kept below the low-pass cutoff
	// so the band never collapses when the speed-driven cutoff is low.
	hpHz := min(textureHighpassHz, cutoffHz*0.5)
	hpAlpha := 1 - math.Exp(-2*math.Pi*hpHz/sampleRate)

	for channel := range numChannels {
		if !g.cfg.GetSynthRouteEnabled(synthesizer.ChannelTexture, channel) {
			continue
		}

		need := g.textureRefillSamples(channel, sampleRate)
		if need <= 0 {
			continue
		}

		targetAmp := signal.Equalize(amplitude, pulseWidth, channel, g.cfg)

		if cap(g.textureScratch) < need {
			g.textureScratch = make([]float64, need)
		} else {
			g.textureScratch = g.textureScratch[:need]
		}

		buffer := g.textureScratch

		state := &g.textureState[channel]
		if state.rng == 0 {
			state.rng = textureSeed(channel)
			state.meanSq = 0.02 // seed so the first samples are not divided by ~0
		}

		startAmp := state.prevAmp
		span := float64(len(buffer))

		for index := range buffer {
			// Ramp amplitude across the block so a per-frame envelope change does not
			// step (zipper). span >= 1 here since need > 0.
			amp := startAmp + (targetAmp-startAmp)*(float64(index)+1)/span

			// White noise in [-1, 1) from an xorshift PRNG.
			state.rng = textureNextRand(state.rng)
			white := float64(state.rng>>11)/float64(uint64(1)<<53)*2 - 1

			// Two-pole low-pass to set the bright upper edge of the texture band.
			state.lp1 += lpAlpha * (white - state.lp1)
			state.lp2 += lpAlpha * (state.lp1 - state.lp2)

			// Subtract a slow low-pass to form the high-pass lower edge: the result is a
			// band-passed fuzz with the distinct-kick sub-bass removed.
			state.hpLow += hpAlpha * (state.lp2 - state.hpLow)
			band := state.lp2 - state.hpLow

			// Add a low-passed copy of the band back in for a gentle downward spectral
			// tilt, emphasising the lower end of the band as a real road surface does.
			state.tiltLp += hpAlpha * (band - state.tiltLp)
			tilted := band + textureTilt*state.tiltLp

			// Normalise to ~unit RMS so the drive into the saturator is level-independent.
			state.meanSq += textureRmsTrack * (tilted*tilted - state.meanSq)
			norm := tilted / math.Sqrt(state.meanSq+1e-12)

			// Soft-saturate: bound only the rare extreme crest while leaving the restored
			// Rayleigh liveliness intact. Divide by tanh(drive) so a unit-RMS input maps to
			// ~unit output, keeping amp the output level.
			shaped := math.Tanh(textureDrive*norm) / math.Tanh(textureDrive)

			buffer[index] = amp * shaped
		}

		state.prevAmp = targetAmp

		// Append at the channel write cursor (offset 0, overwrite): the block joins the
		// previous one seamlessly. The mixer sums the texture source into the same
		// outputs as chassis, so no cross-source mixing is needed here.
		g.synth.OverwriteBuffer(synthesizer.TextureChannelName(channel), buffer, 0)
	}
}

// textureRefillSamples returns how many carrier samples to append to a texture
// channel this frame to restore it to the texture cushion depth, clamped to at most
// two telemetry frames so a stall cannot produce an oversized block.
func (g *Generator) textureRefillSamples(channel int, sampleRate float64) int {
	samplesPerFrame := int(sampleRate) / telemetryFrameRate
	if samplesPerFrame <= 0 {
		return 0
	}

	depth := g.synth.ChannelDepth(synthesizer.TextureChannelName(channel))
	need := max(samplesPerFrame*textureCushionFrames-depth, 0)

	if maxBlock := samplesPerFrame * 2; need > maxBlock {
		need = maxBlock
	}

	return need
}

// textureSpeedAmplitude returns a 0..1 amplitude factor that mutes the texture at a
// standstill and rises with speed on a soft-saturating curve. It is zero at or below
// the standstill gate, climbs through the mid-speed range, and softly plateaus near
// textureAmplitudeSpeedRefMps — matching how road feel grows with speed and then levels
// off as the tyre carcass and suspension absorb the highest-frequency content.
func textureSpeedAmplitude(speedMps float64) float64 {
	if speedMps <= textureMinSpeedMps {
		return 0
	}

	// Smooth fade-in over a narrow window above the gate so the layer eases in.
	onset := min((speedMps-textureMinSpeedMps)/textureOnsetSpeedMps, 1)

	// Soft-saturating growth across the driving-speed range, compressed into
	// [floor, 1] so the texture is already well established at low speed and grows only
	// modestly with speed.
	x := (speedMps - textureMinSpeedMps) / (textureAmplitudeSpeedRefMps - textureMinSpeedMps)
	growth := 1 - math.Exp(-textureSpeedCurveK*x)

	return onset * (textureLowSpeedFloor + (1-textureLowSpeedFloor)*growth)
}

// surfaceRumble maps a single surface classification to its rumble level (loudness,
// ≈ output RMS at full speed, before the textureMaxAmplitude cap) and coarseness (a
// multiplier on the speed-derived low-pass cutoff: <1 lowers the cutoff for a coarser
// grain). Levels run near full scale so the rumble is loud at unity gain; loose
// surfaces (grass, dirt) are louder and coarser than smooth tarmac, and the loudest
// sit at/above 1.0 so they reach the amplitude cap. Unknown is treated as tarmac.
// Values are starting points to be trimmed on-car; the user trims overall loudness
// with the texture channel gain.
func surfaceRumble(surface models.SurfaceType) (level, coarseness float64) {
	switch surface {
	case models.SurfaceTypeConcrete:
		return 0.50, 0.95
	case models.SurfaceTypeGrass:
		return 0.65, 0.85
	case models.SurfaceTypeDirt:
		return 0.85, 0.80
	case models.SurfaceTypeSand:
		return 0.30, 1.20
	case models.SurfaceTypeSnow:
		return 0.60, 0.90
	case models.SurfaceTypeTarmac, models.SurfaceTypeUnknown:
		fallthrough
	default:
		return 0.40, 1.00
	}
}

// aggregateSurface reduces the per-corner surface types to a single rumble level and
// coarseness by averaging across the four corners. Averaging gives a partial rumble
// when only some wheels drop onto a rough surface (e.g. two wheels on grass) and the
// full rumble when all four are off-track.
func aggregateSurface(surfaces models.CornerSetGeneric[models.SurfaceType]) (level, coarseness float64) {
	corners := [4]models.SurfaceType{
		surfaces.FrontLeft,
		surfaces.FrontRight,
		surfaces.RearLeft,
		surfaces.RearRight,
	}

	var sumLevel, sumCoarseness float64

	for _, surface := range corners {
		cornerLevel, cornerCoarseness := surfaceRumble(surface)
		sumLevel += cornerLevel
		sumCoarseness += cornerCoarseness
	}

	return sumLevel / float64(len(corners)), sumCoarseness / float64(len(corners))
}

// textureCutoffHz maps ground speed into the configured texture frequency band,
// yielding the noise low-pass cutoff (brightness), which rises with speed and
// saturates at the top of the band.
func textureCutoffHz(speedMps, minHz, maxHz float64) float64 {
	factor := speedMps / textureCutoffSpeedRefMps
	if factor < 0 {
		factor = 0
	}

	if factor > 1 {
		factor = 1
	}

	return minHz + (maxHz-minHz)*factor
}

// textureNextRand advances an xorshift64 PRNG. It is fast, allocation-free and good
// enough for shaping audio-band noise.
func textureNextRand(s uint64) uint64 {
	s ^= s << 13
	s ^= s >> 7
	s ^= s << 17

	return s
}

// textureSeed returns a distinct, nonzero PRNG seed per channel so channels decorrelate.
func textureSeed(channel int) uint64 {
	return 0x9E3779B97F4A7C15*uint64(channel+1) | 1
}

// ensureTextureStateLen returns a per-channel state slice of length count, preserving
// existing state and growing on demand as the output channel count changes.
func ensureTextureStateLen(states []textureChannelState, count int) []textureChannelState {
	if cap(states) >= count {
		return states[:count]
	}

	grown := make([]textureChannelState, count)
	copy(grown, states)

	return grown
}
