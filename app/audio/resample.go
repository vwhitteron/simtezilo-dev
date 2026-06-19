package audio

import "math"

// pullBlockFrames is how many input frames the resampler pulls from its source
// per refill, amortising the per-call overhead of the underlying source. It is
// deliberately kept below the synthesizer mixer's default buffer cushion (24 ms
// ~= 192 frames at the 8 kHz internal rate) so a single pull cannot drain an
// upstream channel buffer into a short read (an underrun the consumer zero-pads
// into an audible click).
const pullBlockFrames = 128

// resampleLobes is the half-width of the windowed-sinc (Lanczos) kernel in
// sinc zero-crossings. Larger values sharpen the transition band at the cost of
// more taps per output sample. Eight lobes (16 taps when up-sampling) gives a
// clean reconstruction far better than linear interpolation, which is essential
// for the synthesizer's large up-sample ratio (8 kHz internal -> 32 kHz output).
const resampleLobes = 8

// maxPolyphasePhases is the largest reduced-fraction denominator (L = outRate/gcd)
// for which the polyphase path is used. Ratios whose reduced denominator exceeds
// this fall back to the per-sample Lanczos path.
const maxPolyphasePhases = 4096

// NewResamplingSource wraps src so that audio produced at inRate is presented at
// outRate using per-channel windowed-sinc (Lanczos) interpolation. When the
// rates match, src is returned unchanged. The returned source streams
// indefinitely as long as src does.
//
// For rational ratios whose reduced denominator fits within maxPolyphasePhases
// the kernel weights are precomputed once per phase (polyphaseSource), removing
// all math.Sin calls from the hot loop. Ratios with larger denominators fall
// back to the per-sample resamplingSource.
//
// This is the N-channel replacement for beep.Resample, which only operates on
// stereo [][2]float64 streams. Unlike plain linear interpolation it band-limits
// the signal, so the large 8 kHz -> 32 kHz up-sample no longer introduces the
// imaging/aliasing artifacts that made the output sound harsh and choppy.
func NewResamplingSource(src SampleSource, inRate, outRate, channels int) SampleSource { //nolint:ireturn // constructor returns SampleSource interface by design
	if src == nil || inRate <= 0 || outRate <= 0 || inRate == outRate {
		return src
	}

	gcd := gcdInt(inRate, outRate)
	numPhases := outRate / gcd

	if numPhases > maxPolyphasePhases {
		return newLanczosSource(src, inRate, outRate, channels)
	}

	return newPolyphaseSource(src, inRate, outRate, channels, numPhases, gcd)
}

// newLanczosSource constructs the per-sample windowed-sinc fallback. It is
// called when the reduced denominator L exceeds maxPolyphasePhases.
func newLanczosSource(src SampleSource, inRate, outRate, channels int) *resamplingSource {
	cutoff := math.Min(1.0, float64(outRate)/float64(inRate))
	halfTaps := int(math.Ceil(float64(resampleLobes) / cutoff))

	return &resamplingSource{
		src:      src,
		channels: channels,
		step:     float64(inRate) / float64(outRate),
		cutoff:   cutoff,
		halfTaps: halfTaps,
		weights:  make([]float64, 2*halfTaps),
		srcOK:    true,
	}
}

// resamplingSource is the per-sample windowed-sinc (Lanczos) fallback used only
// for ratios whose reduced denominator exceeds maxPolyphasePhases. For normal
// use cases (e.g. 8 kHz -> 32 kHz, L=4) the polyphaseSource is the hot path.
type resamplingSource struct {
	src      SampleSource
	channels int
	step     float64 // input frames advanced per output frame
	cutoff   float64 // sinc cutoff relative to the input sample rate (<= 1)
	halfTaps int     // kernel reaches halfTaps input frames either side of pos

	inBuf   []float32 // buffered interleaved input frames
	pos     float64   // fractional input-frame index of the next output sample
	pull    []float32 // scratch buffer for pulling input blocks
	weights []float64 // per-output-frame kernel weights (reused, no allocation)
	srcOK   bool
}

func (r *resamplingSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	if channels != r.channels {
		// Caller channel count must match the configured count; fail safe.
		for i := range out {
			out[i] = 0
		}

		return len(out) / channels, true
	}

	outFrames := len(out) / channels

	for produced := range outFrames {
		r.mixFrame(out, produced, channels)
	}

	return outFrames, true
}

