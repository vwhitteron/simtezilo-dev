package audio

import (
	"sync"
	"testing"
	"time"
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
	const channels = 2

	src := &rampSource{channels: channels}
	a := NewAsyncSource(src, channels, 200, 64)
	defer a.Close()

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

		a.ReadInterleaved(out, channels)
		reads++

		for f := 0; f < len(out)/channels; f++ {
			v := out[f*channels]

			// Skip leading/underrun silence frames (value 0 before the ramp has
			// started flowing). Once the ramp begins it must stay contiguous.
			if !started {
				if v == 0 {
					continue
				}

				started = true
				want = v
			}

			if v != want {
				t.Fatalf("read %d frame %d: expected %v, got %v", reads, f, want, v)
			}

			if out[f*channels+1] != want {
				t.Fatalf("read %d frame %d: channel mismatch %v vs %v", reads, f, out[f*channels+1], want)
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
