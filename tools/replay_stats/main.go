// Command replay_stats quantifies the reception quality of a recorded telemetry
// replay (.gtz) before any audio is generated from it. It tests two hypotheses
// about the haptic artifacting:
//
//  1. Dropped / jittered telemetry packets. Gran Turismo increments a per-packet
//     sequence ID at a steady 60 Hz; a gap in that sequence is a packet that was
//     genuinely lost at record time. This tool counts those gaps and their sizes.
//
//  2. Buffer-write-boundary artifacts. The engine haptic regenerates its waveform
//     from the latest RPM every engine-frame and stitches it into the live buffer
//     at a zero crossing (app_haptics_engine.go). The size of the RPM step between
//     consecutive packets bounds how large that seam discontinuity can be, and a
//     dropped packet makes the step jump further. This tool reports the RPM-step
//     distribution, split by whether the step spans a dropped packet.
//
// It reads the file in batch via the gt-telemetry Scan() iterator (no real-time
// playback, no packets dropped by us), so the only drops it sees are the ones
// baked into the recording. There is no per-packet wall-clock timestamp in the
// format, so true inter-arrival jitter cannot be recovered; sequence gaps are the
// faithful proxy for reception loss.
//
//	go run ./tools/replay_stats                                   # data/replays/demo.gtz
//	go run ./tools/replay_stats -file data/replays/short.gtz
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rs/zerolog"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
	gtmodels "github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// packetRateHz is Gran Turismo's telemetry packet rate; the sequence ID advances
// by one per packet at this rate, so one missing ID is ~16.7 ms of lost data.
const packetRateHz = 60.0

func main() {
	file := flag.String("file", "data/replays/demo.gtz", "path to a .gtz replay file")
	topN := flag.Int("top", 10, "how many largest gaps / RPM steps to list")
	rpmFloor := flag.Float64("rpm-floor", 1.0, "ignore frames with RPM below this (menus/stationary)")

	flag.Parse()

	err := run(*file, *topN, *rpmFloor)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// frame is the per-packet data we keep (the Scan iterator reuses its Transformer,
// so everything we need must be copied out each iteration).
type frame struct {
	seq      uint32
	rpm      float64
	gear     int
	throttle float64
	state    gtmodels.GameState
}

func run(file string, topN int, rpmFloor float64) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}

	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("replay file: %w", err)
	}

	logger := zerolog.New(os.Stderr).Level(zerolog.ErrorLevel)
	source := (&url.URL{Scheme: "file", Path: abs}).String()

	client, err := gttelemetry.New(gttelemetry.Options{Source: source, Logger: &logger})
	if err != nil {
		return fmt.Errorf("open telemetry client: %w", err)
	}

	frames := make([]frame, 0, 1<<16)

	for tf, scanErr := range client.Scan(context.Background()) {
		if scanErr != nil {
			return fmt.Errorf("scan: %w", scanErr)
		}

		frames = append(frames, frame{
			seq:      tf.SequenceID(),
			rpm:      float64(tf.EngineRPM()),
			gear:     tf.CurrentGear(),
			throttle: float64(tf.ThrottleOutputPercent()),
			state:    tf.GameState(),
		})
	}

	if len(frames) == 0 {
		return fmt.Errorf("no telemetry frames decoded from %s", file)
	}

	report(file, frames, topN, rpmFloor)

	return nil
}

// gap is a detected run of missing sequence IDs.
type gap struct {
	afterSeq uint32  // last delivered sequence before the gap
	missing  int     // number of packets missing
	rpmJump  float64 // |RPM change| measured across the gap
}

// rpmStep is an RPM change between two consecutive delivered packets.
type rpmStep struct {
	fromSeq   uint32
	delta     float64 // signed RPM change
	fromRPM   float64
	toRPM     float64
	acrossGap bool
}

