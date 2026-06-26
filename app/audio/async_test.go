package audio_test

import (
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

	source := audio.NewAsyncSource(src, channels, 200, 128, 64)
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

	source := audio.NewAsyncSource(inner, channels, 400, 256, 64)
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