// mixFrame synthesises one output frame at index produced into out. It is split
// out of ReadInterleaved to keep the cyclomatic complexity of each method below
// the project's lint threshold.
func (r *resamplingSource) mixFrame(out []float32, produced, channels int) { //nolint:cyclop // windowed-sinc kernel loop; complexity is inherent in the algorithm
	baseFrameIdx := int(math.Floor(r.pos))

	// Ensure the kernel's right-hand taps (up to baseFrameIdx+halfTaps) are buffered.
	need := baseFrameIdx + r.halfTaps + 1
	for r.frameCount() < need && r.srcOK {
		before := r.frameCount()

		r.fill()

		if r.frameCount() == before {
			// Source produced nothing this round; stop to avoid spinning.
			break
		}
	}

	count := r.frameCount()
	if count == 0 {
		// No input at all; emit silence rather than stalling.
		for ch := range channels {
			out[produced*channels+ch] = 0
		}

		r.pos += r.step

		return
	}

	frac := r.pos - float64(baseFrameIdx)

	// Precompute the kernel weights for this output position. They depend
	// only on the fractional offset, not the channel, so compute them once
	// and reuse across all channels.
	wsum := 0.0

	for tap := -r.halfTaps + 1; tap <= r.halfTaps; tap++ {
		tapWeight := lanczos((frac-float64(tap))*r.cutoff, resampleLobes)
		r.weights[tap+r.halfTaps-1] = tapWeight
		wsum += tapWeight
	}

	if wsum == 0 {
		wsum = 1
	}

	for chanIdx := range channels {
		sum := 0.0

		for tap := -r.halfTaps + 1; tap <= r.halfTaps; tap++ {
			idx := max(baseFrameIdx+tap, 0)

			if idx >= count {
				idx = count - 1
			}

			sum += r.weights[tap+r.halfTaps-1] * float64(r.inBuf[idx*r.channels+chanIdx])
		}

		out[produced*channels+chanIdx] = float32(sum / wsum)
	}

	r.pos += r.step

	r.slide()
}

// slide drops fully-consumed input frames to keep inBuf bounded while retaining
// enough history for the kernel's left-hand taps.
func (r *resamplingSource) slide() {
	keep := r.halfTaps + 1

	drop := int(math.Floor(r.pos)) - keep
	if drop < pullBlockFrames {
		return
	}

	r.inBuf = append(r.inBuf[:0], r.inBuf[drop*r.channels:]...)
	r.pos -= float64(drop)
}

func (r *resamplingSource) frameCount() int {
	return len(r.inBuf) / r.channels
}

// fill pulls one more block of input frames from the source, appending to inBuf.
func (r *resamplingSource) fill() {
	if !r.srcOK {
		return
	}

	if cap(r.pull) < pullBlockFrames*r.channels {
		r.pull = make([]float32, pullBlockFrames*r.channels)
	}

	block := r.pull[:pullBlockFrames*r.channels]

	frames, ok := r.src.ReadInterleaved(block, r.channels)
	if frames > 0 {
		r.inBuf = append(r.inBuf, block[:frames*r.channels]...)
	}

	if !ok {
		r.srcOK = false
	}
}

// polyphaseSource resamples using precomputed per-phase kernel weights. At
// construction it computes L weight vectors (one per sub-sample phase), each of
// length 2*halfTaps, normalised so their sum equals 1. The hot loop then
// performs pure multiply-accumulate with no transcendental math, reducing the
// idle CPU cost of the 8 kHz -> 32 kHz up-sample from ~87 % (math.Sin dominated)
// to a handful of multiply-adds.
type polyphaseSource struct {
	src       SampleSource
	channels  int
	numPhases int // number of polyphase phases (= outRate/gcd)
	inStep    int // input frames advanced per output frame numerator (= inRate/gcd)
	halfTaps  int

	weights [][]float64 // weights[phase][tap index], pre-normalised
	inBuf   []float32   // buffered interleaved input frames
	pull    []float32   // scratch buffer for pulling input blocks
	acc     int         // accumulated phase counter: baseFrame=acc/numPhases, phase=acc%numPhases
	srcOK   bool
}

