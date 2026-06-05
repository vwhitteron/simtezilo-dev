package audio

import "math"

// pullBlockFrames is how many input frames the resampler pulls from its source
// per refill, amortising the per-call overhead of the underlying source.
const pullBlockFrames = 256

// NewResamplingSource wraps src so that audio produced at inRate is presented at
// outRate using per-channel linear interpolation. When the rates match, src is
// returned unchanged. The returned source streams indefinitely as long as src
// does.
//
// This is the N-channel replacement for beep.Resample, which only operates on
// stereo [][2]float64 streams.
func NewResamplingSource(src SampleSource, inRate, outRate, channels int) SampleSource {
	if src == nil || inRate <= 0 || outRate <= 0 || inRate == outRate {
		return src
	}

	return &resamplingSource{
		src:      src,
		channels: channels,
		step:     float64(inRate) / float64(outRate),
		srcOK:    true,
	}
}

type resamplingSource struct {
	src      SampleSource
	channels int
	step     float64 // input frames advanced per output frame

	inBuf []float32 // buffered interleaved input frames
	pos   float64   // fractional input-frame index of the next output sample
	pull  []float32 // scratch buffer for pulling input blocks
	srcOK bool
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
		idx := int(math.Floor(r.pos))

		// Ensure input frames idx and idx+1 are available.
		for r.frameCount() < idx+2 && r.srcOK {
			r.fill()
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

		if idx >= count {
			idx = count - 1
		}

		frac := float32(r.pos - math.Floor(r.pos))

		for c := range channels {
			a := r.inBuf[idx*r.channels+c]

			b := a
			if idx+1 < count {
				b = r.inBuf[(idx+1)*r.channels+c]
			}

			out[produced*channels+c] = a + (b-a)*frac
		}

		r.pos += r.step

		r.slide()
	}

	return outFrames, true
}

// slide drops fully-consumed input frames to keep inBuf bounded.
func (r *resamplingSource) slide() {
	drop := int(math.Floor(r.pos))
	if drop < pullBlockFrames {
		return
	}

	r.inBuf = append(r.inBuf[:0], r.inBuf[drop*r.channels:]...)
	r.pos -= float64(drop)
}
