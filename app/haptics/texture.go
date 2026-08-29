package haptics

import (
	"math"

	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// Road-texture layer render tuning. The rumble is driven by ground speed and road
// surface type, modulated by a slow roughness envelope taken from suspension
// movement. The envelope is a ~200 ms sliding RMS, so it measures how rough the
// surface IS rather than tracking individual bumps; the chassis impact pulse keeps
// sole ownership of discrete bumps and kerbs, so this layer stays a distinct,
// continuous background. Suspension data cannot supply the carrier in any case:
// telemetry arrives at 59.94 Hz, and its Nyquist limit sits below the bottom of this
// layer's 55-150 Hz band, so it can only contribute a level, never a spectrum.
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
	// speed so a parked car does not buzz. Above it the speed curve ramps in. The gate sits
	// at zero because the fade-in window below starts there; a creeping car is kept quiet by
	// the fade, not by a speed threshold.
	textureMinSpeedMps = 0.0

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

	// textureOnsetSpeedMps is the width of the fade-in window above the standstill gate over
	// which the amplitude eases from silence up to the floor, so the layer slides in smoothly
	// as the car pulls away rather than snapping on. The window spans 0 to 30 km/h
	// (8.33 m/s), which holds car-park and pit-lane speeds well down and makes the onset far
	// too gradual to hear as a step. The same ramp runs in reverse as the car slows to a
	// stop.
	textureOnsetSpeedMps = 8.3333

	// textureCutoffSpeedRefMps is the ground speed at which the noise low-pass cutoff
	// reaches the top of its band; the texture brightens with speed because road features
	// sweep under the tyres faster. The cutoff rises linearly with speed up to this point,
	// which is the shape the physics gives: the texture frequency a tyre generates is the
	// speed divided by the road feature wavelength, so it is proportional to speed. The
	// reference sits at the top of ordinary road speed (~160 km/h) so the sweep spreads
	// across the whole driven range instead of finishing in the first few m/s.
	textureCutoffSpeedRefMps = 45.0

	// textureHighpassHz is the upper limit of the noise band's lower edge. A one-pole
	// high-pass at this corner removes the sub-bass content that, below the tactile
	// flutter-fusion threshold (~30-50 Hz), is felt as distinct individual kicks a few times
	// a second rather than a continuous rumble. It also keeps the layer clear of the
	// whole-body ride band (~1-30 Hz); the tactile road feel through the pedals and floorpan
	// that this layer models is a higher-register phenomenon (studies put pedal/floor feel
	// mainly at 20-100 Hz, with structure-borne tyre/suspension roughness up to
	// ~160-300 Hz). The peaky low-frequency impacts are left to the chassis pulse layer.
	textureHighpassHz = 65.0

	// textureHighpassFloorHz is the lower limit of that same edge. The high-pass corner
	// tracks the speed-driven low-pass cutoff (see textureHighpassRatio) so the whole band
	// slides down at low speed rather than only its top edge. This floor stops the slide at
	// the flutter-fusion threshold, below which the rumble breaks up into separate kicks.
	textureHighpassFloorHz = 32.0

	// textureHighpassRatio sets the high-pass corner as a fraction of the low-pass cutoff,
	// which fixes the band width at a constant ratio (~1 octave) as the band sweeps. A
	// constant-ratio band keeps the grain's character the same while its register moves.
	textureHighpassRatio = 0.5

	// textureBandMinHz and textureBandMaxHz are the fixed lower and upper ends of the
	// speed-swept cutoff. Both were configurable and are now fixed. They set the cutoff
	// only: the noise is normalised to unit RMS below, so they move the spectrum and leave
	// the output level to the surface and the speed. Anything above textureDeviceCeilingHz
	// is clamped away as well, so the setting had no useful travel.
	//
	// The low end sits well under the old 90 Hz so a car just off the standstill gate
	// rumbles in a low register and audibly climbs as it gathers speed. Paired with the
	// tracking high-pass this puts the band at roughly 32-61 Hz at 10 km/h and roughly
	// 65-148 Hz at 160 km/h: over an octave of travel spread across the driven speed
	// range, instead of the near-fixed pitch the old 90 Hz floor gave.
	textureBandMinHz = 55.0
	textureBandMaxHz = 150.0

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

	// textureRoughnessRefMps is the pivot roughness (m/s) against which the layer's
	// live SuspensionRoughness is compared. It is a MEASURED value, not a chosen one:
	// the median of the tarmac 20-40 m/s median roughness across eight replays
	// spanning eight cars and three tracks (Spa, Nordschleife, Saint-Croix), which
	// ranged 0.0189 to 0.0271, a 1.43x spread. Telemetry arrives at 59.94 Hz and road
	// content above 30 Hz folds down (aliases) into the measured band, so this is a
	// relative correlate of roughness and never a calibrated physical quantity.
	// Re-measure it with TestRoughnessProbe in app/haptics/roughness_probe_test.go.
	textureRoughnessRefMps = 0.0241

	// textureRoughnessCurve is the compression exponent applied to the roughness
	// ratio. Roughness RMS scales roughly with road amplitude, but perceived
	// vibration intensity is nearer its square root, so a 4x rougher patch reads as
	// 2x.
	textureRoughnessCurve = 0.5

	// textureRoughnessAmpDepth is how far roughness moves the level away from the
	// surface's tuned value. 0 reproduces the pre-roughness behaviour exactly; it
	// sits below 1 so a rough patch lifts rather than swamping the surface
	// distinction the tuned surfaceRumble levels encode.
	textureRoughnessAmpDepth = 0.60

	// textureRoughnessAmpMin and textureRoughnessAmpMax bound the roughness
	// amplitude factor. The min deliberately floors the airborne case: with the
	// wheels off the ground the envelope collapses, and a 30% dip reads as "the
	// road went away" while a drop to silence would read as a bug. Do not lower it
	// below about 0.6.
	textureRoughnessAmpMin = 0.70
	textureRoughnessAmpMax = 1.60

	// textureRoughnessCoarsenDepth moves the low-pass cutoff DOWNWARD as roughness
	// rises, matching the existing coarseness convention where dirt at 0.80 is
	// coarser than tarmac at 1.00. It is a quarter of the amplitude depth so it
	// cannot fight the speed sweep, which owns brightness.
	textureRoughnessCoarsenDepth = 0.15

	// textureRoughnessCutoffMin and textureRoughnessCutoffMax bound the roughness
	// cutoff factor, deliberately asymmetric. A rougher road should audibly coarsen;
	// a smoother one should barely brighten, because textureCutoffSpeedRefMps
	// already owns the speed-to-brightness sweep. The span is about one semitone, a
	// character shift rather than a pitch move.
	textureRoughnessCutoffMin = 0.88
	textureRoughnessCutoffMax = 1.06
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
// per routed texture channel whose amplitude is set by road surface and speed, then
// lifted or trimmed by the suspension-roughness envelope; brightness (low-pass
// cutoff) rises with speed and coarsens slightly as the road roughens. Texture is its
// own synth source (independent mute/gain/routing); each block is appended at the
// channel write cursor and the filter state persists across blocks, so successive
// blocks join without a click.
func (g *Generator) Texture() {
	if g.cfg.GetSynthTextureMute() {
		return
	}

	speed := g.kin.Current.GroundSpeed
	surfaceLevel, surfaceCoarseness := aggregateSurface(g.cfg, g.kin.Current.SurfaceType)
	roughAmp, roughCutoff := textureRoughnessFactors(g.kin.Current.SuspensionRoughness, g.kin.Current.SuspensionRoughnessValid)

	amplitude := surfaceLevel * textureSpeedAmplitude(speed) * roughAmp
	amplitude = min(amplitude, textureMaxAmplitude)

	cutoffHz := textureCutoffHz(
		speed,
		textureBandMinHz,
		textureBandMaxHz,
	) * surfaceCoarseness * roughCutoff

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
	// sub-bass slow swells that read as distinct kicks. It tracks the low-pass cutoff at a
	// fixed ratio, so the whole band slides with speed at a constant width, and is clamped
	// between the flutter-fusion floor and the fixed upper corner.
	hpHz := min(max(cutoffHz*textureHighpassRatio, textureHighpassFloorHz), textureHighpassHz)
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

// textureRoughnessFactors maps the live suspension-roughness measurement into an
// amplitude and cutoff multiplier for the texture layer. Both factors are exactly
// 1.0 when roughness equals textureRoughnessRefMps, so the hand-tuned surfaceRumble
// levels play back unchanged on ordinary tarmac and roughness only ever deviates
// from them. That property is structural: ratio == 1 gives factor == 1 for any
// exponent.
func textureRoughnessFactors(roughness float64, valid bool) (amplitude, cutoff float64) {
	// A telemetry gap, a format without suspension data, or an uncalibrated
	// reference are all safe: this leaves the layer exactly as it behaved before
	// roughness existed.
	if !valid || textureRoughnessRefMps <= 0 {
		return 1, 1
	}

	factor := math.Pow(max(roughness, 0)/textureRoughnessRefMps, textureRoughnessCurve)

	amplitude = min(max(1+textureRoughnessAmpDepth*(factor-1), textureRoughnessAmpMin), textureRoughnessAmpMax)
	cutoff = min(max(1-textureRoughnessCoarsenDepth*(factor-1), textureRoughnessCutoffMin), textureRoughnessCutoffMax)

	return amplitude, cutoff
}

// surfaceRumble maps a single surface classification to its rumble level (loudness,
// ≈ output RMS at full speed, before the textureMaxAmplitude cap) and coarseness (a
// multiplier on the speed-derived low-pass cutoff: <1 lowers the cutoff for a coarser
// grain). The values come from haptics.surfaceRumble in the config, keyed by the
// surface's lowercase name; the switch below is the fallback for a surface missing
// from the config. Levels run near full scale so the rumble is loud at unity gain;
// loose surfaces (grass, dirt) are louder and coarser than smooth tarmac, and the
// loudest sit at/above 1.0 so they reach the amplitude cap. Unknown is treated as
// tarmac. The fallback values are starting points to be trimmed on-car; the user
// trims overall loudness with the texture channel gain.
func surfaceRumble(cfg *config.Config, surface models.SurfaceType) (level, coarseness float64) {
	// Unknown has no entry of its own. It reads tarmac's, so a tuned tarmac carries
	// the unclassified surface with it, as the shipped values always did.
	lookup := surface
	if lookup == models.SurfaceTypeUnknown {
		lookup = models.SurfaceTypeTarmac
	}

	if rumble, ok := cfg.GetHapticsSurfaceRumble(lookup.String()); ok {
		return rumble.Level, rumble.Coarseness
	}

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
func aggregateSurface(cfg *config.Config, surfaces models.CornerSetGeneric[models.SurfaceType]) (level, coarseness float64) {
	corners := [4]models.SurfaceType{
		surfaces.FrontLeft,
		surfaces.FrontRight,
		surfaces.RearLeft,
		surfaces.RearRight,
	}

	var sumLevel, sumCoarseness float64

	for _, surface := range corners {
		cornerLevel, cornerCoarseness := surfaceRumble(cfg, surface)
		sumLevel += cornerLevel
		sumCoarseness += cornerCoarseness
	}

	return sumLevel / float64(len(corners)), sumCoarseness / float64(len(corners))
}

// textureCutoffHz maps ground speed into the texture frequency band, yielding the noise
// low-pass cutoff (brightness), which rises linearly with speed and saturates at the top
// of the band.
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
