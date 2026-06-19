package audio

import (
	"math"
	"testing"
)

// internalConstantSource is a constant-value source used only in internal tests.
type internalConstantSource struct {
	value    float32
	channels int
}

func (c *internalConstantSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels
	for i := range frames {
		for ch := range channels {
			buf[i*channels+ch] = c.value
		}
	}

	return frames, true
}

// internalSineSource emits a phase-continuous sine on every channel.
type internalSineSource struct {
	rate     int
	freq     float64
	channels int
	phase    float64
}

func (s *internalSineSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels
	inc := 2 * math.Pi * s.freq / float64(s.rate)

	for f := range frames {
		v := float32(math.Sin(s.phase))
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

// TestPolyphaseSource_DCPass verifies that NewResamplingSource (polyphase path,
// 8000->32000) outputs a value converging to the constant input in steady state.
// The weights in each phase must sum to 1 for DC to pass unchanged.
func TestPolyphaseSource_DCPass(t *testing.T) {
	t.Parallel()

	const (
		inRate   = 8000
		outRate  = 32000
		channels = 2
		value    = float32(0.3)
		epsilon  = 1e-5
	)

	src := &internalConstantSource{value: value, channels: channels}
	res := NewResamplingSource(src, inRate, outRate, channels)

	// polyphaseSource must be chosen for this ratio (L=4 < maxPolyphasePhases).
	if _, ok := res.(*polyphaseSource); !ok {
		t.Fatalf("expected *polyphaseSource for 8000->32000, got %T", res)
	}

	// Warm up: skip the edge-replicated startup region.
	warmup := make([]float32, outRate/10*channels) // 100 ms
	res.ReadInterleaved(warmup, channels)

	// Steady state: every sample should be within epsilon of value.
	buf := make([]float32, outRate/2*channels) // 500 ms
	frames, ok := res.ReadInterleaved(buf, channels)

	if !ok {
		t.Fatal("ReadInterleaved returned ok=false")
	}

	for f := range frames {
		for c := range channels {
			got := buf[f*channels+c]
			if math.Abs(float64(got-value)) > epsilon {
				t.Errorf("frame %d ch %d: expected ~%.4f, got %.6f", f, c, value, got)

				return // first failure is enough
			}
		}
	}
}

// TestPolyphaseVsLanczos verifies that the polyphase and Lanczos paths produce
// output that agrees to within a small tolerance for a sine input at 8000->32000.
func TestPolyphaseVsLanczos(t *testing.T) {
	t.Parallel()

	const (
		inRate   = 8000
		outRate  = 32000
		channels = 1
		freq     = 440.0
		maxDiff  = 1e-3
		warmup   = 256 // output frames to skip (startup transient)
	)

	makeSine := func() *internalSineSource {
		return &internalSineSource{rate: inRate, freq: freq, channels: channels}
	}

	poly := NewResamplingSource(makeSine(), inRate, outRate, channels)
	if _, ok := poly.(*polyphaseSource); !ok {
		t.Fatalf("expected *polyphaseSource, got %T", poly)
	}

	lanc := newLanczosSource(makeSine(), inRate, outRate, channels)

	// Drain the warmup region from both so startup edge effects cancel.
	wBuf := make([]float32, warmup*channels)
	poly.ReadInterleaved(wBuf, channels)
	lanc.ReadInterleaved(wBuf, channels)

	// Compare 1 s of output.
	bufP := make([]float32, outRate*channels)
	bufL := make([]float32, outRate*channels)
	poly.ReadInterleaved(bufP, channels)
	lanc.ReadInterleaved(bufL, channels)

	for i := range bufP {
		diff := math.Abs(float64(bufP[i] - bufL[i]))
		if diff > maxDiff {
			t.Fatalf("sample %d: polyphase=%.6f lanczos=%.6f diff=%.6f > %.6f",
				i, bufP[i], bufL[i], diff, maxDiff)
		}
	}
}

// BenchmarkResample8kTo32kStereo measures the throughput of the polyphase
// resampler at the synthesizer's standard 8 kHz -> 32 kHz stereo up-sample.
// Run with -bench=BenchmarkResample8kTo32kStereo -benchtime=2s.
func BenchmarkResample8kTo32kStereo(b *testing.B) {
	const (
		inRate   = 8000
		outRate  = 32000
		channels = 2
	)

	src := &internalConstantSource{value: 0.25, channels: channels}
	res := NewResamplingSource(src, inRate, outRate, channels)

	buf := make([]float32, outRate*channels) // 1 s of stereo output

	for b.Loop() {
		res.ReadInterleaved(buf, channels)
	}
}
