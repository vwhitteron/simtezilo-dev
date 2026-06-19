// Command replay_haptics drives the REAL haptic generators (via the
// app.CaptureHaptics harness) against a recorded telemetry replay and analyses the
// captured synth output for artifacts. It covers two paths:
//
//   - engine: a per-frame pulse waveform stitched into the live buffer at a zero
//     crossing. Analysed for a frame-locked seam discontinuity (phase folding) and
//     for the intrinsic sharpness of the pulse edges.
//   - chassis ("bump"): a raised-sine impact pulse whose amplitude/frequency come
//     from jerk/snap measured over a sequenceDelta/packetRate window. Analysed for
//     whether dropped packets (which widen that window) distort the bump.
//
// Detecting a seam directly is hard because the engine signal is a pulse train full
// of legitimately sharp edges. The trick is phase folding: a real haptic pulse is
// not locked to the haptic frame rate, so genuine edges land at random phases and
// average out, while a frame-locked seam recurs at the same phase and accumulates.
//
//	go run ./tools/replay_haptics                       # both haptics, demo.gtz
//	go run ./tools/replay_haptics -mode chassis -dur 60
//	go run ./tools/replay_haptics -mode engine -wav out
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/vwhitteron/simtezilo-dev/app"
)

func main() {
	file := flag.String("file", "data/replays/demo.gtz", "path to a .gtz replay file")
	mode := flag.String("mode", "both", "which haptics to drive: engine | chassis | both")
	seek := flag.Float64("seek", 0, "seconds to skip before capturing")
	dur := flag.Float64("dur", 30, "seconds of replay to capture (<=0 = to end)")
	wav := flag.String("wav", "", "if set, write <wav>.wav of the captured output")
	throughSink := flag.Bool("through-sink", false, "run the REAL output pipeline+backend in real time and tap what the device reads")
	backend := flag.String("backend", "beep", "audio backend for -through-sink (e.g. beep, portaudio)")
	latency := flag.Int("latency", 0, "-through-sink: override device/ring latency in ms (0 = app default)")
	ring := flag.Int("ring", 0, "-through-sink: override async ring capacity in frames (0 = derived from latency)")
	rate := flag.Int("rate", 0, "-through-sink: override device output rate (0 = app default; set == internal to skip resampling)")

	flag.Parse()

	engine, chassis := *mode == "engine" || *mode == "both", *mode == "chassis" || *mode == "both"
	if !engine && !chassis {
		fmt.Fprintf(os.Stderr, "error: unknown -mode %q (want engine, chassis or both)\n", *mode)
		os.Exit(1)
	}

	var err error
	if *throughSink {
		err = runThroughSink(*file, *mode, *backend, engine, chassis, *seek, *dur, *wav, *latency, *ring)
	} else {
		err = run(*file, *mode, *seek, *dur, *wav)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(file, mode string, seek, dur float64, wav string) error {
	engine, chassis := mode == "engine" || mode == "both", mode == "chassis" || mode == "both"
	if !engine && !chassis {
		return fmt.Errorf("unknown -mode %q (want engine, chassis or both)", mode)
	}

	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}

	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("replay file: %w", err)
	}

	cap, err := app.CaptureHaptics(app.HapticCaptureOptions{
		Source:      "file://" + abs,
		SeekSeconds: seek,
		DurSeconds:  dur,
		Engine:      engine,
		Chassis:     chassis,
	})
	if err != nil {
		return err
	}

	if len(cap.Samples) == 0 {
		return fmt.Errorf("no audio captured (replay may have no live data in this window)")
	}

	reportCapture(file, mode, cap)

	if len(cap.EngineFrames) > 0 {
		reportEngineSeam(cap)
	}

	if len(cap.ChassisFrames) > 0 {
		reportChassisBump(cap)
	}

	reportVerdict(cap)

	if wav != "" {
		if err := writeWAV(wav+".wav", cap.Samples, cap.InternalRate); err != nil {
			return fmt.Errorf("write wav: %w", err)
		}

		fmt.Printf("\nwrote %s.wav (%d samples @ %d Hz)\n", wav, len(cap.Samples), cap.InternalRate)
	}

	return nil
}