func report(file string, frames []frame, topN int, rpmFloor float64) {
	fmt.Printf("file   : %s\n", file)
	fmt.Printf("packets: %d decoded\n", len(frames))
	fmt.Printf("states : %s\n\n", stateBreakdown(frames))

	// --- Sequence / drop analysis -----------------------------------------
	// Sessions restart the sequence at a low value; treat a non-increasing step
	// as a session boundary rather than a (nonsensical) negative gap.
	var (
		gaps        []gap
		delivered   int // consecutive-pair count where seq advanced
		totalMissed int
		sessions    int
		contiguous  []rpmStep
		acrossGaps  []rpmStep
	)

	for i := 1; i < len(frames); i++ {
		prev, cur := frames[i-1], frames[i]

		step := int64(cur.seq) - int64(prev.seq)
		if step <= 0 {
			sessions++

			continue // session reset; not a comparable pair
		}

		delivered++

		rs := rpmStep{fromSeq: prev.seq, delta: cur.rpm - prev.rpm, fromRPM: prev.rpm, toRPM: cur.rpm}

		if step > 1 {
			missing := int(step - 1)
			totalMissed += missing
			rs.acrossGap = true

			gaps = append(gaps, gap{afterSeq: prev.seq, missing: missing, rpmJump: math.Abs(cur.rpm - prev.rpm)})
			acrossGaps = append(acrossGaps, rs)
		} else {
			contiguous = append(contiguous, rs)
		}
	}

	expected := delivered + totalMissed // pairs that should have been contiguous
	dropRate := 0.0

	if expected > 0 {
		dropRate = 100 * float64(totalMissed) / float64(expected)
	}

	fmt.Println("== packet reception (hypothesis 1: drops/jitter) ==")
	fmt.Printf("  session resets   : %d\n", sessions)
	fmt.Printf("  delivered pairs  : %d\n", delivered)
	fmt.Printf("  dropped packets  : %d across %d gaps (%.3f%% of expected stream)\n", totalMissed, len(gaps), dropRate)

	if len(gaps) > 0 {
		fmt.Printf("  lost time        : ~%.1f ms total (%.1f ms in the largest gap)\n",
			1000*float64(totalMissed)/packetRateHz, 1000*float64(maxMissing(gaps))/packetRateHz)
		fmt.Printf("  largest gaps     :\n")

		sort.Slice(gaps, func(a, b int) bool { return gaps[a].missing > gaps[b].missing })

		for _, g := range gaps[:min(topN, len(gaps))] {
			fmt.Printf("    after seq %-8d  %2d missing (~%5.1f ms)  RPM jump %7.0f\n",
				g.afterSeq, g.missing, 1000*float64(g.missing)/packetRateHz, g.rpmJump)
		}
	}

	// --- RPM-step analysis (hypothesis 2: seam discontinuity magnitude) ----
	fmt.Println("\n== RPM steps between packets (hypothesis 2: seam jump size) ==")
	fmt.Printf("  (engine RPM >= %.0f; one step bounds the engine-haptic buffer seam)\n", rpmFloor)

	reportSteps("  contiguous (seq+1)", filterSteps(contiguous, rpmFloor))
	reportSteps("  across a drop     ", filterSteps(acrossGaps, rpmFloor))

	// Largest individual steps, regardless of gap, for inspection.
	all := append(append([]rpmStep{}, contiguous...), acrossGaps...)
	all = filterSteps(all, rpmFloor)
	sort.Slice(all, func(a, b int) bool { return math.Abs(all[a].delta) > math.Abs(all[b].delta) })

	if len(all) > 0 {
		fmt.Printf("  largest RPM steps :\n")

		for _, s := range all[:min(topN, len(all))] {
			tag := ""
			if s.acrossGap {
				tag = "  (across drop)"
			}

			fmt.Printf("    seq %-8d  %+7.0f rpm  (%6.0f -> %6.0f)%s\n", s.fromSeq, s.delta, s.fromRPM, s.toRPM, tag)
		}
	}

	// --- Gear changes (transmission haptic triggers) ----------------------
	gearChanges := 0

	for i := 1; i < len(frames); i++ {
		if frames[i].gear != frames[i-1].gear && int64(frames[i].seq)-int64(frames[i-1].seq) > 0 {
			gearChanges++
		}
	}

	fmt.Printf("\n== events ==\n  gear changes     : %d (transmission haptic triggers)\n", gearChanges)

	// --- Verdict ----------------------------------------------------------
	fmt.Println()
	conclude(dropRate, len(gaps), filterSteps(contiguous, rpmFloor), filterSteps(acrossGaps, rpmFloor))
}

