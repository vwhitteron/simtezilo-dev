package synthesizer_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// These tests validate that samples written to the synthesizer's AdaptiveBuffer
// are read back intact over a long interval, so the source of audible pops and
// clicks (discontinuities in the read-side stream) can be localised. The signal
// is a strictly increasing integer ramp encoded as the sample value (value ==
// global write index), which makes every gap, repeat, or underrun zero-pad
// trivially detectable on the read side.

// seqBreak describes a discontinuity in the read-back ramp.
type seqBreak struct {
	Index int     // sample index in the read stream where the break was seen
	Kind  string  // "gap" | "repeat" | "zero"
	Want  float64 // ramp value expected at this point
	Got   float64 // value actually read
}

// detectSequenceBreaks treats underrun zeros as INSERTIONS rather than ramp
// members: it tracks only the non-zero samples and asserts they form a
// contiguous +1 ramp after an auto-detected constant latency. It resynchronises
// after every break so a single glitch does not cascade into thousands.
//
//   - "zero":   a 0 appeared mid-ramp (a consumer zero-padded an underrun short read)
//   - "gap":    the ramp jumped forward (samples were dropped, e.g. overflow)
//   - "repeat": the ramp went backward or stalled (stale/duplicated data)
func detectSequenceBreaks(read []float64) []seqBreak {
	var breaks []seqBreak

	prev := -1.0 // last accepted ramp value; < 0 means "not started"
	inZeroRun := false

	for idx, got := range read {
		if got == 0 {
			if prev >= 0 && !inZeroRun {
				// One break per contiguous run of inserted zeros.
				breaks = append(breaks, seqBreak{Index: idx, Kind: "zero", Want: prev + 1, Got: 0})
				inZeroRun = true
			}

			continue
		}

		inZeroRun = false

		if prev < 0 {
			// First real sample defines the (constant) latency offset.
			prev = got

			continue
		}

		switch want := prev + 1; {
		case got == want:
		case got > want:
			breaks = append(breaks, seqBreak{Index: idx, Kind: "gap", Want: want, Got: got})
		default:
			breaks = append(breaks, seqBreak{Index: idx, Kind: "repeat", Want: want, Got: got})
		}

		prev = got
	}

	return breaks
}

// writeRamp writes count ramp samples (in overwrite mode) starting at *next and
// advances *next. Overwrite mode is used because mix mode peak-limits and sums,
// which would corrupt the ramp encoding.
func writeRamp(buf *synthesizer.AdaptiveBuffer, next *float64, count int) {
	chunk := make([]float64, count)
	for i := range chunk {
		chunk[i] = *next
		*next++
	}

	buf.Write(chunk, 0, true)
}

// TestAdaptiveBuffer_RampPassThrough writes the ramp in varying chunk sizes and
// reads it back in different sizes, kept balanced so the buffer never starves or
// overflows. The read stream must be a clean, contiguous ramp: this establishes
// that the buffer itself is glitch-free when it is neither starved nor overrun.
func TestAdaptiveBuffer_RampPassThrough(t *testing.T) {
	t.Parallel()

	const internalRate = 8000

	buf := synthesizer.NewAdaptiveBuffer(2*time.Second, internalRate)

	writeSizes := []int{100, 256, 64, 333, 17}
	readSizes := []int{128, 200, 50, 256}

	var (
		readStream []float64
		ramp       = 1.0
		offset     int
	)

	for step := range 4000 {
		writeSize := writeSizes[step%len(writeSizes)]
		writeRamp(buf, &ramp, writeSize)

		// Read back exactly writeSize samples (in differently sized sub-reads) so the
		// buffer fill returns to baseline each step and never drifts into
		// overflow or underrun.
		toRead := writeSize
		for toRead > 0 {
			readSize := readSizes[offset%len(readSizes)]
			offset++

			if readSize > toRead {
				readSize = toRead
			}

			if avail := buf.Used(); readSize > avail {
				readSize = avail
			}

			if readSize == 0 {
				break
			}

			block := make([]float64, readSize)
			length := buf.Read(block)
			readStream = append(readStream, block[:length]...)
			toRead -= readSize
		}
	}

	for buf.Used() > 0 {
		block := make([]float64, buf.Used())
		length := buf.Read(block)
		readStream = append(readStream, block[:length]...)
	}

	breaks := detectSequenceBreaks(readStream)
	overflows, underruns, _ := buf.Health()

	if underruns != 0 || overflows != 0 {
		t.Fatalf("expected no underruns/overflows in balanced run, got underruns=%d overflows=%d", underruns, overflows)
	}

	if len(breaks) != 0 {
		t.Fatalf("ramp corrupted: %d break(s), first=%+v", len(breaks), breaks[0])
	}
}

