package audio

import (
	"math"
	"time"
)

// DefaultTestToneHz is the frequency used for device/channel test tones. It sits
// in the haptic band so it is felt as well as (faintly) heard.
const DefaultTestToneHz = 60.0

// testToneSource is a self-contained sine generator that emits a tone on a single
// channel (all others silent) for a fixed number of frames, then silence. It is
// deliberately independent of the synthesizer/calibrator so the audio package
// has no upward dependencies.
type testToneSource struct {
	channels   int
	channel    int // target channel index; -1 means all channels
	sampleRate int
	freq       float64
	gain       float64
	remaining  int // frames of tone left to emit
	phase      float64
}

func (t *testToneSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	frames := len(out) / channels
	inc := 2 * math.Pi * t.freq / float64(t.sampleRate)

	for f := range frames {
		var sample float32
		if t.remaining > 0 {
			sample = float32(math.Sin(t.phase) * t.gain)

			t.phase += inc
			if t.phase > 2*math.Pi {
				t.phase -= 2 * math.Pi
			}

			t.remaining--
		}

		for c := range channels {
			if t.channel < 0 || c == t.channel {
				out[f*channels+c] = sample
			} else {
				out[f*channels+c] = 0
			}
		}
	}

	return frames, true
}

// PlayTestTone opens a sink on the given backend and plays a short sine tone on
// the requested channel (channel < 0 targets all channels), blocking until the
// tone has finished. It is used by the Web UI to verify device/channel wiring.
func PlayTestTone(backend Backend, cfg SinkConfig, channel int, freq float64, dur time.Duration) error {
	if freq <= 0 {
		freq = DefaultTestToneHz
	}

	if cfg.Channels <= 0 {
		cfg.Channels = 2
	}

	sink, err := backend.OpenSink(cfg)
	if err != nil {
		return err
	}

	defer func() { _ = sink.Stop() }()

	src := &testToneSource{
		channels:   sink.Channels(),
		channel:    channel,
		sampleRate: cfg.SampleRate,
		freq:       freq,
		gain:       0.3,
		remaining:  int(float64(cfg.SampleRate) * dur.Seconds()),
	}

	if err := sink.Start(src); err != nil {
		return err
	}

	// Let the tone play out, plus a short tail so the device buffer drains.
	time.Sleep(dur + 200*time.Millisecond)

	return nil
}
