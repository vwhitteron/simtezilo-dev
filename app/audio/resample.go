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

// NewResamplingSource wraps src so that audio produced at inRate is presented at
// outRate using per-channel windowed-sinc (Lanczos) interpolation. When the
// rates match, src is returned unchanged. The returned source streams
// indefinitely as long as src does.
//
// This is the N-channel replacement for beep.Resample, which only operates on
// stereo [][2]float64 streams. Unlike plain linear interpolation it band-limits
// the signal, so the large 8 kHz -> 32 kHz up-sample no longer introduces the
// imaging/aliasing artifacts that made the output sound harsh and choppy.
func NewResamplingSource(src SampleSource, inRate, outRate, channels int) SampleSource {
	if src == nil || inRate <= 0 || outRate <= 0 || inRate == outRate {
		return src
	}

	// When down-sampling the kernel must be band-limited to the output Nyquist,
	// which widens its support (and thus the tap count) by 1/cutoff. When
	// up-sampling the cutoff is the input Nyquist and the kernel keeps its
	// nominal width.
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
		i0 := int(math.Floor(r.pos))

		// Ensure the kernel's right-hand taps (up to i0+halfTaps) are buffered.
		need := i0 + r.halfTaps + 1
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
			for c := range channels {
				out[produced*channels+c] = 0
			}

			r.pos += r.step

			continue
		}

		frac := r.pos - float64(i0)

		// Precompute the kernel weights for this output position. They depend
		// only on the fractional offset, not the channel, so compute them once
		// and reuse across all channels.
		wsum := 0.0
		for t := -r.halfTaps + 1; t <= r.halfTaps; t++ {
			w := lanczos((frac-float64(t))*r.cutoff, resampleLobes)
			r.weights[t+r.halfTaps-1] = w
			wsum += w
		}

		if wsum == 0 {
			wsum = 1
		}

		for c := range channels {
			sum := 0.0

			for t := -r.halfTaps + 1; t <= r.halfTaps; t++ {
				idx := i0 + t
				if idx < 0 {
					idx = 0
				}

				if idx >= count {
					idx = count - 1
				}

				sum += r.weights[t+r.halfTaps-1] * float64(r.inBuf[idx*r.channels+c])
			}

			out[produced*channels+c] = float32(sum / wsum)
		}

		r.pos += r.step

		r.slide()
	}

	return outFrames, true
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

// lanczos evaluates the Lanczos kernel (a windowed sinc) of width a at x.
func lanczos(x float64, a float64) float64 {
	switch {
	case x == 0:
		return 1
	case x <= -a || x >= a:
		return 0
	}

	px := math.Pi * x

	return a * math.Sin(px) * math.Sin(px/a) / (px * px)
}