// TestAdaptiveBuffer_UnderrunInsertsZeros proves the click mechanism: when a Read
// requests more than is buffered, the buffer returns a SHORT slice and bumps the
// underrun counter. Consumers (MixToMaster, Streamer.readOutputBuffers) then
// zero-pad the missing tail, inserting a silent step into a live signal — an
// audible click.
func TestAdaptiveBuffer_UnderrunInsertsZeros(t *testing.T) {
	t.Parallel()

	const internalRate = 8000

	buf := synthesizer.NewAdaptiveBuffer(2*time.Second, internalRate)

	ramp := 1.0
	writeRamp(buf, &ramp, 300)

	available := buf.Used()

	const request = 512

	block := make([]float64, request)
	length := buf.Read(block)
	got := block[:length]

	if len(got) >= request {
		t.Fatalf("expected a short read on underrun, requested %d got %d", request, len(got))
	}

	if len(got) != available {
		t.Fatalf("expected short read to return all %d buffered samples, got %d", available, len(got))
	}

	_, underruns, _ := buf.Health()
	if underruns == 0 {
		t.Fatal("expected underrun counter to increment")
	}

	// Reproduce what a consumer does: zero-pad to the requested length and show
	// the detector flags the inserted silence as a "zero" break.
	padded := make([]float64, request)
	copy(padded, got)

	breaks := detectSequenceBreaks(padded)

	sawZero := false

	for _, b := range breaks {
		if b.Kind == "zero" {
			sawZero = true

			break
		}
	}

	if !sawZero {
		t.Fatalf("expected a zero-insertion break from the zero-padded underrun, got %+v", breaks)
	}
}

// cadenceScenario parameterises the upstream-starvation simulation so each
// candidate fix can be measured against the same workload.
type cadenceScenario struct {
	name           string
	cushionMs      int  // writer lead the upstream buffer maintains (effective cushion)
	pull           int  // MixToMaster read size (audio.pullBlockFrames)
	ringMs         int  // async ring depth, output-rate equivalent, in ms
	silencePrefill bool // start the ring full of silence instead of pulling the synth
	pacedRefill    bool // refill at most one block per tick instead of greedily filling
}

type cadenceResult struct {
	startupUnderruns int
	steadyUnderruns  int
	breaks           int
}

