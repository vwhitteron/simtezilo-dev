// Command audio_cleanup is a diagnostic harness for the haptic audio output
// pipeline. The live output (both beep and portaudio backends) exhibits a small
// amount of artifacting; this tool feeds a well-understood signal (a pure sine)
// through the real pipeline stages used by the app — the windowed-sinc resampler
// (audio.NewResamplingSource) and the async ring buffer (audio.NewAsyncSource) —
// captures the result, and measures how far the output deviates from the input.
//
// Because the actual DAC output of beep/portaudio cannot be read back in-process,
// the default "capture" backend is a memory sink that implements audio.Backend:
// it exercises every stage of OUR code (resample, ring buffer, the interleaved
// float32 transport) deterministically and records exactly what a device callback
// would have received. Running each stage in isolation localises where artifacts
// are introduced:
//
//	control  : sine -> capture                       (baseline; should be clean)
//	resample : sine -> resample -> capture           (isolates the resampler)
//	async    : sine -> async    -> capture           (isolates the ring buffer)
//	full     : sine -> resample -> async -> capture  (the live app pipeline)
//
// For each stage it reports clipping, NaN/Inf, dropouts (underrun zero-runs),
// sample-to-sample discontinuities, the recovered fundamental amplitude (gain),
// and the residual after subtracting the best-fit fundamental — i.e. the
// distortion+noise the stage added, expressed as a peak error and an SNR.
//
// The beep and portaudio backends can also be selected (-backend) to play the
// same pipeline audibly for a listening check; those paths drive a real device so
// there is nothing to capture or analyse.
//
//	go run ./tools/audio_cleanup                       # analyse all stages
//	go run ./tools/audio_cleanup -wav out              # dump out-<stage>.wav
//	go run -tags portaudio ./tools/audio_cleanup -backend portaudio -dur 3
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"slices"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/audio/audioqa"
)