// newPolyphaseSource builds a polyphaseSource for the given rate pair. numPhases
// is the reduced output/gcd and gcd is gcd(inRate,outRate).
func newPolyphaseSource(src SampleSource, inRate, outRate, channels, numPhases, gcd int) *polyphaseSource {
	inStep := inRate / gcd
	cutoff := math.Min(1.0, float64(outRate)/float64(inRate))
	halfTaps := int(math.Ceil(float64(resampleLobes) / cutoff))

	weights := make([][]float64, numPhases)
	for phaseIdx := range numPhases {
		frac := float64(phaseIdx) / float64(numPhases)
		phaseWeights := make([]float64, 2*halfTaps)
		sum := 0.0

		for tap := -halfTaps + 1; tap <= halfTaps; tap++ {
			tapWeight := lanczos((frac-float64(tap))*cutoff, resampleLobes)
			phaseWeights[tap+halfTaps-1] = tapWeight
			sum += tapWeight
		}

		if sum == 0 {
			sum = 1
		}

		for i := range phaseWeights {
			phaseWeights[i] /= sum
		}

		weights[phaseIdx] = phaseWeights
	}

	return &polyphaseSource{
		src:       src,
		channels:  channels,
		numPhases: numPhases,
		inStep:    inStep,
		halfTaps:  halfTaps,
		weights:   weights,
		srcOK:     true,
	}
}

func (r *polyphaseSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	if channels != r.channels {
		// Caller channel count must match the configured count; fail safe.
		for i := range out {
			out[i] = 0
		}

		return len(out) / channels, true
	}

	outFrames := len(out) / channels

	for produced := range outFrames {
		r.mixFrame(out, produced, channels)
	}

	return outFrames, true
}

// mixFrame synthesises one output frame at index produced into out. It is split
// out of ReadInterleaved to keep the cyclomatic complexity of each method below
// the project's lint threshold.
func (r *polyphaseSource) mixFrame(out []float32, produced, channels int) {
	baseFrame := r.acc / r.numPhases
	phase := r.acc % r.numPhases

	// Ensure right-hand taps are buffered.
	need := baseFrame + r.halfTaps + 1
	for r.frameCount() < need && r.srcOK {
		before := r.frameCount()

		r.fill()

		if r.frameCount() == before {
			break
		}
	}

	count := r.frameCount()
	if count == 0 {
		// No input at all; emit silence rather than stalling.
		for chanIdx := range channels {
			out[produced*channels+chanIdx] = 0
		}

		r.acc += r.inStep

		return
	}

	phaseWeights := r.weights[phase]

	for chanIdx := range channels {
		sum := 0.0

		for tap := -r.halfTaps + 1; tap <= r.halfTaps; tap++ {
			idx := max(baseFrame+tap, 0)

			if idx >= count {
				idx = count - 1
			}

			sum += phaseWeights[tap+r.halfTaps-1] * float64(r.inBuf[idx*r.channels+chanIdx])
		}

		out[produced*channels+chanIdx] = float32(sum)
	}

	r.acc += r.inStep

	r.slide()
}

// slide drops fully-consumed input frames to keep inBuf bounded while retaining
// enough history for the kernel's left-hand taps. The integer accumulator acc is
// adjusted to remain consistent with the new inBuf origin.
func (r *polyphaseSource) slide() {
	keep := r.halfTaps + 1
	baseFrame := r.acc / r.numPhases
	drop := baseFrame - keep

	if drop < pullBlockFrames {
		return
	}

	r.inBuf = append(r.inBuf[:0], r.inBuf[drop*r.channels:]...)
	r.acc -= drop * r.numPhases
}

func (r *polyphaseSource) frameCount() int {
	return len(r.inBuf) / r.channels
}

// fill pulls one more block of input frames from the source, appending to inBuf.
func (r *polyphaseSource) fill() {
	if !r.srcOK {
		return
	}

	if cap(r.pull) < pullBlockFrames*r.channels {
		r.pull = make([]float32, pullBlockFrames*r.channels)
	}

	block := r.pull[:pullBlockFrames*r.channels]

	frames, ok := r.src.ReadInterleaved(block, r.channels)
	if frames > 0 {
		r.inBuf = append(r.inBuf, block[:frames*r.channels]...)
	}

	if !ok {
		r.srcOK = false
	}
}

// lanczos evaluates the Lanczos kernel (a windowed sinc) of width lobes at position.
func lanczos(position float64, lobes float64) float64 {
	switch {
	case position == 0:
		return 1
	case position <= -lobes || position >= lobes:
		return 0
	}

	px := math.Pi * position

	return lobes * math.Sin(px) * math.Sin(px/lobes) / (px * px)
}

// gcdInt returns the greatest common divisor of a and b using Euclid's algorithm.
func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}

	return a
}