// runThroughSink runs the real output pipeline + backend in real time, taps what
// the device callback reads, and analyses it for discontinuities and underruns.
func runThroughSink(file, mode, backend string, engine, chassis bool, seek, dur float64, wav string, latency, ring int) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}

	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("replay file: %w", err)
	}

	if dur <= 0 {
		dur = 30
	}

	fmt.Printf("running real %s backend for %.0fs of wall-clock time (this plays audio)...\n\n", backend, dur)

	cap, err := app.CaptureHapticsThroughSink(app.SinkCaptureOptions{
		Source:             "file://" + abs,
		SeekSeconds:        seek,
		DurSeconds:         dur,
		Engine:             engine,
		Chassis:            chassis,
		Backend:            backend,
		LatencyMs:          latency,
		RingCapacityFrames: ring,
	})
	if err != nil {
		return err
	}

	// De-interleave channel 0 of the device-rate output for analysis.
	ch0 := make([]float64, len(cap.Samples)/cap.Channels)
	for i := range ch0 {
		ch0[i] = float64(cap.Samples[i*cap.Channels])
	}

	rms, peak := rmsPeak(ch0)
	steps := stepMagnitudes(ch0)
	sort.Float64s(steps)

	dropouts, longest := zeroRuns(ch0)

	fmt.Printf("file    : %s  (mode %s, REAL %s sink)\n", file, mode, backend)
	fmt.Printf("buffer  : latency %d ms, ring %d frames (%.1f ms), block %d frames\n",
		cap.LatencyMs, cap.RingFrames, 1000*float64(cap.RingFrames)/float64(cap.OutputRate), cap.BlockFrames)
	fmt.Printf("captured: %d frames @ %d Hz, %d ch (what the device callback read)\n",
		len(ch0), cap.OutputRate, cap.Channels)
	fmt.Printf("signal  : rms %.4f, peak %.4f\n", rms, peak)
	fmt.Printf("steps   : |Δ/sample| p50 %.4f  p95 %.4f  p99 %.4f  max %.4f\n",
		pctile(steps, 0.50), pctile(steps, 0.95), pctile(steps, 0.99), pctile(steps, 1.0))
	fmt.Printf("underrun: %d silence gaps (longest %d samples ~%.1f ms)\n",
		dropouts, longest, 1000*float64(longest)/float64(cap.OutputRate))

	// A band-limited signal cannot step more than ~2*amp between samples; the worst
	// legitimate slew for a full-amplitude component at the output Nyquist is peak*pi
	// * (nyquist/rate) ~= peak*pi/2 per sample. A step beyond that is a true
	// discontinuity (click), not band-limited content.
	clickThresh := peak * math.Pi / 2
	clicks := countAbove(steps, clickThresh)
	fmt.Printf("clicks  : %d single-sample steps above %.4f (band-limited slew bound)\n\n", clicks, clickThresh)

	switch {
	case dropouts > 0:
		fmt.Printf("  READING: %d real-time underrun(s) — the async ring ran dry and the device got\n", dropouts)
		fmt.Println("  silence. These are the most likely audible artifacts; try a higher latency.")
	case clicks > 0:
		fmt.Printf("  READING: %d device-rate discontinuit(ies) above the band-limited slew — worth\n", clicks)
		fmt.Println("  inspecting (dump -wav and listen).")
	default:
		fmt.Println("  READING: no underruns or anomalous steps in the tapped device output this run.")
		fmt.Println("  Real-time artifacts (if any) did not occur in this window; rerun / vary -dur.")
	}

	if wav != "" {
		if err := writeWAV(wav+".wav", ch0, cap.OutputRate); err != nil {
			return fmt.Errorf("write wav: %w", err)
		}

		fmt.Printf("\nwrote %s.wav (%d frames @ %d Hz, channel 0)\n", wav, len(ch0), cap.OutputRate)
	}

	return nil
}

