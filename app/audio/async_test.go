package audio_test

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

// rampSource emits a strictly increasing per-channel ramp so the consumer can
// verify it receives every produced sample in order with none lost or repeated.
type rampSource struct {
	mu       sync.Mutex
	next     float32
	channels int
}

func (r *rampSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	frames := len(buf) / channels
	for f := range frames {
		for c := range channels {
			buf[f*channels+c] = r.next
		}

		r.next++
	}

	return frames, true
}

// TestAsyncSource_OrderedDelivery drains the async source and checks that the
// non-silence samples it yields form the original contiguous ramp: the ring
// neither drops nor duplicates buffered samples across wraparound.
func TestAsyncSource_OrderedDelivery(t *testing.T) {
	t.Parallel()

	const channels = 2

	src := &rampSource{channels: channels}

	source := audio.NewAsyncSource(src, channels, 200, 128, 64, audio.RealtimeConfig{})
	defer source.Close()

	out := make([]float32, 128) // 64 frames per read

	var (
		want    float32
		started bool
		reads   int
	)

	deadline := time.After(2 * time.Second)

	for reads < 200 {
		select {
		case <-deadline:
			t.Fatalf("timed out after %d reads", reads)
		default:
		}

		source.ReadInterleaved(out, channels)

		reads++

		for frame := range len(out) / channels {
			got := out[frame*channels]

			// Skip leading/underrun silence frames (value 0 before the ramp has
			// started flowing). Once the ramp begins it must stay contiguous.
			if !started {
				if got == 0 {
					continue
				}

				started = true
				want = got
			}

			if got != want {
				t.Fatalf("read %d frame %d: expected %v, got %v", reads, frame, want, got)
			}

			if out[frame*channels+1] != want {
				t.Fatalf("read %d frame %d: channel mismatch %v vs %v", reads, frame, out[frame*channels+1], want)
			}

			want++
		}

		// Let the producer refill between reads so we exercise wraparound rather
		// than perpetually underrunning.
		time.Sleep(time.Millisecond)
	}

	if !started {
		t.Fatal("never received any produced samples")
	}
}

// countingSource counts ReadInterleaved calls and always returns a non-zero constant.
type countingSource struct {
	calls atomic.Int64
	value float32
}

func (c *countingSource) ReadInterleaved(buf []float32, _ int) (int, bool) {
	c.calls.Add(1)

	for i := range buf {
		buf[i] = c.value
	}

	return len(buf), true
}

// TestAsyncSource_IdleGate asserts that when SetIdleCheck returns true the
// consumer reads zeros (silence) and inner.ReadInterleaved is NOT called.
func TestAsyncSource_IdleGate(t *testing.T) {
	t.Parallel()

	const channels = 2

	inner := &countingSource{value: 0.5}

	source := audio.NewAsyncSource(inner, channels, 400, 256, 64, audio.RealtimeConfig{})
	source.SetIdleCheck(func() bool { return true })

	defer source.Close()

	// Drain the initial silence pre-fill: the ring starts pre-filled to the target
	// with zeros, so we consume those before checking the idle path produces zeros.
	drain := make([]float32, 400*channels)
	source.ReadInterleaved(drain, channels)

	// Give the producer a moment to run with idle=true and refill the ring.
	time.Sleep(20 * time.Millisecond)

	callsBefore := inner.calls.Load()

	out := make([]float32, 128*channels)
	source.ReadInterleaved(out, channels)

	for i, v := range out {
		if v != 0 {
			t.Fatalf("sample %d: expected 0 (silence), got %v", i, v)
		}
	}

	callsAfter := inner.calls.Load()
	if callsAfter != callsBefore {
		t.Fatalf("inner.ReadInterleaved was called %d time(s) while idle gate was active",
			callsAfter-callsBefore)
	}
}

// TestAsyncSource_RealtimeFallback checks that a scheduling request the machine
// will not grant is non-fatal: audio must still flow, and Health must report the
// reason. Developer machines and unprivileged CI both take this path, so the
// fallback is the common case rather than an edge case.
func TestAsyncSource_RealtimeFallback(t *testing.T) {
	t.Parallel()

	const channels = 2

	src := &rampSource{channels: channels}

	// Priority 99 needs CAP_SYS_NICE and a raised RLIMIT_RTPRIO. The test asserts
	// behaviour in both outcomes rather than requiring one, so it is valid whether
	// or not the runner happens to be privileged.
	source := audio.NewAsyncSource(src, channels, 200, 128, 64, audio.RealtimeConfig{Priority: 99})
	defer source.Close()

	if !waitForAudio(source, channels) {
		t.Fatal("no audio produced within the deadline")
	}

	health := source.Health()
	if runtime.GOOS == "linux" && !health.RealtimeApplied && health.RealtimeError == "" {
		t.Fatal("realtime was neither applied nor reported as failed on linux")
	}

	if health.RealtimeApplied && health.RealtimePriority != 99 {
		t.Fatalf("realtime priority = %d, want 99", health.RealtimePriority)
	}
}

// waitForAudio drains the source until a non-silent sample appears, proving the
// producer is running. It reports false if nothing arrives before the deadline.
func waitForAudio(source *audio.AsyncSource, channels int) bool {
	out := make([]float32, 128)
	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		source.ReadInterleaved(out, channels)

		for _, v := range out {
			if v != 0 {
				return true
			}
		}

		time.Sleep(time.Millisecond)
	}

	return false
}

// TestAsyncSource_MinFillTracksMargin checks the low-water mark falls as the
// consumer drains the ring, and that ResetPeak restores it. MinFill is the
// primary metric for judging producer scheduling changes, so a regression here
// would silently invalidate every before/after measurement.
func TestAsyncSource_MinFillTracksMargin(t *testing.T) {
	t.Parallel()

	const channels = 2

	src := &rampSource{channels: channels}

	source := audio.NewAsyncSource(src, channels, 200, 128, 64, audio.RealtimeConfig{})
	defer source.Close()

	if got := source.Health().MinFill; got != 200*channels {
		t.Fatalf("MinFill before any read = %d, want capacity %d", got, 200*channels)
	}

	// Drain far more than the ring holds so the fill is driven to zero.
	out := make([]float32, 400*channels)
	source.ReadInterleaved(out, channels)

	health := source.Health()
	if health.MinFill != 0 {
		t.Fatalf("MinFill after over-draining = %d, want 0", health.MinFill)
	}

	if health.FillBuckets[0] == 0 {
		t.Fatal("lowest fill bucket was not counted")
	}

	source.ResetPeak()

	health = source.Health()
	if health.MinFill != 200*channels {
		t.Fatalf("MinFill after ResetPeak = %d, want capacity %d", health.MinFill, 200*channels)
	}

	if health.FillBuckets[0] != 0 {
		t.Fatalf("FillBuckets[0] after ResetPeak = %d, want 0", health.FillBuckets[0])
	}
}

// BenchmarkAsyncSourceReadInterleaved guards the realtime callback path. The
// fill counters added for scheduling measurement run on every callback, so they
// must not allocate: an allocation here can trigger a GC pause inside the device
// callback and cause the dropout the counters exist to detect.
func BenchmarkAsyncSourceReadInterleaved(b *testing.B) {
	const channels = 2

	src := &rampSource{channels: channels}

	source := audio.NewAsyncSource(src, channels, 4096, 2048, 64, audio.RealtimeConfig{})
	defer source.Close()

	out := make([]float32, 128)

	b.ReportAllocs()

	for b.Loop() {
		source.ReadInterleaved(out, channels)
	}
}