func main() {
	cfg := parseFlags()

	if cfg.mode == "latency" {
		err := runLatency(cfg)
		if err != nil {
			fail(err)
		}

		return
	}

	switch cfg.backend {
	case "capture":
		err := runCapture(cfg)
		if err != nil {
			fail(err)
		}
	case audio.BackendBeep, audio.BackendPortAudio:
		err := runAudible(cfg)
		if err != nil {
			fail(err)
		}
	default:
		fail(fmt.Errorf("unknown -backend %q (want capture, beep or portaudio)", cfg.backend))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// config holds the parsed command-line parameters.
type config struct {
	inRate    int     // synthesizer-side ("internal") sample rate
	outRate   int     // device output sample rate
	channels  int     // output channel count
	freq      float64 // test tone frequency (Hz)
	amp       float64 // test tone amplitude (0..1)
	dur       float64 // seconds of signal to analyse
	latencyMs int     // requested device latency (drives ring/buffer sizing)
	devBuf    int     // device callback buffer in frames (0 -> derive from latency)
	realtime  bool    // pace the capture pulls at wall-clock rate
	tol       float64 // max acceptable peak residual vs the fitted fundamental
	minSNR    float64 // min acceptable fundamental-to-residual SNR (dB)
	backend   string  // capture | beep | portaudio
	stage     string  // all | control | resample | async | full
	wav       string  // when set, dump <wav>-<stage>.wav per stage
	mode      string  // analyse (stage capture/audible) | latency
	interval  float64 // latency mode: seconds between injected markers
	threshold float64 // latency mode: |sample| impulse-detection threshold
}

func parseFlags() config {
	var cfg config

	flag.IntVar(&cfg.inRate, "in", 8000, "internal (synthesizer) sample rate in Hz")
	flag.IntVar(&cfg.outRate, "out", 32000, "device output sample rate in Hz")
	flag.IntVar(&cfg.channels, "channels", 2, "output channel count")
	flag.Float64Var(&cfg.freq, "freq", audio.DefaultTestToneHz, "test tone frequency in Hz")
	flag.Float64Var(&cfg.amp, "amp", 0.5, "test tone amplitude (0..1)")
	flag.Float64Var(&cfg.dur, "dur", 2.0, "seconds of signal to analyse/play")
	flag.IntVar(&cfg.latencyMs, "latency", 50, "requested device latency in ms")
	flag.IntVar(&cfg.devBuf, "devbuf", 0, "device callback buffer in frames (0 = derive from latency)")
	flag.BoolVar(&cfg.realtime, "realtime", true, "pace capture pulls at wall-clock rate (reveals real underruns)")
	flag.Float64Var(&cfg.tol, "tol", 0.02, "max acceptable peak residual vs fitted fundamental")
	flag.Float64Var(&cfg.minSNR, "snr", 50, "min acceptable fundamental-to-residual SNR in dB")
	flag.StringVar(&cfg.backend, "backend", "capture", "capture (analyse) | beep | portaudio (audible)")
	flag.StringVar(&cfg.stage, "stage", "all", "all | control | resample | async | full")
	flag.StringVar(&cfg.wav, "wav", "", "when set, write <wav>-<stage>.wav for each captured stage")
	flag.StringVar(&cfg.mode, "mode", "analyse", "analyse (stage capture/audible) | latency (measure event->read delay)")
	flag.Float64Var(&cfg.interval, "interval", 0.5, "latency mode: seconds between injected gear-change markers")
	flag.Float64Var(&cfg.threshold, "threshold", 0.5, "latency mode: |sample| impulse-detection threshold")

	flag.Parse()

	return cfg
}

// ---------------------------------------------------------------------------
// Signal source
// ---------------------------------------------------------------------------

// sineSource emits an endless pure sine of the given frequency on every channel.
// It is the "well understood signal": a single spectral line whose amplitude and
// frequency the analysis can recover exactly, so any energy the pipeline adds
// elsewhere is, by definition, an artifact.
type sineSource struct {
	freq, amp float64
	rate      float64
	phase     float64
}

func (s *sineSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	frames := len(out) / channels
	inc := 2 * math.Pi * s.freq / s.rate

	for frame := range frames {
		value := float32(s.amp * math.Sin(s.phase))

		s.phase += inc
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}

		for c := range channels {
			out[frame*channels+c] = value
		}
	}

	return frames, true
}

// ---------------------------------------------------------------------------
// Capture backend: an audio.Backend that records instead of playing
// ---------------------------------------------------------------------------

type captureBackend struct{}

func (captureBackend) Name() string { return "capture" }

func (captureBackend) ListDevices() ([]audio.Device, error) {
	return []audio.Device{{ID: "", Name: "Memory capture", Backend: "capture", IsDefault: true}}, nil
}

func (captureBackend) Close() error { return nil }

func (captureBackend) OpenSink(cfg audio.SinkConfig) (audio.Sink, error) {
	return &captureSink{channels: cfg.Channels, rate: cfg.SampleRate}, nil
}

// captureSink pulls from the source exactly like a real device callback —
// fixed-size buffers, optionally paced at wall-clock rate — and appends every
// sample it receives to rec. Start blocks until totalFrames have been captured.
type captureSink struct {
	channels    int
	rate        int
	blockFrames int
	totalFrames int
	realtime    bool

	rec []float32
}

func (s *captureSink) Channels() int { return s.channels }
func (s *captureSink) Stop() error   { return nil }

func (s *captureSink) Start(src audio.SampleSource) error {
	buf := make([]float32, s.blockFrames*s.channels)
	period := time.Duration(float64(s.blockFrames) / float64(s.rate) * float64(time.Second))
	next := time.Now()

	for captured := 0; captured < s.totalFrames; {
		frames, _ := src.ReadInterleaved(buf, s.channels)

		count := frames
		if captured+count > s.totalFrames {
			count = s.totalFrames - captured
		}

		s.rec = append(s.rec, buf[:count*s.channels]...)
		captured += count

		if s.realtime {
			next = next.Add(period)
			if d := time.Until(next); d > 0 {
				time.Sleep(d)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Capture run: build each stage's pipeline, capture, analyse
// ---------------------------------------------------------------------------

func runCapture(cfg config) error {
	stages := stageList(cfg.stage)
	if stages == nil {
		return fmt.Errorf("unknown -stage %q", cfg.stage)
	}

	// The resample/full stages generate the tone at the internal rate before
	// up-sampling, so a tone at or above the internal Nyquist cannot be represented
	// and will alias — a test-input error that would masquerade as a resampler bug.
	if usesInternalRate(stages) && cfg.freq >= float64(cfg.inRate)/2 {
		return fmt.Errorf("freq %.0f Hz is at/above the internal Nyquist %.0f Hz (inRate %d); "+
			"lower -freq, raise -in, or test only -stage control/async",
			cfg.freq, float64(cfg.inRate)/2, cfg.inRate)
	}

	capacity, target, block := bufferFrames(cfg.outRate, cfg.latencyMs)

	devBuf := cfg.devBuf
	if devBuf <= 0 {
		devBuf = block
	}

	fmt.Fprintf(os.Stdout, "input : %.1f Hz sine, amp %.3f, %d ch\n", cfg.freq, cfg.amp, cfg.channels)
	fmt.Fprintf(os.Stdout, "rates : internal %d Hz -> output %d Hz (ratio %.3gx)\n", cfg.inRate, cfg.outRate, float64(cfg.outRate)/float64(cfg.inRate))
	fmt.Fprintf(os.Stdout, "buffer: ring capacity %d frames, target %d frames, producer block %d frames, device buffer %d frames\n",
		capacity, target, block, devBuf)
	fmt.Fprintf(os.Stdout, "expect: peak residual <= %.4f, SNR >= %.0f dB\n\n", cfg.tol, cfg.minSNR)

	var failures int

	for _, stage := range stages {
		rec, err := captureStage(cfg, stage, capacity, target, block, devBuf)
		if err != nil {
			return fmt.Errorf("stage %s: %w", stage, err)
		}

		if cfg.wav != "" {
			path := fmt.Sprintf("%s-%s.wav", cfg.wav, stage)

			err := writeWAV(path, rec, cfg.channels, cfg.outRate)
			if err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		}

		m, tone := audioqa.AnalyseTone(channel0(rec, cfg.channels), cfg.outRate, cfg.freq, cfg.amp)
		ok := report(stage, cfg, m, tone)

		if !ok {
			failures++
		}
	}

	fmt.Fprintln(os.Stdout)

	if failures == 0 {
		fmt.Fprintln(os.Stdout, "PASS: all stages within tolerance")

		return nil
	}

	fmt.Fprintf(os.Stdout, "FAIL: %d stage(s) introduced artifacts above tolerance\n", failures)
	os.Exit(2)

	return nil
}

// stageNames maps the requested -stage to the ordered list to run.
func stageList(name string) []string {
	all := []string{"control", "resample", "async", "full"}

	switch name {
	case "all":
		return all
	case "control", "resample", "async", "full":
		return []string{name}
	default:
		return nil
	}
}

// usesInternalRate reports whether any stage in the list runs the resampler, and
// therefore generates the tone at the internal (pre-up-sample) rate.
func usesInternalRate(stages []string) bool {
	for _, s := range stages {
		if s == "resample" || s == "full" {
			return true
		}
	}

	return false
}

// captureStage assembles the pipeline for one stage and returns the interleaved
// captured samples. The sine is generated at whichever rate that stage feeds the
// device: at the output rate when no resampler is present, at the internal rate
// when one is (so the resampler does the inRate->outRate conversion the app does).
func captureStage(cfg config, stage string, capacity, target, block, devBuf int) ([]float32, error) {
	resampling := stage == "resample" || stage == "full"

	srcRate := cfg.outRate
	if resampling {
		srcRate = cfg.inRate
	}

	var src audio.SampleSource = &sineSource{freq: cfg.freq, amp: cfg.amp, rate: float64(srcRate)}

	if resampling {
		src = audio.NewResamplingSource(src, cfg.inRate, cfg.outRate, cfg.channels)
	}

	var closeAsync func()

	if stage == "async" || stage == "full" {
		async := audio.NewAsyncSource(src, cfg.channels, capacity, target, block)
		src = async
		closeAsync = async.Close
	}

	// Drive the capture through the audio.Backend abstraction, exactly as the app
	// drives beep/portaudio: open a sink for the requested format, then Start it on
	// the source chain. The capture-specific knobs (pacing, how long to record) are
	// set on the concrete sink after opening.
	sinkIface, err := captureBackend{}.OpenSink(audio.SinkConfig{Channels: cfg.channels, SampleRate: cfg.outRate})
	if err != nil {
		return nil, err
	}

	sink := sinkIface.(*captureSink) //nolint:forcetypeassert // captureBackend only ever returns *captureSink
	sink.blockFrames = devBuf
	sink.realtime = cfg.realtime
	// Capture the requested duration plus enough lead to cover the async ring's
	// pre-filled silence; analyse() trims the silent head and tail.
	sink.totalFrames = int(cfg.dur*float64(cfg.outRate)) + 2*capacity

	err = sink.Start(src)

	if closeAsync != nil {
		closeAsync()
	}

	if err != nil {
		return nil, err
	}

	return sink.rec, nil
}

// bufferFrames mirrors the app's hapticBufferFrames so the harness exercises the
// same ring depth and producer block size the live pipeline uses.
func bufferFrames(outputRate, latencyMs int) (capacity, target, block int) {
	if latencyMs <= 0 {
		latencyMs = 50
	}

	periodFrames := outputRate * latencyMs / 1000
	block = max(outputRate/100, 256)
	target = max(periodFrames, block)
	capacity = max(periodFrames*2, block*4)

	return capacity, target, block
}

// ---------------------------------------------------------------------------
// Analysis
// ---------------------------------------------------------------------------

// report prints one stage's metrics and tone fit and returns whether it passed.
// The numeric analysis now lives in app/audio/audioqa; this only formats and
// applies the tool's pass thresholds.
func report(stage string, cfg config, metrics audioqa.Metrics, tone audioqa.Tone) bool {
	fmt.Fprintf(os.Stdout, "== %s ==\n", stage)

	if metrics.Empty {
		fmt.Fprintf(os.Stdout, "  no signal captured (region %d..%d of %d frames)\n", metrics.RegionFrom, metrics.RegionTo, metrics.Frames)

		return false
	}

	pass := metrics.NonFinite == 0 &&
		metrics.Clipped == 0 &&
		metrics.Dropouts == 0 &&
		tone.PeakResid <= cfg.tol &&
		tone.SNR >= cfg.minSNR &&
		math.Abs(tone.Gain-1) <= cfg.tol

	fmt.Fprintf(os.Stdout, "  region     : %d..%d (%d frames analysed)\n", metrics.RegionFrom, metrics.RegionTo, metrics.RegionTo-metrics.RegionFrom)
	fmt.Fprintf(os.Stdout, "  peak level : %.4f%s\n", metrics.Peak, marker(metrics.Clipped > 0, "  CLIPPING"))
	fmt.Fprintf(os.Stdout, "  non-finite : %d%s\n", metrics.NonFinite, marker(metrics.NonFinite > 0, "  NaN/Inf"))
	fmt.Fprintf(os.Stdout, "  dropouts   : %d (longest %d samples)%s\n", metrics.Dropouts, metrics.MaxDropout, marker(metrics.Dropouts > 0, "  UNDERRUN"))
	fmt.Fprintf(os.Stdout, "  max step   : %.5f (clean bound %.5f)%s\n",
		metrics.MaxStep,
		metrics.StepBound,
		marker(metrics.Glitches > 0,
			fmt.Sprintf("  %d glitches", metrics.Glitches),
		),
	)
	fmt.Fprintf(os.Stdout, "  gain       : %.4f (%.2f dB)%s\n", tone.Gain, 20*math.Log10(tone.Gain), marker(math.Abs(tone.Gain-1) > cfg.tol, "  LEVEL"))
	fmt.Fprintf(os.Stdout, "  residual   : peak %.5f, rms %.5f%s\n", tone.PeakResid, tone.RMSResid, marker(tone.PeakResid > cfg.tol, "  OVER TOL"))
	fmt.Fprintf(os.Stdout, "  SNR        : %.1f dB%s\n", tone.SNR, marker(tone.SNR < cfg.minSNR, "  LOW"))
	fmt.Fprintf(os.Stdout, "  result     : %s\n\n", verdict(pass))

	return pass
}

func marker(cond bool, msg string) string {
	if cond {
		return "  <- " + msg
	}

	return ""
}

func verdict(pass bool) string {
	if pass {
		return "clean"
	}

	return "ARTIFACTS"
}

// channel0 de-interleaves channel 0 to float64 for analysis. The sine is written
// identically to every channel, so channel 0 is representative.
func channel0(interleaved []float32, channels int) []float64 {
	out := make([]float64, len(interleaved)/channels)
	for i := range out {
		out[i] = float64(interleaved[i*channels])
	}

	return out
}

// ---------------------------------------------------------------------------
// Audible mode: drive a real backend for a listening check
// ---------------------------------------------------------------------------

func runAudible(cfg config) error {
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(zerolog.InfoLevel)

	backend, err := audio.New(cfg.backend, log)
	if err != nil {
		return fmt.Errorf("open backend %q: %w (rebuild with -tags portaudio?)", cfg.backend, err)
	}

	defer func() { _ = backend.Close() }()

	channels := cfg.channels
	if cfg.backend == audio.BackendBeep {
		channels = 2 // beep is fixed stereo
	}

	sink, err := backend.OpenSink(audio.SinkConfig{
		Channels:   channels,
		SampleRate: cfg.outRate,
		LatencyMs:  cfg.latencyMs,
	})
	if err != nil {
		return fmt.Errorf("open sink: %w", err)
	}

	defer func() { _ = sink.Stop() }()

	capacity, target, block := bufferFrames(cfg.outRate, cfg.latencyMs)

	src := audio.NewResamplingSource(
		&sineSource{freq: cfg.freq, amp: cfg.amp, rate: float64(cfg.inRate)},
		cfg.inRate, cfg.outRate, sink.Channels())
	async := audio.NewAsyncSource(src, sink.Channels(), capacity, target, block)

	defer async.Close()

	fmt.Fprintf(os.Stdout, "playing %.1f Hz tone through %s for %.1fs (listen for artifacts)...\n", cfg.freq, cfg.backend, cfg.dur)

	err = sink.Start(async)
	if err != nil {
		return fmt.Errorf("start sink: %w", err)
	}

	time.Sleep(time.Duration(cfg.dur*float64(time.Second)) + 200*time.Millisecond)

	return nil
}

// ---------------------------------------------------------------------------
// Latency mode: inject a synthetic gear-change marker through the real pipeline
// (resample -> async ring -> backend) and measure the delay from the event to
// the moment the device callback reads it.
// ---------------------------------------------------------------------------

// probeState carries the in-flight marker state shared across three goroutines: the
// injector (arms a marker and stamps the event time), the markerSource (the
// producer goroutine, which emits the impulse) and the latencyTap (the device
// callback, which detects it). All fields are atomic, so nothing locks on the
// realtime path.
type probeState struct {
	pending atomic.Bool  // a marker is armed and not yet detected
	eventNs atomic.Int64 // UnixNano when armed (the synthetic "gear change")
}

// markerSource emits silence at the internal rate and, when armed, writes a
// single full-scale impulse on the first frame of its next read — the synthetic
// gear-change onset, fed through the same resample/async path the live app uses.
type markerSource struct {
	pr   *probeState
	emit atomic.Bool // set by the injector, consumed once by ReadInterleaved
}

func (m *markerSource) ReadInterleaved(out []float32, channels int) (int, bool) {
	for i := range out {
		out[i] = 0
	}

	if m.emit.CompareAndSwap(true, false) {
		for c := 0; c < channels && c < len(out); c++ {
			out[c] = 1
		}
	}

	return len(out) / channels, true
}

// arm stamps the event time and schedules one impulse on the next read.
func (m *markerSource) arm() {
	m.pr.eventNs.Store(time.Now().UnixNano())
	m.pr.pending.Store(true)
	m.emit.Store(true)
}

// latencyTap is the source handed to the sink, so its ReadInterleaved runs on
// the device callback — the exact "moment portaudio reads". While a marker is
// pending it scans the outgoing buffer for the impulse and records the
// event->read delay. It is the only writer of results (the callback) and writes
// lock-free into a preallocated slice; results are read after Stop.
type latencyTap struct {
	inner     audio.SampleSource
	pr        *probeState
	threshold float32
	results   []time.Duration // preallocated; valid up to count
	count     atomic.Int64
}

func (t *latencyTap) ReadInterleaved(out []float32, channels int) (n int, ok bool) {
	n, ok = t.inner.ReadInterleaved(out, channels)

	if t.pr.pending.Load() {
		count := n * channels

		for i := range count {
			if out[i] >= t.threshold || out[i] <= -t.threshold {
				delay := time.Duration(time.Now().UnixNano() - t.pr.eventNs.Load())

				if idx := t.count.Add(1) - 1; int(idx) < len(t.results) {
					t.results[idx] = delay
				}

				t.pr.pending.Store(false)

				break
			}
		}
	}

	return n, ok
}

// runLatency drives the synthetic gear-change marker through the real output
// pipeline and reports the event->device-read delay distribution plus the ring's
// steady-state contribution. It needs a real backend (beep or portaudio): the
// capture backend's read timing is synthetic, so it cannot measure real latency.
func runLatency(cfg config) error {
	if cfg.backend != audio.BackendBeep && cfg.backend != audio.BackendPortAudio {
		return fmt.Errorf("latency mode needs a real backend: -backend beep or portaudio (got %q)", cfg.backend)
	}

	if cfg.interval <= 0 {
		return errors.New("-interval must be > 0")
	}

	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(zerolog.InfoLevel)

	backend, err := audio.New(cfg.backend, log)
	if err != nil {
		return fmt.Errorf("open backend %q: %w (rebuild with -tags portaudio?)", cfg.backend, err)
	}

	defer func() { _ = backend.Close() }()

	channels := cfg.channels
	if cfg.backend == audio.BackendBeep {
		channels = 2 // beep is fixed stereo
	}

	sink, err := backend.OpenSink(audio.SinkConfig{
		Channels:   channels,
		SampleRate: cfg.outRate,
		LatencyMs:  cfg.latencyMs,
	})
	if err != nil {
		return fmt.Errorf("open sink: %w", err)
	}

	defer func() { _ = sink.Stop() }()

	channels = sink.Channels()

	capacity, target, block := bufferFrames(cfg.outRate, cfg.latencyMs)

	probe := &probeState{}
	marker := &markerSource{pr: probe}
	source := audio.NewResamplingSource(marker, cfg.inRate, cfg.outRate, channels)
	async := audio.NewAsyncSource(source, channels, capacity, target, block)

	defer async.Close()

	tap := &latencyTap{
		inner:     async,
		pr:        probe,
		threshold: float32(cfg.threshold),
		results:   make([]time.Duration, int(cfg.dur/cfg.interval)+8),
	}

	fmt.Fprintf(os.Stdout, "backend : %s\n", cfg.backend)
	fmt.Fprintf(os.Stdout, "rates   : internal %d Hz -> output %d Hz, %d ch\n", cfg.inRate, cfg.outRate, channels)
	fmt.Fprintf(os.Stdout, "buffer  : ring %d frames (%.1f ms cap), target %d frames (%.1f ms), block %d frames, latency hint %d ms\n",
		capacity, framesToMs(capacity, cfg.outRate), target, framesToMs(target, cfg.outRate), block, cfg.latencyMs)
	fmt.Fprintf(os.Stdout, "probe   : impulse every %.0f ms, detect |x|>=%.2f, for %.1fs\n\n",
		cfg.interval*1000, cfg.threshold, cfg.dur)

	err = sink.Start(tap)
	if err != nil {
		return fmt.Errorf("start sink: %w", err)
	}

	misses := injectMarkers(cfg, probe, marker)

	// Let the last in-flight marker drain through the ring and device.
	time.Sleep(300 * time.Millisecond)

	health := async.Health()
	_ = sink.Stop()

	n := min(int(tap.count.Load()), len(tap.results))
	reportLatency(cfg, channels, tap.results[:n], misses, capacity, health)

	return nil
}

// injectMarkers arms a marker every interval for the run duration, skipping ticks
// while a previous marker is still in flight and giving up on one that is never
// detected within a second (counted as a miss).
func injectMarkers(c config, probe *probeState, marker *markerSource) int {
	stop := time.After(time.Duration(c.dur * float64(time.Second)))
	ticker := time.NewTicker(time.Duration(c.interval * float64(time.Second)))

	defer ticker.Stop()

	misses := 0

	for {
		select {
		case <-stop:
			return misses
		case <-ticker.C:
			if probe.pending.Load() {
				if time.Since(time.Unix(0, probe.eventNs.Load())) < time.Second {
					continue // still waiting for the in-flight marker
				}

				probe.pending.Store(false)

				misses++
			}

			marker.arm()
		}
	}
}

// framesToMs converts a frame count at rate Hz to milliseconds.
func framesToMs(frames, rate int) float64 {
	return float64(frames) / float64(rate) * 1000
}

// reportLatency prints the event->read delay distribution and the ring's
// steady-state contribution.
func reportLatency(cfg config, channels int, results []time.Duration, misses, capacity int, health audio.HealthMetrics) {
	fmt.Fprintf(os.Stdout, "== results ==\n")

	if len(results) == 0 {
		fmt.Fprintf(os.Stdout, "  no markers detected (%d missed) — raise -dur, lower -threshold,\n", misses)
		fmt.Fprintf(os.Stdout, "  or check the device is actually playing\n")

		return
	}

	sorted := append([]time.Duration(nil), results...)
	slices.Sort(sorted)

	millisec := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	pct := func(f float64) time.Duration { return sorted[int(f*float64(len(sorted)-1)+0.5)] }

	usedFrames := health.RingUsed / max(channels, 1)

	fmt.Fprintf(os.Stdout, "  detected : %d markers (%d missed)\n", len(sorted), misses)
	fmt.Fprintf(os.Stdout, "  event -> portaudio read:\n")
	fmt.Fprintf(os.Stdout, "    min    %6.1f ms\n", millisec(sorted[0]))
	fmt.Fprintf(os.Stdout, "    median %6.1f ms\n", millisec(pct(0.5)))
	fmt.Fprintf(os.Stdout, "    p95    %6.1f ms\n", millisec(pct(0.95)))
	fmt.Fprintf(os.Stdout, "    max    %6.1f ms\n", millisec(sorted[len(sorted)-1]))
	fmt.Fprintf(os.Stdout, "  ring     : capacity %.1f ms, steady-state fill %.1f ms (%.0f%%)\n",
		framesToMs(capacity, cfg.outRate), framesToMs(usedFrames, cfg.outRate), health.FillRatio*100)
	fmt.Fprintf(os.Stdout, "  underruns: %d (%d samples)\n", health.Underruns, health.UnderrunSamples)
	fmt.Fprintf(os.Stdout, "  note     : add the device's negotiated OutputLatency (logged above by\n")
	fmt.Fprintf(os.Stdout, "             the backend) for the full input->DAC delay.\n")
}

// ---------------------------------------------------------------------------
// WAV dump (16-bit PCM) for offline inspection
// ---------------------------------------------------------------------------

func writeWAV(path string, interleaved []float32, channels, rate int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer func() { _ = file.Close() }()

	const bitsPerSample = 16

	dataLen := len(interleaved) * 2
	byteRate := rate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	header := make([]byte, 0, 44)
	header = append(header, "RIFF"...)
	header = binary.LittleEndian.AppendUint32(header, uint32(36+dataLen))
	header = append(header, "WAVE"...)
	header = append(header, "fmt "...)
	header = binary.LittleEndian.AppendUint32(header, 16)
	header = binary.LittleEndian.AppendUint16(header, 1) // PCM
	header = binary.LittleEndian.AppendUint16(header, uint16(channels))
	header = binary.LittleEndian.AppendUint32(header, uint32(rate))
	header = binary.LittleEndian.AppendUint32(header, uint32(byteRate))
	header = binary.LittleEndian.AppendUint16(header, uint16(blockAlign))
	header = binary.LittleEndian.AppendUint16(header, bitsPerSample)
	header = append(header, "data"...)
	header = binary.LittleEndian.AppendUint32(header, uint32(dataLen))

	_, err = file.Write(header)
	if err != nil {
		return err
	}

	pcm := make([]byte, dataLen)

	for i, v := range interleaved {
		s := math.Round(float64(v) * math.MaxInt16)
		s = math.Max(math.MinInt16, math.Min(math.MaxInt16, s))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(s)))
	}

	_, err = file.Write(pcm)

	return err
}
