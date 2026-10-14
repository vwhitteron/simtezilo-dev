package haptics //nolint:testpackage // white-box testing

// Diagnostic probe for the road-roughness kinematics measurement
// (Kinematics.SuspensionRoughness / SuspensionRoughnessValid). It exists because
// textureRoughnessRefMps, the constant the texture layer will scale roughness
// against, cannot be guessed: it depends on the game's suspension model and the
// car's spring and damper rates, both unknown until measured.
//
// It drives the real kinematics pipeline over a recorded replay, collects a
// roughness sample per frame, and reports the percentiles, correlations, and a
// go/no-go verdict a candidate reference value needs. It is env-gated and writes
// no files: run it with -v and read the log.
//
//	ROUGHNESS_PROBE_REPLAY="$PWD/data/replays/<name>.gtz" \
//	  go test ./app/haptics -run TestRoughnessProbe -v

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	"github.com/vwhitteron/simtezilo-dev/app/videotelemetry"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
	gtmodels "github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// roughnessProbeMinSpeedDefault excludes pit-lane/grid frames, where near-zero
// speed leaves suspension near static and roughness has nothing to measure.
const roughnessProbeMinSpeedDefault = 5.0

// roughnessProbeMinFrames is the smallest usable population the report trusts.
// Below it the percentiles and correlations are too noisy to read.
const roughnessProbeMinFrames = 500

// roughnessProbeHistBuckets is the resolution of the roughness/ref histogram.
const roughnessProbeHistBuckets = 20

// roughnessProbeHistSpan is the roughness/ref ratio the histogram's fixed buckets
// span before frames fall into the overflow bucket.
const roughnessProbeHistSpan = 3.0

func TestRoughnessProbe(t *testing.T) {
	t.Parallel()

	path := os.Getenv("ROUGHNESS_PROBE_REPLAY")
	if path == "" {
		t.Skip("set ROUGHNESS_PROBE_REPLAY to a replay path (.gtz/.gtr) or an .mp4 with an embedded telemetry track to run")
	}

	minSpeed := roughnessProbeMinSpeedDefault
	if v, ok := probeEnvFloat("ROUGHNESS_PROBE_MINSPEED"); ok {
		minSpeed = v
	}

	lap, filterLap := roughnessProbeLapFilter()

	t.Logf("minSpeed=%.1f lapFilter=%v(%d)", minSpeed, filterLap, lap)

	source, err := resolveRoughnessSource(t, path)
	if err != nil {
		t.Fatalf("resolving replay source: %v", err)
	}

	client, err := gttelemetry.New(gttelemetry.Options{
		Source: source,
		Format: gtmodels.Addendum3,
	})
	if err != nil {
		t.Fatalf("telemetry client: %v", err)
	}

	frames, kin, err := runRoughnessProbe(t, client, lap, filterLap)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	reportRoughnessProbe(t, frames, kin, minSpeed)
}

// roughnessProbeLapFilter reads the optional lap restriction. Restricting to one
// lap isolates a single, known surface run from a multi-lap replay that mixes
// track sections or conditions.
func roughnessProbeLapFilter() (int16, bool) {
	v, ok := probeEnvFloat("ROUGHNESS_PROBE_LAP")
	if !ok {
		return 0, false
	}

	return int16(v), true
}

// resolveRoughnessSource turns the env path into a telemetry source URL.
//
// A video's embedded telemetry track is a flat stream of deciphered GT packets
// muxed one-per-sample into the mp4 container. That is exactly the .gtr format,
// so an mp4 is unwrapped into a temporary .gtr and read like any other replay.
func resolveRoughnessSource(t *testing.T, path string) (string, error) {
	t.Helper()

	if !strings.HasSuffix(path, ".mp4") {
		return "file://" + path, nil
	}

	index, err := videotelemetry.Open(path)
	if err != nil {
		return "", err
	}

	defer index.Close()

	gtrPath := filepath.Join(t.TempDir(), "roughness-probe.gtr")

	out, err := os.Create(gtrPath)
	if err != nil {
		return "", err
	}

	defer out.Close()

	err = index.WriteGTR(out)
	if err != nil {
		return "", err
	}

	return "file://" + gtrPath, nil
}

