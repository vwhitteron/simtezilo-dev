package synthesizer_test

import (
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// TestCapDepthBoundsEngineWritePattern reproduces the engine haptic's per-tick
// pattern: an overwrite write of two frames followed by the consumer draining
// roughly one frame before the next tick. Without a cap this appends faster than
// it drains and the buffer climbs to its full capacity (the ~2 s latency bug);
// CapDepth must hold the unread depth at the cushion while never starving it.
func TestCapDepthBoundsEngineWritePattern(t *testing.T) {
	t.Parallel()

	const (
		rate     = 8000
		frame    = rate / 30 // 266 samples per engine frame
		writeLen = frame * 2 // engine generates two frames per tick
		capDepth = frame * 3 // engineBufferFrames cushion
		ticks    = 400
	)

	samples := make([]float64, writeLen)
	for i := range samples {
		samples[i] = 1 // non-zero content so reads are meaningful
	}

	// With the cap: unread depth stays bounded and the consumer never starves.
	capped := synthesizer.NewAdaptiveBuffer(2*time.Second, rate)

	maxUsed := 0
	minUsed := 1 << 30

	for range ticks {
		capped.Write(samples, 0, true) // overwrite mode, as the engine does
		capped.CapDepth(capDepth)
		capped.Read(frame) // consumer drains ~one frame between engine ticks

		used := capped.Used()
		if used > maxUsed {
			maxUsed = used
		}

		if used < minUsed {
			minUsed = used
		}
	}

	if maxUsed > capDepth {
		t.Errorf("capped buffer grew to %d samples, want <= cap %d", maxUsed, capDepth)
	}

	if minUsed <= 0 {
		t.Errorf("capped buffer starved (min used %d); cushion too small for the drain", minUsed)
	}

	// Without the cap the same pattern accumulates well past the cushion,
	// confirming the cap is what bounds it.
	uncapped := synthesizer.NewAdaptiveBuffer(2*time.Second, rate)
	for range ticks {
		uncapped.Write(samples, 0, true)
		uncapped.Read(frame)
	}

	if uncapped.Used() <= capDepth {
		t.Errorf("uncapped buffer used %d, expected runaway growth past cap %d", uncapped.Used(), capDepth)
	}
}
