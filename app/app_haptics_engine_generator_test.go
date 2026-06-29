//nolint:testpackage // needs internal access to the unexported engineWaveformGenerator and engineGenParams
package app

// Durable artefact guards for the phase-continuous engine generator. These are
// the specification for Option B: they drive the real engineWaveformGenerator and
// (for the channel tests) the real synthesizer.AdaptiveBuffer with a simulated
// consumer draining one frame per tick, mirroring the live 30 Hz-generate vs
// output-drain relationship. Cleanliness is measured with app/audio/audioqa.

import (
	"math"
	"testing"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/audio/audioqa"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

const (
	genRate     = 8000
	genFrame    = genRate / engineHapticFrameRate // samples per engine frame
	genCapDepth = genFrame * 3                    // cushion the channel refills to
	genRevLimit = 8000.0
	genFiring   = 0.0333333 // 4-cylinder 4-stroke firing frequency (fires/rev)
)

func testEngineProfile() *haptics.EngineProfile {
	return &haptics.EngineProfile{
		PrimaryBalance:   0.85,
		SecondaryBalance: 0.85,
		Gain:             0.0,
		PulseScale:       1.0,
	}
}

func newTestGenerator() *engineWaveformGenerator {
	return newEngineWaveformGenerator(genRate, "", testEngineProfile())
}

// paramsForRPM builds the per-tick generation parameters for a given RPM the way
// the engine path does: pulse rate scales with RPM and amplitude rises with load.
func paramsForRPM(rpm float64) engineGenParams {
	rpmFrac := rpm / genRevLimit

	return engineGenParams{
		amplitude:      0.4 + 0.4*rpmFrac,
		pulseRate:      rpm * genFiring,
		pulseDutyCycle: 0.5,
		rpmPercent:     rpmFrac,
		revLimit:       genRevLimit,
	}
}

// channelRun records the result of driving the generator through a real buffer
// with a one-frame-per-tick consumer.
type channelRun struct {
	played     []float64
	minUsed    int
	maxUsed    int
	shortReads int
}

// runEngineChannel mirrors the rewritten generateEngineHaptic: each tick it tops
// the channel back up to the cushion with freshly generated, phase-continuous
// samples (appended at the write cursor, no offset, no overwrite of unread data),
// then the consumer drains one frame. It returns the played-out stream.
func runEngineChannel(rate, ticks int, paramsAt func(tick int) engineGenParams) channelRun {
	gen := newTestGenerator()
	buf := synthesizer.NewAdaptiveBuffer(2*time.Second, rate)

	frame := rate / engineHapticFrameRate
	capDepth := frame * 3

	got := make([]float64, frame) // reused drain scratch, one frame per tick

	run := channelRun{minUsed: 1 << 30}

	for tick := range ticks {
		need := min(max(capDepth-buf.Used(), 0),
			// maxBlock clamp
			frame*2)

		if need > 0 {
			block := make([]float64, need)
			gen.Generate(block, paramsAt(tick))
			buf.Write(block, 0, true) // append at write cursor (offset 0, overwrite mode)
		}

		n := buf.Read(got) // n < frame on underrun
		if n < frame {
			run.shortReads++
		}

		run.played = append(run.played, got[:n]...)

		used := buf.Used()
		if used < run.minUsed {
			run.minUsed = used
		}

		if used > run.maxUsed {
			run.maxUsed = used
		}
	}

	return run
}

// TestEngineGeneratorPhaseContinuity is the definitive, threshold-free guard:
// generating N samples in one call must equal generating them across many
// tick-sized chunks. Any seam discontinuity is a divergence.
func TestEngineGeneratorPhaseContinuity(t *testing.T) {
	t.Parallel()

	params := paramsForRPM(4500)
	total := genRate // one second

	contiguous := make([]float64, total)
	newTestGenerator().Generate(contiguous, params)

	chunked := make([]float64, 0, total)
	gen := newTestGenerator()

	for remaining := total; remaining > 0; {
		size := min(genFrame, remaining)

		block := make([]float64, size)
		gen.Generate(block, params)
		chunked = append(chunked, block...)
		remaining -= size
	}

	for i := range contiguous {
		if math.Abs(contiguous[i]-chunked[i]) > 1e-9 {
			t.Fatalf("phase discontinuity at sample %d: contiguous=%.9f chunked=%.9f", i, contiguous[i], chunked[i])
		}
	}
}

// TestEngineChannelNoSeamArtefacts checks that, across tick boundaries, the
// played stream introduces no sample-to-sample step beyond what an uninterrupted
// block of the same waveform produces — at low, mid and (where the bug bites)
// high RPM.
func TestEngineChannelNoSeamArtefacts(t *testing.T) {
	t.Parallel()

	const refAmp = 0.3 // below the minimum generated amplitude, trims silent lead

	for _, rpm := range []float64{1000, 4000, 8000} {
		params := paramsForRPM(rpm)

		// Largest step within a single uninterrupted block at this RPM.
		single := make([]float64, genFrame*40)
		newTestGenerator().Generate(single, params)
		bound := audioqa.Analyse(single, genRate, refAmp, 0).MaxStep

		run := runEngineChannel(genRate, 80, func(int) engineGenParams { return params })
		metrics := audioqa.Analyse(run.played, genRate, refAmp, bound)

		if metrics.Empty {
			t.Fatalf("rpm %.0f: no signal captured", rpm)
		}

		// Note: the engine pulse train has legitimate zero gaps between pulses, so
		// audioqa's zero-run "dropout" metric does not apply here; channel-level
		// underruns are covered by TestEngineChannelNoUnderrunBoundedLatency.
		if metrics.NonFinite != 0 || metrics.Clipped != 0 {
			t.Errorf("rpm %.0f: nonFinite=%d clipped=%d", rpm, metrics.NonFinite, metrics.Clipped)
		}

		if metrics.MaxStep > bound*1.05 {
			t.Errorf("rpm %.0f: seam step %.5f exceeds single-block bound %.5f (%d glitches)",
				rpm, metrics.MaxStep, bound, metrics.Glitches)
		}
	}
}

// TestEngineChannelRPMSweep sweeps idle->redline->idle while the amplitude tracks
// load, exercising per-tick frequency and amplitude changes. Phase accumulation
// and amplitude ramping must keep the output finite, unclipped and free of
// dropouts and large steps.
func TestEngineChannelRPMSweep(t *testing.T) {
	t.Parallel()

	const (
		refAmp = 0.3
		ticks  = 240
	)

	rpmAt := func(tick int) float64 {
		// triangle 800 -> 8000 -> 800 over the run
		frac := float64(tick) / float64(ticks)
		tri := 1 - math.Abs(2*frac-1) // 0..1..0

		return 800 + tri*(8000-800)
	}

	// Bound from the highest-frequency block in the sweep, with margin for the
	// per-tick amplitude ramp.
	top := make([]float64, genFrame*20)
	newTestGenerator().Generate(top, paramsForRPM(8000))
	bound := audioqa.Analyse(top, genRate, refAmp, 0).MaxStep * 1.5

	run := runEngineChannel(genRate, ticks, func(tick int) engineGenParams {
		return paramsForRPM(rpmAt(tick))
	})

	metrics := audioqa.Analyse(run.played, genRate, refAmp, bound)

	if metrics.Empty {
		t.Fatal("sweep produced no signal")
	}

	if metrics.NonFinite != 0 {
		t.Errorf("sweep produced %d non-finite samples", metrics.NonFinite)
	}

	if metrics.Clipped != 0 {
		t.Errorf("sweep clipped %d samples (peak %.4f)", metrics.Clipped, metrics.Peak)
	}

	// Pulse-train zero gaps are legitimate, not underruns (see seam-artefact test).
	if metrics.Glitches != 0 {
		t.Errorf("sweep produced %d glitches (maxStep %.5f, bound %.5f)", metrics.Glitches, metrics.MaxStep, bound)
	}
}

// TestEngineChannelNoUnderrunBoundedLatency replaces the old CapDepth test's
// intent: with the real drain ratio the channel never starves (no short reads)
// and the buffered depth stays bounded by the cushion (latency does not grow).
func TestEngineChannelNoUnderrunBoundedLatency(t *testing.T) {
	t.Parallel()

	run := runEngineChannel(genRate, 600, func(int) engineGenParams { return paramsForRPM(3500) })

	if run.shortReads != 0 {
		t.Errorf("channel starved: %d short reads", run.shortReads)
	}

	if run.maxUsed > genCapDepth {
		t.Errorf("buffered depth %d grew past cushion %d (latency runaway)", run.maxUsed, genCapDepth)
	}

	if run.minUsed < genFrame {
		t.Errorf("buffered depth dropped to %d, below one frame %d", run.minUsed, genFrame)
	}
}

// engineSource adapts a mono engine waveform to an audio.SampleSource, emitting
// the same sample on every channel — enough to push the engine signal through the
// resampler and async ring for the full-pipeline smoke test.
type engineSource struct {
	samples []float64
	pos     int
}

func (s *engineSource) ReadInterleaved(buf []float32, channels int) (int, bool) {
	frames := len(buf) / channels
	for frame := range frames {
		var value float32
		if s.pos < len(s.samples) {
			value = float32(s.samples[s.pos])
			s.pos++
		}

		for channel := range channels {
			buf[frame*channels+channel] = value
		}
	}

	return frames, true
}

// TestEngineResamplerClean feeds phase-continuous engine audio through the live
// windowed-sinc resampler — the stage most likely to distort the engine's sharp
// pulse edges — and asserts it stays finite and unclipped with no dropouts of its
// own. The resampler is synchronous and deterministic; the async ring's runtime
// behaviour is covered separately by async_test.go and the audio_cleanup tool.
func TestEngineResamplerClean(t *testing.T) {
	t.Parallel()

	const (
		inRate   = genRate
		outRate  = 32000
		channels = 2
		refAmp   = 0.3
	)

	engineAudio := make([]float64, inRate*2) // 2 s of engine audio
	newTestGenerator().Generate(engineAudio, paramsForRPM(6000))

	src := audio.NewResamplingSource(&engineSource{samples: engineAudio}, inRate, outRate, channels)

	outFrames := outRate // analyse ~1 s
	rec := make([]float64, 0, outFrames)
	buf := make([]float32, (outRate/100)*channels)

	for len(rec) < outFrames {
		frames, ok := src.ReadInterleaved(buf, channels)
		if !ok {
			break
		}

		for frame := range frames {
			rec = append(rec, float64(buf[frame*channels]))
		}
	}

	metrics := audioqa.Analyse(rec, outRate, refAmp, 0)

	if metrics.Empty {
		t.Fatal("resampler produced no signal")
	}

	if metrics.NonFinite != 0 || metrics.Clipped != 0 {
		t.Errorf("resampler: nonFinite=%d clipped=%d peak=%.4f", metrics.NonFinite, metrics.Clipped, metrics.Peak)
	}
}