// roughnessFrame is one telemetry frame's worth of the quantities the roughness
// measurement is judged against.
type roughnessFrame struct {
	seq            uint32
	lap            int16
	roughness      float64
	valid          bool
	speed          float64
	surface        gtmodels.SurfaceType
	absSnap        float64
	zeroSuspension bool
}

// runRoughnessProbe advances the replay frame by frame, updating the real
// kinematics chain and recording one roughnessFrame per delivered packet once
// the vehicle is known. No haptic generator runs: the probe measures kinematics
// only, so the synth/calibrator rig the gear-shift probe needs is not built here.
func runRoughnessProbe(t *testing.T, client *gttelemetry.Client, lap int16, filterLap bool) ([]roughnessFrame, kinematics.State, error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		kin     kinematics.State
		dims    vehicle.Dimensions
		built   bool
		lastSeq uint32
		frames  []roughnessFrame
	)

	for frame, frameErr := range client.Scan(ctx) {
		if frameErr != nil {
			return nil, kin, frameErr
		}

		if !frame.TelemetryStarted() {
			continue
		}

		seq := frame.SequenceID()

		// Mirror capture.go's chassisRun: the vehicle is captured on the first live
		// frame, and that same frame's delta collapses to one (chassisLastSeq==seq),
		// not the huge wrap distance from a zero-valued lastSeq.
		if !built && frame.VehicleEngineLayout() != "" {
			dims = captureDimensions(client)
			lastSeq = seq
			built = true
		}

		if !built {
			continue
		}

		kin.Update(float64(frameDelta(seq, lastSeq))/float64(telemetryFrameRate), dims, client)
		lastSeq = seq

		curLap := frame.CurrentLap()
		if filterLap && curLap != lap {
			continue
		}

		frames = append(frames, sampleRoughnessFrame(seq, curLap, &kin))
	}

	return frames, kin, nil
}

// sampleRoughnessFrame reads the current kinematics state into a roughnessFrame.
func sampleRoughnessFrame(seq uint32, lap int16, kin *kinematics.State) roughnessFrame {
	cur := kin.Current

	return roughnessFrame{
		seq:            seq,
		lap:            lap,
		roughness:      cur.SuspensionRoughness,
		valid:          cur.SuspensionRoughnessValid,
		speed:          cur.GroundSpeed,
		surface:        dominantSurface(cur.SurfaceType),
		absSnap:        math.Abs(cur.ResolvedTransSnap),
		zeroSuspension: cur.SuspensionHeight == (gtmodels.CornerSet{}),
	}
}

// dominantSurface picks the surface type appearing most often across the four
// corners. Front-left is checked first, so it wins any tie, including the 2-2
// case explicitly called out for this probe.
func dominantSurface(corners gtmodels.CornerSetGeneric[gtmodels.SurfaceType]) gtmodels.SurfaceType {
	values := [4]gtmodels.SurfaceType{corners.FrontLeft, corners.FrontRight, corners.RearLeft, corners.RearRight}

	counts := make(map[gtmodels.SurfaceType]int, 4)
	for _, v := range values {
		counts[v]++
	}

	best, bestCount := values[0], 0

	for _, v := range values {
		if counts[v] > bestCount {
			best, bestCount = v, counts[v]
		}
	}

	return best
}

func surfaceName(s gtmodels.SurfaceType) string { return s.String() }