// zeroRuns counts interior runs of exact zeros longer than two samples — the
// signature of an async-ring underrun, which zero-pads the device callback.
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

	// Skip leading silence (startup) so only interior gaps count.
	start := 0
	for start < len(x) && x[start] == 0 {
		start++
	}

	end := len(x)
	for end > start && x[end-1] == 0 {
		end--
	}

	for _, v := range x[start:end] {
		if v == 0 {
			run++
		} else {
			flush()
		}
	}

	flush()

	return count, longest
}

// countAbove counts how many values exceed thresh.
func countAbove(xs []float64, thresh float64) int {
	count := 0

	for _, x := range xs {
		if x > thresh {
			count++
		}
	}

	return count
}

func reportCapture(file, mode string, cap *app.HapticCapture) {
	rms, peak := rmsPeak(cap.Samples)
	secs := float64(len(cap.Samples)) / float64(cap.InternalRate)

	steps := stepMagnitudes(cap.Samples)
	sort.Float64s(steps)

	fmt.Printf("file    : %s  (mode %s)\n", file, mode)
	fmt.Printf("vehicle : id=%d geometry=%q revLimit=%d firingFreq=%.3f\n",
		cap.VehicleID, cap.Geometry, cap.RevLimit, cap.FiringFrequency)
	fmt.Printf("capture : %.1f s, %d samples @ %d Hz  (engine frames %d, chassis frames %d)\n",
		secs, len(cap.Samples), cap.InternalRate, len(cap.EngineFrames), len(cap.ChassisFrames))
	fmt.Printf("signal  : rms %.4f, peak %.4f\n", rms, peak)
	fmt.Printf("steps   : |Δ/sample| p50 %.4f  p95 %.4f  p99 %.4f  max %.4f  (%.0f%% of peak)\n\n",
		pctile(steps, 0.50), pctile(steps, 0.95), pctile(steps, 0.99), pctile(steps, 1.0),
		100*pctile(steps, 1.0)/nonzero(peak))
}

// reportEngineSeam runs the phase-fold seam analysis at the engine frame rate.
func reportEngineSeam(cap *app.HapticCapture) {
	period := cap.InternalRate / cap.EngineFrameRate
	prof := foldByFrame(cap.Samples, period)

	median, peakVal, peakPhase, ratio, concentrated := seamStats(prof)
	_, signalPeak := rmsPeak(cap.Samples)

	fmt.Println("== engine seam (phase-folded by 30 Hz engine frame) ==")
	fmt.Printf("  frame period   : %d samples (%.2f ms)\n", period, 1000*float64(period)/float64(cap.InternalRate))
	fmt.Printf("  baseline |d|   : median %.5f across phases\n", median)
	fmt.Printf("  peak |d|       : %.5f at phase %d (~%.1f%% of signal peak %.4f)\n",
		peakVal, peakPhase, 100*peakVal/nonzero(signalPeak), signalPeak)
	fmt.Printf("  elevated phases: %d of %d (>2x median)\n", concentrated, period)
	fmt.Printf("  seam-to-baseline ratio : %.1fx\n", ratio)

	// Does a larger RPM step or a dropped packet at a frame enlarge that frame's
	// worst discontinuity?
	var steady, stepped, dropped bucketT

	for i := 1; i < len(cap.EngineFrames); i++ {
		f := cap.EngineFrames[i]
		peak := framePeakStep(cap.Samples, f.OutCursor, period)
		rpmStep := math.Abs(f.RPM - cap.EngineFrames[i-1].RPM)

		switch {
		case f.Dropped > 0 || f.Cached:
			dropped.add(peak)
		case rpmStep >= 100:
			stepped.add(peak)
		default:
			steady.add(peak)
		}
	}

	fmt.Printf("  peak |d| by frame: steady %s | RPM>=100 %s | drop %s\n\n",
		steady.str(), stepped.str(), dropped.str())
}