// simulateUpstream runs a deterministic, seeded simulation of one upstream
// haptic buffer (chassis/engine/transmission) driven by the haptic write cadence
// on one side and the async ring's pull cadence on the other, for 10 s of audio.
// It returns the underruns observed during the startup window and afterwards,
// plus the number of discontinuities in the read-back ramp. No real time elapses,
// so it is fast and reproducible while still crossing the long periodic
// boundaries (config-watch 200 ms, health check 5 s).
func simulateUpstream(s cadenceScenario) cadenceResult {
	const (
		internalRate = 8000
		durationMs   = 10000
		warmupMs     = 200
		telemetryMs  = 16 // haptic generators write at ~60 Hz
		periodMs     = 66 // device callback period (one portaudio period)
	)

	periodFrames := periodMs * internalRate / 1000

	buf := synthesizer.NewAdaptiveBuffer(2*time.Second, internalRate)
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic test jitter

	ramp := 1.0

	// Build the requested cushion as the writer's lead: the buffer already holds
	// its built-in 24 ms (192-sample) readDelay, so top it up with real data.
	const builtInCushion = 192
	if lead := s.cushionMs*internalRate/1000 - builtInCushion; lead > 0 {
		writeRamp(buf, &ramp, lead)
	}

	ringCap := s.ringMs * internalRate / 1000

	var ringFill float64
	if s.silencePrefill {
		ringFill = float64(ringCap) // ring starts full of silence; synth is not pulled
	}

	pull := func() {
		block := make([]float64, s.pull)
		length := buf.Read(block)
		got := block[:length]

		out := make([]float64, s.pull) // consumer zero-pads short reads
		copy(out, got)
		simReadStream = append(simReadStream, out...)

		ringFill += float64(s.pull)
	}

	var (
		writeAccum     float64
		warmupUnderrun int
	)

	simReadStream = simReadStream[:0]

	for tick := range durationMs {
		writeAccum += float64(internalRate) / 1000.0

		// 5% of telemetry frames arrive late: nothing is written this tick, but
		// the owed samples stay in writeAccum and are flushed on the next frame
		// (a deferral, not a loss — the long-run write rate stays at 8 kHz).
		if tick%telemetryMs == 0 && rng.Float64() >= 0.05 {
			n := int(writeAccum)
			writeAccum -= float64(n)

			if n > 0 {
				writeRamp(buf, &ramp, n)
			}
		}

		// The real device callback removes a whole period at once (it does not
		// drain smoothly), so the producer must then refill a period-sized hole.
		// Model that bursty read rather than a smooth per-ms drain, because the
		// burst is what stresses the upstream buffer.
		if tick > 0 && tick%periodMs == 0 {
			ringFill -= float64(periodFrames)
			if ringFill < 0 {
				ringFill = 0
			}
		}

		// Refill the ring: greedily back to full (a burst right after the device
		// read), or at most one block per tick (paced).
		for ringFill <= float64(ringCap-s.pull) {
			pull()

			if s.pacedRefill {
				break
			}
		}

		if tick == warmupMs {
			_, warmupUnderrun, _ = buf.Health()
		}
	}

	_, total, _ := buf.Health()

	return cadenceResult{
		startupUnderruns: warmupUnderrun,
		steadyUnderruns:  total - warmupUnderrun,
		breaks:           len(detectSequenceBreaks(simReadStream)),
	}
}

// simReadStream is reused across scenarios to avoid reallocating a large slice
// per run; simulateUpstream truncates it at the start of each call.
var simReadStream []float64 //nolint:gochecknoglobals // test-only; shared across scenario runs to avoid large allocs

// TestAdaptiveBuffer_UpstreamStarvation_Scenarios measures the working
// hypothesis and every candidate fix against the same workload, printing a
// comparison table. The baseline reproduces the reported pops/clicks; the
// recommended fix (a deeper cushion plus a silence-prefilled, paced ring) must
// drive both startup and steady-state underruns to zero.
func TestAdaptiveBuffer_UpstreamStarvation_Scenarios(t *testing.T) {
	t.Parallel()

	const shipped = "shipped: prefill + pull128"

	scenarios := []cadenceScenario{
		{name: "baseline (old: no prefill)", cushionMs: 24, pull: 256, ringMs: 132},
		{name: shipped, cushionMs: 24, pull: 128, ringMs: 132, silencePrefill: true},
		{name: "shipped + 64ms cushion", cushionMs: 64, pull: 128, ringMs: 132, silencePrefill: true},
		{name: "prefill, old pull256", cushionMs: 24, pull: 256, ringMs: 132, silencePrefill: true},
		{name: "shrink ring, no prefill", cushionMs: 24, pull: 128, ringMs: 66},
	}

	t.Logf("%-32s %8s %8s %8s", "scenario", "startup", "steady", "breaks")

	var shippedResult cadenceResult

	for _, s := range scenarios {
		r := simulateUpstream(s)
		t.Logf("%-32s %8d %8d %8d", s.name, r.startupUnderruns, r.steadyUnderruns, r.breaks)

		if s.name == shipped {
			shippedResult = r
		}
	}

	// The shipped low-latency config (silence-prefilled ring + a pull smaller than
	// the default cushion) must eliminate both the startup burst and steady-state
	// underruns. The cushion knob (see "shipped + 64ms cushion") remains available
	// to add margin on real hardware.
	if shippedResult.startupUnderruns != 0 || shippedResult.steadyUnderruns != 0 {
		t.Fatalf("shipped fix still starves: startup=%d steady=%d underruns (expected 0/0)",
			shippedResult.startupUnderruns, shippedResult.steadyUnderruns)
	}
}