// reportRoughnessProbe emits every measurement table plus the go/no-go verdict.
func reportRoughnessProbe(t *testing.T, frames []roughnessFrame, kin kinematics.State, minSpeed float64) {
	t.Helper()

	usable := usableRoughnessFrames(frames, minSpeed)

	t.Logf("scanned=%d usable=%d (valid && speed>%.1f)", len(frames), len(usable), minSpeed)

	// Below this the percentiles and correlations below are too noisy to trust,
	// and that is a real failure of the input, not a verdict for the human to read.
	if len(usable) < roughnessProbeMinFrames {
		t.Fatalf("usable frames %d below the %d minimum needed for a report", len(usable), roughnessProbeMinFrames)

		return
	}

	logOverallPercentiles(t, usable)
	bandCells := logSurfaceSpeedBands(t, usable)
	ref, refN := logCalibrationLine(t, bandCells)
	logSpeedCorrelations(t, usable)
	dynRange := logDynamicRange(t, bandCells)
	logSnapCorrelation(t, usable)
	logDutyCycles(t, frames, kin)
	logRoughnessHistogram(t, usable, ref)

	logVerdict(t, dynRange, pearsonR(speedsOf(usable), roughnessesOf(usable)),
		pearsonR(roughnessesOf(usable), absSnapsOf(usable)), bandCells, ref, refN)
}

func usableRoughnessFrames(frames []roughnessFrame, minSpeed float64) []roughnessFrame {
	out := make([]roughnessFrame, 0, len(frames))

	for _, f := range frames {
		if f.valid && f.speed > minSpeed {
			out = append(out, f)
		}
	}

	return out
}

func roughnessesOf(frames []roughnessFrame) []float64 {
	out := make([]float64, len(frames))
	for i, f := range frames {
		out[i] = f.roughness
	}

	return out
}

func speedsOf(frames []roughnessFrame) []float64 {
	out := make([]float64, len(frames))
	for i, f := range frames {
		out[i] = f.speed
	}

	return out
}

func absSnapsOf(frames []roughnessFrame) []float64 {
	out := make([]float64, len(frames))
	for i, f := range frames {
		out[i] = f.absSnap
	}

	return out
}

// logOverallPercentiles reports the shape of the whole usable population, before
// it is split by surface or speed.
func logOverallPercentiles(t *testing.T, usable []roughnessFrame) {
	t.Helper()

	r := roughnessesOf(usable)

	t.Logf("PCT p1=%.6g p5=%.6g p25=%.6g p50=%.6g p75=%.6g p90=%.6g p99=%.6g m/s",
		percentile(r, 0.01), percentile(r, 0.05), percentile(r, 0.25), percentile(r, 0.50),
		percentile(r, 0.75), percentile(r, 0.90), percentile(r, 0.99))
}

// speedBand is one of the fixed bands the by-surface table is cut into.
type speedBand struct {
	label  string
	lo, hi float64
}

// roughnessSpeedBands returns the fixed speed cuts the report uses everywhere a
// band breakdown is needed, so every table cuts the data the same way.
func roughnessSpeedBands() []speedBand {
	return []speedBand{
		{"5-10", 5, 10},
		{"10-20", 10, 20},
		{"20-30", 20, 30},
		{"30-40", 30, 40},
		{"40+", 40, math.Inf(1)},
	}
}

// bandCell is one surface x speed-band cell's population and its roughness stats.
type bandCell struct {
	surface  gtmodels.SurfaceType
	band     speedBand
	frames   []roughnessFrame
	p10, p50 float64
	p90      float64
}

// bandCellMinN gates which cells are trusted enough to print stats for. A thin
// cell's percentiles are noise, not signal.
const bandCellMinN = 200

// logSurfaceSpeedBands builds and prints the surface x speed-band table, and
// returns the cells so the calibration line and dynamic-range checks reuse the
// exact same population they were read from.
func logSurfaceSpeedBands(t *testing.T, usable []roughnessFrame) []bandCell {
	t.Helper()

	surfaces := distinctSurfaces(usable)
	bands := roughnessSpeedBands()
	cells := make([]bandCell, 0, len(surfaces)*len(bands))

	for _, surface := range surfaces {
		for _, band := range bands {
			cell := bandCell{surface: surface, band: band, frames: framesInCell(usable, surface, band)}
			cell.p10, cell.p50, cell.p90 = cellPercentiles(cell.frames)
			cells = append(cells, cell)

			logBandCell(t, cell)
		}
	}

	return cells
}