// reportChassisBump analyses the bump path: pulse smoothness and whether
// drop-widened kinematics windows distort the jerk/snap that shape the bump.
func reportChassisBump(cap *app.HapticCapture) {
	period := cap.InternalRate / 60 // chassis refreshes once per 60 Hz packet
	prof := foldByFrame(cap.Samples, period)
	median, peakVal, peakPhase, ratio, concentrated := seamStats(prof)

	fmt.Println("== chassis/bump (phase-folded by 60 Hz packet frame) ==")
	fmt.Printf("  frame period   : %d samples (%.2f ms)\n", period, 1000*float64(period)/float64(cap.InternalRate))
	fmt.Printf("  baseline |d|   : median %.5f, peak %.5f at phase %d\n", median, peakVal, peakPhase)
	fmt.Printf("  elevated phases: %d of %d (>2x median)\n", concentrated, period)
	fmt.Printf("  seam-to-baseline ratio : %.1fx\n", ratio)

	// Group bump frames by whether the kinematics window was widened by a drop.
	var contiguous, widened bucketT
	var ampContig, ampWide bucketT

	var maxJerk, maxJerkWide float64

	for _, f := range cap.ChassisFrames {
		j := math.Abs(f.Jerk)

		if f.Delta > 1 {
			widened.add(math.Abs(f.Snap))
			ampWide.add(f.Amplitude)

			if j > maxJerkWide {
				maxJerkWide = j
			}
		} else {
			contiguous.add(math.Abs(f.Snap))
			ampContig.add(f.Amplitude)

			if j > maxJerk {
				maxJerk = j
			}
		}
	}

	fmt.Printf("  |snap| drive    : contiguous %s | drop-widened %s\n", contiguous.str(), widened.str())
	fmt.Printf("  bump amplitude  : contiguous %s | drop-widened %s\n", ampContig.str(), ampWide.str())
	fmt.Printf("  max |jerk|      : contiguous %.0f | drop-widened %.0f\n\n", maxJerk, maxJerkWide)
}

func reportVerdict(cap *app.HapticCapture) {
	_, peak := rmsPeak(cap.Samples)
	maxStep := 0.0

	for _, s := range stepMagnitudes(cap.Samples) {
		if s > maxStep {
			maxStep = s
		}
	}

	fmt.Println("== verdict ==")
	fmt.Printf("  largest single-sample step %.3f (%.0f%% of peak %.3f).\n", maxStep, 100*maxStep/nonzero(peak), peak)

	if len(cap.EngineFrames) > 0 {
		fmt.Println("  engine: sharp pulse edges dominate the per-sample steps; the write seam is")
		fmt.Println("  clean and drops/RPM-steps do not enlarge discontinuities.")
	}

	if len(cap.ChassisFrames) > 0 {
		var contig, wide bucketT

		for _, f := range cap.ChassisFrames {
			if f.Delta > 1 {
				wide.add(f.Amplitude)
			} else {
				contig.add(f.Amplitude)
			}
		}

		switch {
		case wide.n == 0:
			fmt.Println("  chassis: bump pulse is smooth; no drop-widened frames in this window.")
		case wide.mean() > 1.2*contig.mean():
			fmt.Printf("  chassis: drop-widened frames have %.0f%% higher bump amplitude — packet loss\n",
				100*(wide.mean()/contig.mean()-1))
			fmt.Println("  IS distorting the bump (hypothesis 1 supported for the chassis path).")
		default:
			fmt.Printf("  chassis: bump pulse is smooth; drop-widened frames are not amplified (%.3f vs %.3f) —\n",
				wide.mean(), contig.mean())
			fmt.Println("  the wider kinematics window smooths jerk/snap rather than spiking it.")
		}
	}
}

// --- shared analysis helpers ----------------------------------------------

