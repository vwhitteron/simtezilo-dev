package audio_test

import (
	"math"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

// sineSource emits a continuous, phase-continuous sine on every channel, so it
// never underruns and any discontinuity observed downstream must have been
// introduced by the stage under test rather than the source.
type sineSource struct {
	rate     int
	freq     float64
	amp      float64
	channels int
	phase    float64
}

func (s *sineSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels

	inc := 2 * math.Pi * s.freq / float64(s.rate)

	for f := range frames {
		v := float32(s.amp * math.Sin(s.phase))
		for c := range channels {
			buf[f*channels+c] = v
		}

		s.phase += inc
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}
	}

	return frames, true
}

// detectClicks flags transient discontinuities via the second difference
// d2[n] = x[n] - 2x[n-1] + x[n-2]. For a clean sine the curvature is bounded by
// amplitude * (2*pi*f/fs)^2; anything exceeding toleranceFactor times that bound
// is a click. Returns the offending sample indices.
func detectClicks(read []float64, frequencyHz, sampleRateHz, amplitude, toleranceFactor float64) []int {
	var clicks []int

	if len(read) < 3 {
		return clicks
	}

	omega := 2 * math.Pi * frequencyHz / sampleRateHz
	threshold := amplitude * omega * omega * toleranceFactor

	for i := 2; i < len(read); i++ {
		if d2 := read[i] - 2*read[i-1] + read[i-2]; math.Abs(d2) > threshold {
			clicks = append(clicks, i)
		}
	}

	return clicks
}

// TestResampler_SineNoTransientClicks runs a continuous sine through the
// windowed-sinc resampler at the synthesizer's real 8 kHz -> 32 kHz ratio and
// asserts the output carries no transient discontinuities. The linear
// interpolator this replaced introduced curvature spikes (imaging) on every
// output frame; the Lanczos kernel must not.
func TestResampler_SineNoTransientClicks(t *testing.T) {
	t.Parallel()

	const (
		inRate  = 8000
		outRate = 32000
		freq    = 300.0
		amp     = 0.8
		warmup  = 64 // skip the kernel's edge-replicated startup region
	)

	src := &sineSource{rate: inRate, freq: freq, amp: amp, channels: 1}
	res := audio.NewResamplingSource(src, inRate, outRate, 1)

	out := make([]float32, outRate) // 1 s of output
	res.ReadInterleaved(out, 1)

	f := make([]float64, len(out))
	for i, v := range out {
		f[i] = float64(v)
	}

	clicks := detectClicks(f[warmup:], freq, outRate, amp, 8.0)
	if len(clicks) != 0 {
		t.Fatalf("resampler introduced %d transient click(s); first few output indices: %v",
			len(clicks), clicks[:min(len(clicks), 8)])
	}
}