func cellPercentiles(frames []roughnessFrame) (p10, p50, p90 float64) {
	r := roughnessesOf(frames)

	return percentile(r, 0.10), percentile(r, 0.50), percentile(r, 0.90)
}

func logBandCell(t *testing.T, cell bandCell) {
	t.Helper()

	cellCount := len(cell.frames)
	if cellCount < bandCellMinN {
		t.Logf("BAND %-8s %-6s n=%-6d (suppressed, below n=%d)", surfaceName(cell.surface), cell.band.label, cellCount, bandCellMinN)

		return
	}

	t.Logf("BAND %-8s %-6s n=%-6d p10=%.6g p50=%.6g p90=%.6g",
		surfaceName(cell.surface), cell.band.label, cellCount, cell.p10, cell.p50, cell.p90)
}

func distinctSurfaces(frames []roughnessFrame) []gtmodels.SurfaceType {
	seen := map[gtmodels.SurfaceType]struct{}{}
	for _, f := range frames {
		seen[f.surface] = struct{}{}
	}

	out := make([]gtmodels.SurfaceType, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}

	slices.Sort(out)

	return out
}

func framesInCell(frames []roughnessFrame, surface gtmodels.SurfaceType, band speedBand) []roughnessFrame {
	out := make([]roughnessFrame, 0)

	for _, f := range frames {
		if f.surface == surface && f.speed >= band.lo && f.speed < band.hi {
			out = append(out, f)
		}
	}

	return out
}

// tarmac2040 collects every tarmac frame across the 20-30 and 30-40 bands
// combined, which is the population the calibration reference and the dynamic
// range check both read.
func tarmac2040(cells []bandCell) []roughnessFrame {
	out := make([]roughnessFrame, 0)

	for _, cell := range cells {
		if cell.surface != gtmodels.SurfaceTypeTarmac {
			continue
		}

		if cell.band.label != "20-30" && cell.band.label != "30-40" {
			continue
		}

		out = append(out, cell.frames...)
	}

	return out
}

// logCalibrationLine prints the candidate textureRoughnessRefMps: tarmac's median
// roughness in the speed range the texture layer actually renders at speed.
func logCalibrationLine(t *testing.T, cells []bandCell) (float64, int) {
	t.Helper()

	pop := tarmac2040(cells)
	ref := percentile(roughnessesOf(pop), 0.5)

	t.Log(strings.Repeat("=", 72))
	t.Logf("= CALIBRATION: candidate textureRoughnessRefMps=%.6g (tarmac 20-40 m/s, n=%d)", ref, len(pop))
	t.Log(strings.Repeat("=", 72))

	return ref, len(pop)
}

// logSpeedCorrelations reports r(roughness, speed) overall and per band, which
// separates "roughness rises with speed everywhere" from "roughness rises with
// speed only because faster corners happen to be rougher corners".
func logSpeedCorrelations(t *testing.T, usable []roughnessFrame) {
	t.Helper()

	t.Logf("CORR roughness~speed overall r=%.4f n=%d", pearsonR(speedsOf(usable), roughnessesOf(usable)), len(usable))

	for _, band := range roughnessSpeedBands() {
		inBand := framesInBand(usable, band)
		if len(inBand) < bandCellMinN {
			continue
		}

		t.Logf("CORR roughness~speed %-6s r=%.4f n=%d", band.label,
			pearsonR(speedsOf(inBand), roughnessesOf(inBand)), len(inBand))
	}
}

// framesInBand filters to one speed band across every surface, for the
// speed-only (not surface-split) correlation table.
func framesInBand(frames []roughnessFrame, band speedBand) []roughnessFrame {
	out := make([]roughnessFrame, 0)

	for _, f := range frames {
		if f.speed >= band.lo && f.speed < band.hi {
			out = append(out, f)
		}
	}

	return out
}