// reportSteps prints summary statistics for a set of RPM steps.
func reportSteps(label string, steps []rpmStep) {
	if len(steps) == 0 {
		fmt.Printf("%s: (none)\n", label)

		return
	}

	abss := make([]float64, len(steps))
	for i, s := range steps {
		abss[i] = math.Abs(s.delta)
	}

	sort.Float64s(abss)

	fmt.Printf("%s: n=%-6d  mean %5.0f  p50 %5.0f  p95 %5.0f  p99 %5.0f  max %6.0f rpm\n",
		label, len(steps), mean(abss), pct(abss, 0.50), pct(abss, 0.95), pct(abss, 0.99), abss[len(abss)-1])
}

// conclude prints a short interpretation pointing at the likely artifact source.
func conclude(dropRate float64, gapCount int, contiguous, acrossGaps []rpmStep) {
	fmt.Println("== reading ==")

	switch {
	case gapCount == 0:
		fmt.Println("  No dropped packets in this recording: hypothesis 1 (reception loss) is not")
		fmt.Println("  the cause of artifacts in THIS replay. Inter-arrival jitter is not recoverable")
		fmt.Println("  from a batch read, so a live-source capture is still needed to rule it out.")
	case dropRate < 0.5:
		fmt.Printf("  Sparse drops (%.3f%%). Unlikely to be the dominant artifact source on their own,\n", dropRate)
		fmt.Println("  but each drop enlarges an engine-haptic seam step (see across-drop stats above).")
	default:
		fmt.Printf("  Frequent drops (%.3f%%). A plausible contributor: every gap forces a larger RPM\n", dropRate)
		fmt.Println("  step at the engine-haptic buffer seam.")
	}

	if len(contiguous) > 0 && len(acrossGaps) > 0 {
		cm, am := pct(absDeltas(contiguous), 0.95), pct(absDeltas(acrossGaps), 0.95)
		if am > 1.5*cm && cm > 0 {
			fmt.Printf("  RPM steps across drops are ~%.1fx larger at p95 (%.0f vs %.0f rpm): drops do\n", am/cm, am, cm)
			fmt.Println("  measurably worsen the seam discontinuity the engine haptic must absorb.")
		}
	}

	fmt.Println("  Next: feed this RPM sequence through the engine waveform + buffer seam and capture")
	fmt.Println("  the output to see whether these steps actually become audible discontinuities.")
}

// --- helpers --------------------------------------------------------------

func filterSteps(steps []rpmStep, rpmFloor float64) []rpmStep {
	out := steps[:0:0]

	for _, s := range steps {
		if s.fromRPM >= rpmFloor && s.toRPM >= rpmFloor {
			out = append(out, s)
		}
	}

	return out
}

func absDeltas(steps []rpmStep) []float64 {
	out := make([]float64, len(steps))
	for i, s := range steps {
		out[i] = math.Abs(s.delta)
	}

	sort.Float64s(out)

	return out
}

func maxMissing(gaps []gap) int {
	m := 0
	for _, g := range gaps {
		if g.missing > m {
			m = g.missing
		}
	}

	return m
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}

	var s float64
	for _, x := range xs {
		s += x
	}

	return s / float64(len(xs))
}

// pct returns the p-quantile (0..1) of an already-sorted slice.
func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	idx := int(math.Round(p * float64(len(sorted)-1)))

	return sorted[idx]
}

func stateBreakdown(frames []frame) string {
	counts := map[gtmodels.GameState]int{}
	for _, f := range frames {
		counts[f.state]++
	}

	name := map[gtmodels.GameState]string{
		gtmodels.GameStateUnknown:  "unknown",
		gtmodels.GameStateMainMenu: "menu",
		gtmodels.GameStateRaceMenu: "race-menu",
		gtmodels.GameStateLive:     "live",
		gtmodels.GameStateReplay:   "replay",
	}

	order := []gtmodels.GameState{
		gtmodels.GameStateLive, gtmodels.GameStateReplay,
		gtmodels.GameStateRaceMenu, gtmodels.GameStateMainMenu, gtmodels.GameStateUnknown,
	}

	parts := make([]string, 0, len(order))

	for _, s := range order {
		if c := counts[s]; c > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", name[s], c))
		}
	}

	out := ""

	var outSb373 strings.Builder

	for i, p := range parts {
		if i > 0 {
			outSb373.WriteString(", ")
		}

		outSb373.WriteString(p)
	}

	out += outSb373.String()

	return out
}
