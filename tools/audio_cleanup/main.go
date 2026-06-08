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
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

func main() {
	cfg := parseFlags()

	switch cfg.backend {
	case "capture":
		if err := runCapture(cfg); err != nil {
			fail(err)
		}
	case audio.BackendBeep, audio.BackendPortAudio:
		if err := runAudible(cfg); err != nil {
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
}

func parseFlags() config {
	var c config

	flag.IntVar(&c.inRate, "in", 8000, "internal (synthesizer) sample rate in Hz")
	flag.IntVar(&c.outRate, "out", 32000, "device output sample rate in Hz")
	flag.IntVar(&c.channels, "channels", 2, "output channel count")
	flag.Float64Var(&c.freq, "freq", audio.DefaultTestToneHz, "test tone frequency in Hz")
	flag.Float64Var(&c.amp, "amp", 0.5, "test tone amplitude (0..1)")
	flag.Float64Var(&c.dur, "dur", 2.0, "seconds of signal to analyse/play")
	flag.IntVar(&c.latencyMs, "latency", 50, "requested device latency in ms")
	flag.IntVar(&c.devBuf, "devbuf", 0, "device callback buffer in frames (0 = derive from latency)")
	flag.BoolVar(&c.realtime, "realtime", true, "pace capture pulls at wall-clock rate (reveals real underruns)")
	flag.Float64Var(&c.tol, "tol", 0.02, "max acceptable peak residual vs fitted fundamental")
	flag.Float64Var(&c.minSNR, "snr", 50, "min acceptable fundamental-to-residual SNR in dB")
	flag.StringVar(&c.backend, "backend", "capture", "capture (analyse) | beep | portaudio (audible)")
	flag.StringVar(&c.stage, "stage", "all", "all | control | resample | async | full")
	flag.StringVar(&c.wav, "wav", "", "when set, write <wav>-<stage>.wav for each captured stage")

	flag.Parse()

	return c
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

	for f := range frames {
		v := float32(s.amp * math.Sin(s.phase))

		s.phase += inc
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}

		for c := range channels {
			out[f*channels+c] = v
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

func (captureBackend) OpenSink(cfg audio.SinkConfig) (audio.Sink, error) { //nolint:ireturn // implements audio.Backend
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

		n := frames
		if captured+n > s.totalFrames {
			n = s.totalFrames - captured
		}

		s.rec = append(s.rec, buf[:n*s.channels]...)
		captured += n

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

func runCapture(c config) error {
	stages := stageList(c.stage)
	if stages == nil {
		return fmt.Errorf("unknown -stage %q", c.stage)
	}

	// The resample/full stages generate the tone at the internal rate before
	// up-sampling, so a tone at or above the internal Nyquist cannot be represented
	// and will alias — a test-input error that would masquerade as a resampler bug.
	if usesInternalRate(stages) && c.freq >= float64(c.inRate)/2 {
		return fmt.Errorf("freq %.0f Hz is at/above the internal Nyquist %.0f Hz (inRate %d); "+
			"lower -freq, raise -in, or test only -stage control/async",
			c.freq, float64(c.inRate)/2, c.inRate)
	}

	capacity, block := bufferFrames(c.outRate, c.latencyMs)

	devBuf := c.devBuf
	if devBuf <= 0 {
		devBuf = block
	}

	fmt.Printf("input : %.1f Hz sine, amp %.3f, %d ch\n", c.freq, c.amp, c.channels)
	fmt.Printf("rates : internal %d Hz -> output %d Hz (ratio %.3gx)\n", c.inRate, c.outRate, float64(c.outRate)/float64(c.inRate))
	fmt.Printf("buffer: ring capacity %d frames, producer block %d frames, device buffer %d frames\n",
		capacity, block, devBuf)
	fmt.Printf("expect: peak residual <= %.4f, SNR >= %.0f dB\n\n", c.tol, c.minSNR)

	var failures int

	for _, st := range stages {
		rec, err := captureStage(c, st, capacity, block, devBuf)
		if err != nil {
			return fmt.Errorf("stage %s: %w", st, err)
		}

		if c.wav != "" {
			path := fmt.Sprintf("%s-%s.wav", c.wav, st)
			if err := writeWAV(path, rec, c.channels, c.outRate); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		}

		m := analyse(channel0(rec, c.channels), c.outRate, c.freq, c.amp)
		ok := m.report(st, c)

		if !ok {
			failures++
		}
	}

	fmt.Println()

	if failures == 0 {
		fmt.Println("PASS: all stages within tolerance")

		return nil
	}

	fmt.Printf("FAIL: %d stage(s) introduced artifacts above tolerance\n", failures)
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
func captureStage(c config, stage string, capacity, block, devBuf int) ([]float32, error) {
	resampling := stage == "resample" || stage == "full"

	srcRate := c.outRate
	if resampling {
		srcRate = c.inRate
	}

	var src audio.SampleSource = &sineSource{freq: c.freq, amp: c.amp, rate: float64(srcRate)}

	if resampling {
		src = audio.NewResamplingSource(src, c.inRate, c.outRate, c.channels)
	}

	var closeAsync func()

	if stage == "async" || stage == "full" {
		async := audio.NewAsyncSource(src, c.channels, capacity, block)
		src = async
		closeAsync = async.Close
	}

	// Drive the capture through the audio.Backend abstraction, exactly as the app
	// drives beep/portaudio: open a sink for the requested format, then Start it on
	// the source chain. The capture-specific knobs (pacing, how long to record) are
	// set on the concrete sink after opening.
	sinkIface, err := captureBackend{}.OpenSink(audio.SinkConfig{Channels: c.channels, SampleRate: c.outRate})
	if err != nil {
		return nil, err
	}

	sink := sinkIface.(*captureSink) //nolint:forcetypeassert // captureBackend only ever returns *captureSink
	sink.blockFrames = devBuf
	sink.realtime = c.realtime
	// Capture the requested duration plus enough lead to cover the async ring's
	// pre-filled silence; analyse() trims the silent head and tail.
	sink.totalFrames = int(c.dur*float64(c.outRate)) + 2*capacity

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
func bufferFrames(outputRate, latencyMs int) (capacity, block int) {
	if latencyMs <= 0 {
		latencyMs = 50
	}

	periodFrames := outputRate * latencyMs / 1000
	block = max(outputRate/100, 256)
	capacity = max(periodFrames*2, block*4)

	return capacity, block
}

// ---------------------------------------------------------------------------
// Analysis
// ---------------------------------------------------------------------------

type metrics struct {
	frames     int
	regionFrom int
	regionTo   int
	peak       float64
	clipped    int
	nonFinite  int
	dropouts   int     // count of interior zero-runs (underruns)
	maxDropout int     // longest zero-run in samples
	maxStep    float64 // largest |x[i]-x[i-1]| in the region
	stepBound  float64 // theoretical max step for a clean tone
	glitches   int     // steps exceeding 3x the bound
	gain       float64 // recovered fundamental amplitude / input amplitude
	rmsResid   float64 // RMS of (signal - fitted fundamental)
	peakResid  float64 // peak of the residual
	snr        float64 // 20*log10(fundamental / (sqrt2 * rmsResid))
	empty      bool
}

// analyse measures a single channel of captured output against the known tone.
func analyse(x []float64, rate int, freq, inAmp float64) metrics {
	m := metrics{frames: len(x), stepBound: inAmp * 2 * math.Pi * freq / float64(rate)}

	from, to := signalRegion(x, inAmp)

	// Trim a short guard band (~10 ms) inside each edge so the onset/offset ramps —
	// where the tone fades up from the ring's pre-filled silence — do not inflate
	// the peak residual or step measurements. Those ramps are real but transient;
	// the steady-state body is what reveals whether the pipeline distorts the tone.
	guard := rate / 100
	if from+guard < to-guard {
		from += guard
		to -= guard
	}

	m.regionFrom, m.regionTo = from, to

	if to-from < rate/int(math.Max(freq, 1)) {
		m.empty = true

		return m
	}

	region := x[from:to]

	// Hard checks over the active signal region.
	for i, v := range region {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			m.nonFinite++

			continue
		}

		if a := math.Abs(v); a > m.peak {
			m.peak = a
		}

		if math.Abs(v) >= 0.999 {
			m.clipped++
		}

		if i > 0 {
			if step := math.Abs(v - region[i-1]); step > m.maxStep {
				m.maxStep = step
			}
		}

		if i > 0 && math.Abs(region[i-1]-region[i]) > 3*m.stepBound {
			m.glitches++
		}
	}

	m.dropouts, m.maxDropout = zeroRuns(region)

	// Best-fit fundamental: project the region onto cos/sin at freq. The phase is
	// absorbed by the two coefficients, so no alignment is needed. Whatever does
	// not fit the single expected sinusoid is distortion + noise the pipeline added.
	w := 2 * math.Pi * freq / float64(rate)
	n := float64(len(region))

	var ac, as float64

	for i, v := range region {
		ac += v * math.Cos(w*float64(i))
		as += v * math.Sin(w*float64(i))
	}

	ac *= 2 / n
	as *= 2 / n
	fundAmp := math.Hypot(ac, as)
	m.gain = fundAmp / inAmp

	var sumSq float64

	for i, v := range region {
		fit := ac*math.Cos(w*float64(i)) + as*math.Sin(w*float64(i))
		r := v - fit

		sumSq += r * r
		if a := math.Abs(r); a > m.peakResid {
			m.peakResid = a
		}
	}

	m.rmsResid = math.Sqrt(sumSq / n)
	if m.rmsResid > 0 {
		m.snr = 20 * math.Log10(fundAmp/(math.Sqrt2*m.rmsResid))
	} else {
		m.snr = math.Inf(1)
	}

	return m
}

// signalRegion returns the [from,to) frame range carrying the tone, trimming the
// async ring's silent lead and any silent tail by locating the first and last
// samples that reach a quarter of the input amplitude (a sine crosses this every
// cycle, so the bound is hit promptly once the tone is flowing).
func signalRegion(x []float64, inAmp float64) (from, to int) {
	thresh := 0.25 * inAmp

	from = 0
	for from < len(x) && math.Abs(x[from]) < thresh {
		from++
	}

	to = len(x)
	for to > from && math.Abs(x[to-1]) < thresh {
		to--
	}

	return from, to
}

// zeroRuns counts interior runs of exact zeros longer than two samples — the
// signature of an async ring underrun, which zero-pads the device callback.
func zeroRuns(x []float64) (count, longest int) {
	run := 0

	flush := func() {
		if run > 2 {
			count++
			if run > longest {
				longest = run
			}
		}

		run = 0
	}

	for _, v := range x {
		if v == 0 {
			run++
		} else {
			flush()
		}
	}

	flush()

	return count, longest
}

// report prints the metrics for one stage and returns whether it passed.
func (m metrics) report(stage string, c config) bool {
	fmt.Printf("== %s ==\n", stage)

	if m.empty {
		fmt.Printf("  no signal captured (region %d..%d of %d frames)\n", m.regionFrom, m.regionTo, m.frames)

		return false
	}

	pass := m.nonFinite == 0 &&
		m.clipped == 0 &&
		m.dropouts == 0 &&
		m.peakResid <= c.tol &&
		m.snr >= c.minSNR &&
		math.Abs(m.gain-1) <= c.tol

	fmt.Printf("  region     : %d..%d (%d frames analysed)\n", m.regionFrom, m.regionTo, m.regionTo-m.regionFrom)
	fmt.Printf("  peak level : %.4f%s\n", m.peak, marker(m.clipped > 0, "  CLIPPING"))
	fmt.Printf("  non-finite : %d%s\n", m.nonFinite, marker(m.nonFinite > 0, "  NaN/Inf"))
	fmt.Printf("  dropouts   : %d (longest %d samples)%s\n", m.dropouts, m.maxDropout, marker(m.dropouts > 0, "  UNDERRUN"))
	fmt.Printf("  max step   : %.5f (clean bound %.5f)%s\n", m.maxStep, m.stepBound, marker(m.glitches > 0, fmt.Sprintf("  %d glitches", m.glitches)))
	fmt.Printf("  gain       : %.4f (%.2f dB)%s\n", m.gain, 20*math.Log10(m.gain), marker(math.Abs(m.gain-1) > c.tol, "  LEVEL"))
	fmt.Printf("  residual   : peak %.5f, rms %.5f%s\n", m.peakResid, m.rmsResid, marker(m.peakResid > c.tol, "  OVER TOL"))
	fmt.Printf("  SNR        : %.1f dB%s\n", m.snr, marker(m.snr < c.minSNR, "  LOW"))
	fmt.Printf("  result     : %s\n\n", verdict(pass))

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

func runAudible(c config) error {
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).Level(zerolog.InfoLevel)

	backend, err := audio.New(c.backend, log)
	if err != nil {
		return fmt.Errorf("open backend %q: %w (rebuild with -tags portaudio?)", c.backend, err)
	}

	defer func() { _ = backend.Close() }()

	channels := c.channels
	if c.backend == audio.BackendBeep {
		channels = 2 // beep is fixed stereo
	}

	sink, err := backend.OpenSink(audio.SinkConfig{
		Channels:   channels,
		SampleRate: c.outRate,
		LatencyMs:  c.latencyMs,
	})
	if err != nil {
		return fmt.Errorf("open sink: %w", err)
	}

	defer func() { _ = sink.Stop() }()

	capacity, block := bufferFrames(c.outRate, c.latencyMs)

	src := audio.NewResamplingSource(
		&sineSource{freq: c.freq, amp: c.amp, rate: float64(c.inRate)},
		c.inRate, c.outRate, sink.Channels())
	async := audio.NewAsyncSource(src, sink.Channels(), capacity, block)

	defer async.Close()

	fmt.Printf("playing %.1f Hz tone through %s for %.1fs (listen for artifacts)...\n", c.freq, c.backend, c.dur)

	if err := sink.Start(async); err != nil {
		return fmt.Errorf("start sink: %w", err)
	}

	time.Sleep(time.Duration(c.dur*float64(time.Second)) + 200*time.Millisecond)

	return nil
}

// ---------------------------------------------------------------------------
// WAV dump (16-bit PCM) for offline inspection
// ---------------------------------------------------------------------------

func writeWAV(path string, interleaved []float32, channels, rate int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	const bitsPerSample = 16

	dataLen := len(interleaved) * 2
	byteRate := rate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	h := make([]byte, 0, 44)
	h = append(h, "RIFF"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(36+dataLen))
	h = append(h, "WAVE"...)
	h = append(h, "fmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1) // PCM
	h = binary.LittleEndian.AppendUint16(h, uint16(channels))
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(byteRate))
	h = binary.LittleEndian.AppendUint16(h, uint16(blockAlign))
	h = binary.LittleEndian.AppendUint16(h, bitsPerSample)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(dataLen))

	if _, err := f.Write(h); err != nil {
		return err
	}

	pcm := make([]byte, dataLen)
	for i, v := range interleaved {
		s := math.Round(float64(v) * math.MaxInt16)
		s = math.Max(math.MinInt16, math.Min(math.MaxInt16, s))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(s)))
	}

	_, err = f.Write(pcm)

	return err
}