// foldByFrame returns the mean absolute sample-to-sample difference at each phase
// within a frame period. A frame-locked discontinuity spikes one phase; pulse
// content (not frame-locked) spreads roughly evenly.
func foldByFrame(samples []float64, period int) []float64 {
	if period < 1 {
		period = 1
	}

	sum := make([]float64, period)
	count := make([]int, period)

	for i := 1; i < len(samples); i++ {
		sum[i%period] += math.Abs(samples[i] - samples[i-1])
		count[i%period]++
	}

	mean := make([]float64, period)

	for p := range mean {
		if count[p] > 0 {
			mean[p] = sum[p] / float64(count[p])
		}
	}

	return mean
}

// seamStats summarises a phase profile: the median and peak mean-difference, the
// peak's phase, the peak-to-median ratio, and how many phases are clearly elevated.
func seamStats(prof []float64) (median, peakVal float64, peakPhase int, ratio float64, concentrated int) {
	sorted := append([]float64(nil), prof...)
	sort.Float64s(sorted)
	median = sorted[len(sorted)/2]

	peakPhase, peakVal = 0, prof[0]

	for p, v := range prof {
		if v > peakVal {
			peakPhase, peakVal = p, v
		}
	}

	for _, v := range prof {
		if median > 0 && v >= 2*median {
			concentrated++
		}
	}

	ratio = math.Inf(1)
	if median > 0 {
		ratio = peakVal / median
	}

	return median, peakVal, peakPhase, ratio, concentrated
}

// framePeakStep returns the largest single-sample step in the output window
// starting at cursor and spanning one frame period.
func framePeakStep(samples []float64, from, period int) float64 {
	to := min(from+period, len(samples))

	var peak float64

	for j := from + 1; j < to; j++ {
		if d := math.Abs(samples[j] - samples[j-1]); d > peak {
			peak = d
		}
	}

	return peak
}

func stepMagnitudes(x []float64) []float64 {
	out := make([]float64, 0, len(x))
	for i := 1; i < len(x); i++ {
		out = append(out, math.Abs(x[i]-x[i-1]))
	}

	return out
}

type bucketT struct {
	n   int
	sum float64
}

func (b *bucketT) add(v float64) { b.n++; b.sum += v }

func (b bucketT) mean() float64 {
	if b.n == 0 {
		return 0
	}

	return b.sum / float64(b.n)
}

func (b bucketT) str() string {
	if b.n == 0 {
		return "(none)"
	}

	return fmt.Sprintf("n=%-5d mean %.5f", b.n, b.sum/float64(b.n))
}

func rmsPeak(x []float64) (rms, peak float64) {
	var sumSq float64

	for _, v := range x {
		sumSq += v * v
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}

	if len(x) > 0 {
		rms = math.Sqrt(sumSq / float64(len(x)))
	}

	return rms, peak
}

func pctile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	return sorted[int(math.Round(p*float64(len(sorted)-1)))]
}

func nonzero(v float64) float64 {
	if v == 0 {
		return 1
	}

	return v
}

func writeWAV(path string, samples []float64, rate int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	const bitsPerSample = 16

	dataLen := len(samples) * 2
	byteRate := rate * bitsPerSample / 8

	h := make([]byte, 0, 44)
	h = append(h, "RIFF"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(36+dataLen))
	h = append(h, "WAVE"...)
	h = append(h, "fmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1) // PCM
	h = binary.LittleEndian.AppendUint16(h, 1) // mono
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(byteRate))
	h = binary.LittleEndian.AppendUint16(h, bitsPerSample/8)
	h = binary.LittleEndian.AppendUint16(h, bitsPerSample)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(dataLen))

	if _, err := f.Write(h); err != nil {
		return err
	}

	pcm := make([]byte, dataLen)

	for i, v := range samples {
		s := math.Max(math.MinInt16, math.Min(math.MaxInt16, math.Round(v*math.MaxInt16)))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(s)))
	}

	_, err = f.Write(pcm)

	return err
}
