package audio

import (
	"fmt"
	"math"
	"testing"
)

// constantSource is a helper SampleSource that emits a constant value per channel indefinitely.
type constantSource struct {
	values []float32 // one value per channel
}

func (c *constantSource) ReadInterleaved(buf []float32, channels int) (frames int, ok bool) {
	if len(c.values) != channels {
		return 0, false
	}

	frames = len(buf) / channels
	for i := range frames {
		for ch := range channels {
			buf[i*channels+ch] = c.values[ch]
		}
	}

	return frames, true
}

func TestNewResamplingSource_NilSource(t *testing.T) {
	result := NewResamplingSource(nil, 8000, 16000, 1)
	if result != nil {
		t.Errorf("expected nil result for nil source, got %v", result)
	}
}

func TestNewResamplingSource_SameRate(t *testing.T) {
	src := &constantSource{values: []float32{0.5}}

	result := NewResamplingSource(src, 8000, 8000, 1)
	if result != src {
		t.Errorf("expected same pointer for same rate, got %p vs %p", result, src)
	}
}

func TestResampling_UpsamplingConstantSignal(t *testing.T) {
	src := &constantSource{values: []float32{0.5}}

	resampler := NewResamplingSource(src, 8000, 32000, 1)
	if resampler == nil {
		t.Fatal("resampler should not be nil")
	}

	buf := make([]float32, 1024)

	frames, ok := resampler.ReadInterleaved(buf, 1)
	if !ok {
		t.Fatal("expected ok to be true")
	}

	if frames == 0 {
		t.Fatal("expected some frames")
	}

	for i := range frames {
		if math.Abs(float64(buf[i]-0.5)) > 1e-6 {
			t.Errorf("frame %d: expected ~0.5, got %f", i, buf[i])
		}
	}
}

func TestResampling_UpsamplingFrameCount(t *testing.T) {
	src := &constantSource{values: []float32{0.5}}

	resampler := NewResamplingSource(src, 8000, 32000, 1)
	if resampler == nil {
		t.Fatal("resampler should not be nil")
	}

	// Pull a large buffer to verify it doesn't panic and fills completely.
	buf := make([]float32, 4096)

	frames, ok := resampler.ReadInterleaved(buf, 1)
	if !ok {
		t.Fatal("expected ok to be true")
	}

	if frames != len(buf) {
		t.Errorf("expected buffer fully filled (%d frames), got %d", len(buf), frames)
	}
}

func TestResampling_MultiChannelSeparation(t *testing.T) {
	src := &constantSource{values: []float32{1.0, -1.0}}

	resampler := NewResamplingSource(src, 8000, 16000, 2)
	if resampler == nil {
		t.Fatal("resampler should not be nil")
	}

	buf := make([]float32, 1024)

	frames, ok := resampler.ReadInterleaved(buf, 2)
	if !ok {
		t.Fatal("expected ok to be true")
	}

	if frames == 0 {
		t.Fatal("expected some frames")
	}

	for i := range frames {
		ch0 := buf[i*2+0]
		ch1 := buf[i*2+1]

		if math.Abs(float64(ch0-1.0)) > 1e-6 {
			t.Errorf("frame %d ch0: expected ~1.0, got %f", i, ch0)
		}

		if math.Abs(float64(ch1-(-1.0))) > 1e-6 {
			t.Errorf("frame %d ch1: expected ~-1.0, got %f", i, ch1)
		}
	}
}

func TestResampling_DownsamplingConstantSignal(t *testing.T) {
	src := &constantSource{values: []float32{0.5}}

	resampler := NewResamplingSource(src, 48000, 16000, 1)
	if resampler == nil {
		t.Fatal("resampler should not be nil")
	}

	buf := make([]float32, 1024)

	frames, ok := resampler.ReadInterleaved(buf, 1)
	if !ok {
		t.Fatal("expected ok to be true")
	}

	if frames == 0 {
		t.Fatal("expected some frames")
	}

	for i := range frames {
		if math.Abs(float64(buf[i]-0.5)) > 1e-6 {
			t.Errorf("frame %d: expected ~0.5, got %f", i, buf[i])
		}
	}
}

func TestNewResamplingSource_InvalidRates(t *testing.T) {
	src := &constantSource{values: []float32{0.5}}

	result := NewResamplingSource(src, 0, 16000, 1)
	if result != src {
		t.Errorf("expected original source for zero inRate, got %p vs %p", result, src)
	}

	result = NewResamplingSource(src, 8000, 0, 1)
	if result != src {
		t.Errorf("expected original source for zero outRate, got %p vs %p", result, src)
	}

	result = NewResamplingSource(src, -8000, 16000, 1)
	if result != src {
		t.Errorf("expected original source for negative inRate, got %p vs %p", result, src)
	}

	result = NewResamplingSource(src, 8000, -16000, 1)
	if result != src {
		t.Errorf("expected original source for negative outRate, got %p vs %p", result, src)
	}
}

func TestResampling_PointerIdentity(t *testing.T) {
	src := &constantSource{values: []float32{0.5}}

	result := NewResamplingSource(src, 8000, 8000, 1)

	srcPtr := fmt.Sprintf("%p", src)
	resultPtr := fmt.Sprintf("%p", result)

	if srcPtr != resultPtr {
		t.Errorf("pointer identity mismatch: src=%s, result=%s", srcPtr, resultPtr)
	}
}