// logDynamicRange reports p90/p10 for the calibration population: how wide the
// roughness signal actually swings once speed and surface are held near-fixed.
func logDynamicRange(t *testing.T, cells []bandCell) float64 {
	t.Helper()

	pop := tarmac2040(cells)
	r := roughnessesOf(pop)

	p10, p90 := percentile(r, 0.10), percentile(r, 0.90)

	ratio := 0.0
	if p10 > 0 {
		ratio = p90 / p10
	}

	t.Logf("RANGE tarmac 20-40 m/s p10=%.6g p90=%.6g p90/p10=%.4f", p10, p90, ratio)

	return ratio
}

// logSnapCorrelation reports r(roughness, |snap|): how much roughness merely
// re-derives the chassis pulse the transmission/chassis layers already play.
func logSnapCorrelation(t *testing.T, usable []roughnessFrame) {
	t.Helper()

	r := pearsonR(roughnessesOf(usable), absSnapsOf(usable))

	t.Logf("CORR roughness~|snap| overall r=%.4f n=%d", r, len(usable))
}

// logDutyCycles reports how often the measurement has nothing to say, over the
// full recorded population (not the speed-thresholded one), since a consumer
// hits every frame regardless of speed.
func logDutyCycles(t *testing.T, frames []roughnessFrame, kin kinematics.State) {
	t.Helper()

	if len(frames) == 0 {
		return
	}

	var zeroSuspension, invalid int

	for _, f := range frames {
		if f.zeroSuspension {
			zeroSuspension++
		}

		if !f.valid {
			invalid++
		}
	}

	n := float64(len(frames))

	t.Logf("DUTY zeroSuspension=%.1f%% invalid=%.1f%% gapResets=%d lastGapDelta=%d",
		float64(zeroSuspension)/n*100, float64(invalid)/n*100, kin.GapResets, kin.LastGapDelta)
}

// logRoughnessHistogram buckets roughness/ref from 0 to roughnessProbeHistSpan
// plus an overflow bucket, so an outlier tail is visible without stretching the
// bucket width to accommodate it.
func logRoughnessHistogram(t *testing.T, usable []roughnessFrame, ref float64) {
	t.Helper()

	if ref <= 0 {
		t.Log("HIST skipped: no calibration reference (tarmac 20-40 m/s population is empty)")

		return
	}

	counts := make([]int, roughnessProbeHistBuckets+1) // last slot is overflow
	bucketWidth := roughnessProbeHistSpan / roughnessProbeHistBuckets

	for _, f := range usable {
		ratio := f.roughness / ref

		bucket := min(max(int(ratio/bucketWidth), 0), roughnessProbeHistBuckets)

		counts[bucket]++
	}

	logHistogramBars(t, counts, bucketWidth)
}

func logHistogramBars(t *testing.T, counts []int, bucketWidth float64) {
	t.Helper()

	maxCount := 0
	for _, bucketCount := range counts {
		if bucketCount > maxCount {
			maxCount = bucketCount
		}
	}

	const barWidth = 50

	for i, bucketCount := range counts {
		label := fmt3g(float64(i)*bucketWidth) + "-" + fmt3g(float64(i+1)*bucketWidth)
		if i == len(counts)-1 {
			label = "overflow"
		}

		bar := ""
		if maxCount > 0 {
			bar = strings.Repeat("#", bucketCount*barWidth/maxCount)
		}

		t.Logf("HIST %-11s n=%-6d %s", label, bucketCount, bar)
	}
}

// fmt3g formats a bucket boundary to 3 significant figures, trimming a
// trailing decimal point so whole-number boundaries read cleanly. Guards zero
// separately: naive trailing-zero trimming reduces "0" to an empty string.
func fmt3g(v float64) string {
	if v == 0 {
		return "0"
	}

	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3g", v), "0"), ".")
}

// logVerdict prints the explicit PASS/ABORT block. It never fails the test: this
// is a measurement tool and the human reads the verdict.
func logVerdict(t *testing.T, tarmacRange, speedR, snapR float64, cells []bandCell, ref float64, refN int) {
	t.Helper()

	t.Log("VERDICT")

	// A flat envelope means roughness cannot distinguish anything; scaling it
	// against textureRoughnessRefMps would just replay a constant.
	verdictLine(t, tarmacRange >= 1.5, "flat envelope — the roughness signal is a constant",
		"tarmac p90/p10=%.4f (need >= 1.5)", tarmacRange)

	// High speed correlation plus a narrow range means roughness is standing in
	// for ground speed, which the effect already has direct access to.
	speedProxy := speedR > 0.85 && tarmacRange < 1.3
	verdictLine(t, !speedProxy, "speed proxy — roughness duplicates the existing speed curve",
		"r(speed)=%.4f tarmac p90/p10=%.4f (abort needs r>0.85 and range<1.3)", speedR, tarmacRange)

	// High correlation with |snap| means roughness fires on the same chassis
	// events the existing pulse already renders, so it would add no information.
	verdictLine(t, snapR <= 0.8, "double-counting — roughness tracks the same events as the chassis pulse",
		"r(|snap|)=%.4f (need <= 0.8)", snapR)

	logPassCriteria(t, tarmacRange, cells, ref, refN)
}

func verdictLine(t *testing.T, pass bool, label, detail string, args ...any) {
	t.Helper()

	status := "PASS"
	if !pass {
		status = "ABORT"
	}

	t.Logf("VERDICT %-6s %s | %s", status, label, fmt.Sprintf(detail, args...))
}

func logPassCriteria(t *testing.T, tarmacRange float64, cells []bandCell, ref float64, refN int) {
	t.Helper()

	rangeOK := tarmacRange >= 2.0

	verdictLine(t, rangeOK, "dynamic range — tarmac 20-40 m/s must separate smooth from rough",
		"tarmac p90/p10=%.4f (need >= 2.0)", tarmacRange)

	offRoad, offRoadN := offRoadMedian(cells)
	if offRoadN < bandCellMinN {
		t.Logf("VERDICT n/a    dirt/grass contrast — no dirt or grass population with n >= %d (n=%d)", bandCellMinN, offRoadN)

		return
	}

	contrastOK := ref > 0 && offRoad >= ref*1.5

	verdictLine(t, contrastOK, "dirt/grass contrast — off-road must read at least 1.5x tarmac",
		"offRoadMedian=%.6g tarmacRef=%.6g (n=%d/%d)", offRoad, ref, offRoadN, refN)
}

// offRoadMedian combines every dirt and grass frame across all speed bands, and
// returns its median roughness plus population size.
func offRoadMedian(cells []bandCell) (float64, int) {
	var frames []roughnessFrame

	for _, cell := range cells {
		if cell.surface != gtmodels.SurfaceTypeDirt && cell.surface != gtmodels.SurfaceTypeGrass {
			continue
		}

		frames = append(frames, cell.frames...)
	}

	return percentile(roughnessesOf(frames), 0.5), len(frames)
}

// pearsonR is the linear correlation coefficient between two equal-length
// series. It returns 0 for a degenerate input rather than NaN, so a verdict
// comparison against it fails safe instead of propagating NaN silently.
func pearsonR(series1, series2 []float64) float64 {
	count := len(series1)
	if count == 0 || len(series2) != count {
		return 0
	}

	var sumX, sumY, sumXY, sumX2, sumY2 float64

	for i := range series1 {
		sumX += series1[i]
		sumY += series2[i]
		sumXY += series1[i] * series2[i]
		sumX2 += series1[i] * series1[i]
		sumY2 += series2[i] * series2[i]
	}

	nf := float64(count)
	num := nf*sumXY - sumX*sumY
	den := math.Sqrt((nf*sumX2 - sumX*sumX) * (nf*sumY2 - sumY*sumY))

	if den == 0 {
		return 0
	}

	return num / den
}
