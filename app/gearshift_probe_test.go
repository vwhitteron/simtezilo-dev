package app //nolint:testpackage // white-box testing

// Diagnostic probe for the gear-change haptic. It drives the real kinematics
// pipeline over a recorded replay, detects gear transitions exactly as the live
// app does, and dumps per-shift context so the transmission effect can be tuned
// against measured data rather than guessed at.
//
// It is env-gated and writes no files: run it with -v and read the log.
//
//	GEARSHIFT_PROBE_REPLAY="file://$PWD/data/replays/<name>.gtz" \
//	  go test ./app -run TestGearShiftProbe -v

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	gttelemetry "github.com/zetetos/gt-telemetry/v2"
	gtmodels "github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// probeShippedWindow is the measurement window the effect shipped with, retained so
// the probe can keep reporting how much of the true peak that window captured.
const probeShippedWindow = 3

const (
	// probeLead is how many frames before a shift the ring buffer retains, so the
	// pre-shift RPM and gear ratio are available at the transition.
	probeLead = 8

	// probeTrail is how many frames after a shift are followed. It must be long
	// enough to contain a gated manual's clutch re-engagement, which is the event
	// the shipped 3-frame window is suspected of missing.
	probeTrail = 32
)

// probeFrame is one telemetry frame's worth of the quantities the transmission
// effect either uses today or is a candidate to use.
type probeFrame struct {
	seq       uint32
	gear      int
	speed     float64
	surgeJerk float64
	surgeAcc  float64
	rpm       float64
	throttle  float64
	brake     float64
	clutchAct float64
	clutchEng float64
	ratio     float64
}

// probeShift is one detected gear change plus the window around it.
type probeShift struct {
	seq            uint32
	lap            int16
	from, to       int
	speed          float64
	throttle       float64
	brake          float64
	rpmBefore      float64
	ratioFrom      float64
	ratioTo        float64
	jerkTrace      []float64
	accTrace       []float64
	clutchTrace    []float64
	rpmTrace       []float64
	speedTrace     []float64
	argmaxOffset   int
	peakWindow3    float64 // peak jerk over the shipped 3-frame window
	peakWindowFull float64 // peak jerk over the full trail
	magnitudePlaye float64 // what the current code played
	jerkEWMAAt     float64

	// The re-sync replay: syncTarget mirrors gearShiftSyncTarget, computed from the
	// pre-shift frame exactly as the live code does. resyncOffset is the frame offset
	// (0 = shift frame) at which the criterion in app_haptics_transmission.go first
	// declares re-engagement, or -1 when the trail never satisfies it. peakToResync and
	// argmaxToResync are the peak jerk and its offset restricted to [0, resyncOffset],
	// which is what the live measurement window would actually have captured.
	syncTarget     float64
	resyncOffset   int
	peakToResync   float64
	argmaxToResync int
}

func (shift probeShift) down() bool { return shift.to < shift.from }

func (shift probeShift) ratioStep() float64 {
	if shift.ratioFrom <= 0 || shift.ratioTo <= 0 {
		return 0
	}

	return shift.ratioTo / shift.ratioFrom
}

func TestGearShiftProbe(t *testing.T) {
	t.Parallel()

	source := os.Getenv("GEARSHIFT_PROBE_REPLAY")
	if source == "" {
		t.Skip("set GEARSHIFT_PROBE_REPLAY to a file:// replay URL to run")
	}

	app, client, err := newProbeApp(source)
	if err != nil {
		t.Fatalf("probe app: %v", err)
	}

	defer func() { _ = app.synth.Close() }()

	shifts, format := app.runProbe(t, client)

	reportProbe(t, app, shifts, format)
}

// newProbeApp mirrors newCaptureApp but pins the telemetry format the way
// tuneassist does, since the .gtz replays carry Addendum3 payloads.
func newProbeApp(source string) (*App, *gttelemetry.Client, error) {
	app, _, err := newCaptureApp(source)
	if err != nil {
		return nil, nil, err
	}

	client, err := gttelemetry.New(gttelemetry.Options{
		Source: source,
		Format: gtmodels.Addendum3,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry client: %w", err)
	}

	app.gtClient = client

	applyProbeTuningOverrides(app)

	return app, client, nil
}

// applyProbeTuningOverrides lets a sweep drive the tuning knobs from the environment
// without editing defaults, so candidate calibrations can be compared across the
// whole replay set in one pass.
//
// The baseline every override applies on top of is set explicitly to literals
// matching the current shipped defaults, rather than left to whatever config.New
// happens to default to, so a retuned default cannot silently change what an
// env-var-less probe run measures.
func applyProbeTuningOverrides(app *App) {
	gearShiftCharacterMax = 1800.0
	gearShiftStepMax = 0.30
	gearShiftMaxMeasureFrames = 32

	app.config.SetHapticsTransmissionStepBlend(0.5)
	app.config.SetHapticsTransmissionJerkCurve(750)

	if v, ok := probeEnvFloat("GEARSHIFT_PROBE_CHARACTER_MAX"); ok {
		gearShiftCharacterMax = v
	}

	if v, ok := probeEnvFloat("GEARSHIFT_PROBE_STEP_MAX"); ok {
		gearShiftStepMax = v
	}

	if v, ok := probeEnvFloat("GEARSHIFT_PROBE_STEP_BLEND"); ok {
		app.config.SetHapticsTransmissionStepBlend(v)
	}

	if v, ok := probeEnvFloat("GEARSHIFT_PROBE_CURVE"); ok {
		app.config.SetHapticsTransmissionJerkCurve(int(v))
	}

	if v, ok := probeEnvFloat("GEARSHIFT_PROBE_FRAMES"); ok {
		gearShiftMaxMeasureFrames = int(v)
	}

	// Overrides the probe's own replay of the re-sync criterion (see resyncOffset),
	// not the live gearShiftSyncTolerance constant, so a sweep can find the tolerance
	// that best separates gearbox re-engagement from measurement noise without a
	// rebuild.
	if v, ok := probeEnvFloat("GEARSHIFT_PROBE_SYNC_TOL"); ok {
		probeSyncTolerance = v
	}
}

func probeEnvFloat(name string) (float64, bool) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}

	return value, true
}

// probeRunState carries the loop state runProbe accumulates across frames.
type probeRunState struct {
	ring    []probeFrame
	shifts  []probeShift
	open    []int // indices into shifts still collecting their trail
	format  string
	built   bool
	lastLap int16
}

// runProbe advances the replay frame by frame, building the vehicle on the first
// live frame and recording a window around every gear transition.
func (a *App) runProbe(t *testing.T, client *gttelemetry.Client) ([]probeShift, string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var state probeRunState

	for frame, frameErr := range client.Scan(ctx) {
		if frameErr != nil {
			t.Fatalf("scan: %v", frameErr)
		}

		a.processProbeFrame(frame, client, &state)
	}

	for idx := range state.shifts {
		finaliseProbeShift(&state.shifts[idx])
	}

	return state.shifts, state.format
}

// processProbeFrame advances the vehicle for one telemetry frame and, once the
// vehicle is built, records gear-change context into state.
func (a *App) processProbeFrame(frame *gttelemetry.Transformer, client *gttelemetry.Client, state *probeRunState) {
	if !frame.TelemetryStarted() {
		return
	}

	if !state.built && frame.VehicleEngineLayout() != "" {
		a.buildVehicleForCapture()

		a.state.telemetryActive = true
		state.built = true
	}

	if !state.built {
		return
	}

	a.kinematics.Update(1.0/frameRate, a.vehicle.Dimensions, client)
	a.advanceGearShiftDriveline()

	state.format = a.kinematics.Current.Format
	state.lastLap = frame.CurrentLap()

	cur := a.sampleProbeFrame(frame)

	// Extend every shift still inside its trail window before recording a new
	// one, so a shift detected this frame does not see its own trail.
	state.open = extendOpenShifts(state.shifts, state.open, cur)

	if a.gearHasChanged() && len(state.ring) > 0 {
		state.shifts = append(state.shifts, a.recordProbeShift(state.ring, cur, state.lastLap))
		state.open = append(state.open, len(state.shifts)-1)
	}

	// tickGearShiftMeasurement is deliberately not called on shift frames, matching
	// generateForceHaptics' if/else so the learned EWMA tracks the live app exactly.
	if !a.gearHasChanged() {
		a.tickGearShiftMeasurement()
	}

	state.ring = append(state.ring, cur)
	if len(state.ring) > probeLead {
		state.ring = state.ring[1:]
	}

	a.kinematics.Last = a.kinematics.Current
}

// sampleProbeFrame reads every quantity of interest for the current frame.
func (a *App) sampleProbeFrame(frame *gttelemetry.Transformer) probeFrame {
	return probeFrame{
		seq:       frame.SequenceID(),
		gear:      a.kinematics.Current.TransmissionGear,
		speed:     a.kinematics.Current.GroundSpeed,
		surgeJerk: a.kinematics.GetSurgeJerk(),
		surgeAcc:  a.kinematics.GetSurgeGforce() * 9.80665,
		rpm:       float64(frame.EngineRPM()),
		throttle:  float64(frame.ThrottleInputPercent()),
		brake:     float64(frame.BrakeInputPercent()),
		clutchAct: float64(frame.ClutchActuationPercent()),
		clutchEng: float64(frame.ClutchEngagementPercent()),
		ratio:     probeGearRatio(frame, a.kinematics.Current.TransmissionGear),
	}
}

// probeGearRatio reads the ratio for a gear without going through
// Transformer.CurrentGearRatio, which indexes GearRatios[gear-1] and so panics on
// reverse (gear 0). Returns 0 when the gear has no usable ratio.
func probeGearRatio(frame *gttelemetry.Transformer, gear int) float64 {
	ratios := frame.Transmission().GearRatios
	if gear < 1 || gear > len(ratios) {
		return 0
	}

	ratio := float64(ratios[gear-1])
	if ratio <= 0 {
		return 0
	}

	return ratio
}

// recordProbeShift captures the state at a detected transition, including what the
// current implementation would have played, before the effect mutates the profile.
func (a *App) recordProbeShift(ring []probeFrame, cur probeFrame, lap int16) probeShift {
	prev := ring[len(ring)-1]

	shift := probeShift{
		seq:        cur.seq,
		lap:        lap,
		from:       prev.gear,
		to:         cur.gear,
		speed:      cur.speed,
		throttle:   cur.throttle,
		brake:      cur.brake,
		rpmBefore:  prev.rpm,
		ratioFrom:  prev.ratio,
		ratioTo:    cur.ratio,
		jerkEWMAAt: a.gearShift.character(cur.gear < prev.gear),
	}

	// Mirror gearShiftSyncTarget exactly: rpm/speed at the new ratio, self-calibrating
	// because the final-drive/rolling-radius constant cancels out of the step. 0 means
	// this shift gives no usable prediction and finaliseProbeShift will never resync it.
	if prev.ratio > 0 && cur.ratio > 0 && prev.speed > 0 && prev.rpm > 0 {
		shift.syncTarget = (prev.rpm / prev.speed) * (cur.ratio / prev.ratio)
	}

	// Reproduce the live magnitude, then advance the learner exactly as
	// playGearShiftHaptic does so the EWMA trajectory matches the real session.
	shift.magnitudePlaye = a.determineGearShiftMagnitude()

	if a.gearShift.measuring {
		a.completeGearShiftMeasurement()
	}

	a.armGearShiftMeasurement(cur.surgeJerk, a.gearShiftIsDownshift())

	shift.jerkTrace = append(shift.jerkTrace, cur.surgeJerk)
	shift.accTrace = append(shift.accTrace, cur.surgeAcc)
	shift.clutchTrace = append(shift.clutchTrace, cur.clutchEng)
	shift.rpmTrace = append(shift.rpmTrace, cur.rpm)
	shift.speedTrace = append(shift.speedTrace, cur.speed)

	return shift
}

// extendOpenShifts appends the current frame to every shift still inside its trail
// window, returning the shifts that remain open.
func extendOpenShifts(shifts []probeShift, open []int, cur probeFrame) []int {
	still := open[:0]

	for _, idx := range open {
		shift := &shifts[idx]
		if len(shift.jerkTrace) > probeTrail {
			continue
		}

		shift.jerkTrace = append(shift.jerkTrace, cur.surgeJerk)
		shift.accTrace = append(shift.accTrace, cur.surgeAcc)
		shift.clutchTrace = append(shift.clutchTrace, cur.clutchEng)
		shift.rpmTrace = append(shift.rpmTrace, cur.rpm)
		shift.speedTrace = append(shift.speedTrace, cur.speed)

		still = append(still, idx)
	}

	return still
}

// finaliseProbeShift derives the peak statistics that decide whether the shipped
// 3-frame measurement window is long enough.
func finaliseProbeShift(shift *probeShift) {
	for idx, jerk := range shift.jerkTrace {
		if jerk > shift.peakWindowFull {
			shift.peakWindowFull = jerk
			shift.argmaxOffset = idx
		}

		if idx <= probeShippedWindow && jerk > shift.peakWindow3 {
			shift.peakWindow3 = jerk
		}
	}

	shift.resyncOffset = resyncOffset(shift, probeSyncTolerance)

	// The live measurement window closes at resyncOffset, so that is the peak the
	// effect would actually have learned from. When the trail never resyncs, the
	// window never closes either, so it falls back to the full-trail peak already
	// computed above.
	if shift.resyncOffset < 0 {
		shift.peakToResync = shift.peakWindowFull
		shift.argmaxToResync = shift.argmaxOffset

		return
	}

	for idx := 0; idx <= shift.resyncOffset && idx < len(shift.jerkTrace); idx++ {
		if shift.jerkTrace[idx] > shift.peakToResync {
			shift.peakToResync = shift.jerkTrace[idx]
			shift.argmaxToResync = idx
		}
	}
}

// probeSyncTolerance is the probe's own copy of gearShiftSyncTolerance, overridable
// via GEARSHIFT_PROBE_SYNC_TOL so a sweep can find the tolerance without touching the
// live constant, which is what actually ships.
var probeSyncTolerance = gearShiftSyncTolerance //nolint:gochecknoglobals // probe tuning knob, see applyProbeTuningOverrides

// resyncOffset replays the live re-sync criterion (gearShiftHasResynced) over a
// captured trace, so the probe can report the frame offset the measurement window
// would actually have closed at, without duplicating any state the live code carries
// only across ticks. It mirrors that function frame-for-frame:
//   - offsets before gearShiftMinMeasureFrames never touch the consecutive-frame
//     counter, matching the early return that skips it entirely;
//   - a frame counts as in-tolerance only when speed is positive and rpm/speed sits
//     within tol of syncTarget;
//   - re-sync is declared on the 2nd (gearShiftSyncFrames) consecutive in-tolerance
//     frame.
//
// Offset 0 is the shift frame itself; the live code cannot resync on it, so the
// replay starts at offset 1. Returns -1 when the shift has no usable target or the
// trail never satisfies the criterion.
func resyncOffset(shift *probeShift, tol float64) int {
	if shift.syncTarget <= 0 {
		return -1
	}

	syncFrames := 0

	for offset := 1; offset < len(shift.rpmTrace); offset++ {
		if offset < gearShiftMinMeasureFrames {
			continue
		}

		speed := shift.speedTrace[offset]

		inTolerance := speed > 0 &&
			math.Abs(shift.rpmTrace[offset]/speed-shift.syncTarget) <= tol*shift.syncTarget

		if !inTolerance {
			syncFrames = 0

			continue
		}

		syncFrames++

		if syncFrames >= gearShiftSyncFrames {
			return offset
		}
	}

	return -1
}

// reportProbe emits the per-shift table and the per-replay summary.
func reportProbe(t *testing.T, app *App, shifts []probeShift, format string) {
	t.Helper()

	t.Logf("vehicle=%d type=%s revLimit=%d format=%q floorGain=%.2fdB shifts=%d",
		app.vehicle.ID, app.vehicle.VehicleType, app.vehicle.RevLimit, format,
		app.transmissionGainMin, len(shifts))

	t.Logf("CSV seq,lap,from,to,dir,speed,throttle,brake,rpmBefore,ratioFrom,ratioTo," +
		"ratioStep,rpmPredicted,rpmMeasured12,peak3,peakFull,argmax,magnitude,jerkEWMA,resync,peakToResync,argmaxToResync")

	for _, shift := range shifts {
		t.Logf("CSV %d,%d,%d,%d,%s,%.1f,%.0f,%.0f,%.0f,%.3f,%.3f,%.3f,%.0f,%.0f,%.1f,%.1f,%d,%.4f,%.1f,%d,%.1f,%d",
			shift.seq, shift.lap, shift.from, shift.to, dirLabel(shift), shift.speed, shift.throttle, shift.brake,
			shift.rpmBefore, shift.ratioFrom, shift.ratioTo, shift.ratioStep(),
			shift.rpmBefore*shift.ratioStep(), traceAt(shift.rpmTrace, 12),
			shift.peakWindow3, shift.peakWindowFull, shift.argmaxOffset,
			shift.magnitudePlaye, shift.jerkEWMAAt,
			shift.resyncOffset, shift.peakToResync, shift.argmaxToResync)
	}

	summariseProbe(t, app, shifts)
}

func dirLabel(shift probeShift) string {
	if shift.down() {
		return "down"
	}

	return "up"
}

func traceAt(trace []float64, idx int) float64 {
	if idx >= len(trace) {
		if len(trace) == 0 {
			return 0
		}

		return trace[len(trace)-1]
	}

	return trace[idx]
}

// summariseProbe answers the decision points directly: does the 3-frame window
// find the peak, do the ratios predict the RPM step, and did the effect ever
// leave its floor?
func summariseProbe(t *testing.T, app *App, shifts []probeShift) {
	t.Helper()

	if len(shifts) == 0 {
		t.Log("no gear changes detected")

		return
	}

	stats := collectProbeStats(shifts, app)

	logWindowArgmaxStats(t, stats, len(shifts))
	logResyncStats(t, shifts)
	logRatioPredictionError(t, stats)
	logDrivelineStepPercentiles(t, stats)
	logMagnitudeStats(t, app, stats)

	// Does shift direction separate the learned character? If the two peak
	// populations overlap, splitting the EWMA buys nothing.
	dir := collectDirectionStats(shifts, app)

	logDirectionSplit(t, dir)

	// What the learner actually settled on, against what the traces say it should
	// have. A large gap means measurements are being truncated before their peak.
	logLearnedVsTarget(t, app, shifts, dir)

	// The learner deliberately warms up from the floor, so the opening shifts of a
	// session are quieter by design. Steady state is what the car actually feels like.
	logSteadyState(t, shifts, stats.mags)
}

// probeStats holds the per-replay quantities that every SUMMARY line except the
// direction split and steady-state ones are derived from.
type probeStats struct {
	argmaxes            []float64
	ratioErr            []float64
	mags                []float64
	withRatios          int
	downBraking         []float64
	windowMissFraction  float64
	stepValues          []float64
	downMags, upMags    []float64
	peakFull, peak3Mean float64
	count               float64
}

// collectProbeStats walks every shift once, gathering the raw quantities the
// SUMMARY lines are derived from.
func collectProbeStats(shifts []probeShift, app *App) probeStats {
	var stats probeStats

	for _, shift := range shifts {
		stats.argmaxes = append(stats.argmaxes, float64(shift.argmaxOffset))
		stats.mags = append(stats.mags, shift.magnitudePlaye)
		stats.peakFull += shift.peakWindowFull
		stats.peak3Mean += shift.peakWindow3

		if shift.peakWindowFull > 0 && shift.peakWindow3 < shift.peakWindowFull*0.8 {
			stats.windowMissFraction++
		}

		if step := shift.ratioStep(); step > 0 {
			stats.withRatios++

			stats.stepValues = append(stats.stepValues, math.Abs(step*step-1)*
				(shift.rpmBefore/math.Max(1, float64(app.vehicle.RevLimit))))

			if measured := traceAt(shift.rpmTrace, 12); measured > 0 && shift.rpmBefore > 0 {
				stats.ratioErr = append(stats.ratioErr, measured/(shift.rpmBefore*step)-1)
			}
		}

		if shift.down() {
			stats.downMags = append(stats.downMags, shift.magnitudePlaye)

			if shift.brake > 50 {
				stats.downBraking = append(stats.downBraking, shift.magnitudePlaye)
			}
		} else {
			stats.upMags = append(stats.upMags, shift.magnitudePlaye)
		}
	}

	stats.count = float64(len(shifts))
	stats.windowMissFraction /= stats.count

	return stats
}

func logWindowArgmaxStats(t *testing.T, stats probeStats, shiftCount int) {
	t.Helper()

	t.Logf("SUMMARY ratiosPopulated=%d/%d argmax p50=%.0f p95=%.0f | peak3 mean=%.1f peakFull mean=%.1f | window miss (peak3 < 80%% of full) = %.0f%%",
		stats.withRatios, shiftCount, percentile(stats.argmaxes, 0.5), percentile(stats.argmaxes, 0.95),
		stats.peak3Mean/stats.count, stats.peakFull/stats.count, stats.windowMissFraction*100)
}

func logRatioPredictionError(t *testing.T, stats probeStats) {
	t.Helper()

	if len(stats.ratioErr) == 0 {
		return
	}

	t.Logf("SUMMARY ratio-predicted RPM vs measured@+12: median error %.1f%% p95 %.1f%%",
		percentile(absAll(stats.ratioErr), 0.5)*100, percentile(absAll(stats.ratioErr), 0.95)*100)
}

// logResyncStats reports where the re-sync criterion actually closes the window,
// split by direction since downshifts (added engine braking, often under threshold
// braking) and upshifts (a clean torque cut/re-apply) settle at very different
// rates. The two questions this answers directly: how far out does the window need
// to stay open, and how often does closing it early throw away the true peak.
func logResyncStats(t *testing.T, shifts []probeShift) {
	t.Helper()

	logResyncStatsForDirection(t, "up", shiftsInDirection(shifts, false))
	logResyncStatsForDirection(t, "down", shiftsInDirection(shifts, true))
}

func shiftsInDirection(shifts []probeShift, down bool) []probeShift {
	out := make([]probeShift, 0, len(shifts))

	for _, shift := range shifts {
		if shift.down() == down {
			out = append(out, shift)
		}
	}

	return out
}

func logResyncStatsForDirection(t *testing.T, label string, shifts []probeShift) {
	t.Helper()

	if len(shifts) == 0 {
		return
	}

	var (
		resynced         []float64
		neverResynced    int
		afterResync      int
		fullOverToResync []float64
	)

	for _, shift := range shifts {
		if shift.resyncOffset < 0 {
			neverResynced++

			continue
		}

		resynced = append(resynced, float64(shift.resyncOffset))

		if shift.argmaxOffset > shift.resyncOffset {
			afterResync++

			if shift.peakToResync > 0 {
				fullOverToResync = append(fullOverToResync, shift.peakWindowFull/shift.peakToResync)
			}
		}
	}

	pct := 0.0
	if len(shifts) > 0 {
		pct = float64(afterResync) / float64(len(shifts)) * 100
	}

	t.Logf("SUMMARY resync %s n=%d resyncOffset p50=%.0f p95=%.0f neverResynced=%d | argmax-after-resync=%d (%.0f%%) peakFull/peakToResync p50=%.2f",
		label, len(shifts), percentile(resynced, 0.5), percentile(resynced, 0.95), neverResynced,
		afterResync, pct, percentile(fullOverToResync, 0.5))
}

func logDrivelineStepPercentiles(t *testing.T, stats probeStats) {
	t.Helper()

	if len(stats.stepValues) == 0 {
		return
	}

	t.Logf("SUMMARY drivelineStep p50=%.3f p95=%.3f max=%.3f",
		percentile(stats.stepValues, 0.5), percentile(stats.stepValues, 0.95), percentile(stats.stepValues, 1.0))
}

func logMagnitudeStats(t *testing.T, app *App, stats probeStats) {
	t.Helper()

	floor := math.Pow(10, app.transmissionGainMin/10)
	atFloor := 0

	for _, m := range stats.mags {
		if m <= floor*1.001 {
			atFloor++
		}
	}

	t.Logf("SUMMARY magnitude floor=%.4f atFloor=%d/%d (%.0f%%) min=%.4f p50=%.4f max=%.4f span=%.1fdB",
		floor, atFloor, len(stats.mags), float64(atFloor)/stats.count*100,
		percentile(stats.mags, 0.0), percentile(stats.mags, 0.5), percentile(stats.mags, 1.0),
		spanDB(stats.mags))

	t.Logf("SUMMARY up n=%d span=%.1fdB | down n=%d span=%.1fdB | down under >50%% brake n=%d span=%.1fdB",
		len(stats.upMags), spanDB(stats.upMags), len(stats.downMags), spanDB(stats.downMags),
		len(stats.downBraking), spanDB(stats.downBraking))

	t.Logf("SUMMARY histogram %s", histogram(stats.mags, floor))
}

// directionStats splits the peak jerk and driveline step by shift direction, so
// the learned character can be checked against each direction separately.
type directionStats struct {
	upPeak, downPeak, upStep, downStep []float64
}

func collectDirectionStats(shifts []probeShift, app *App) directionStats {
	var dir directionStats

	for _, shift := range shifts {
		step := math.Abs(shift.ratioStep()*shift.ratioStep()-1) * (shift.rpmBefore / math.Max(1, float64(app.vehicle.RevLimit)))

		if shift.down() {
			dir.downPeak = append(dir.downPeak, shift.peakWindowFull)
			dir.downStep = append(dir.downStep, step)
		} else {
			dir.upPeak = append(dir.upPeak, shift.peakWindowFull)
			dir.upStep = append(dir.upStep, step)
		}
	}

	return dir
}

func logDirectionSplit(t *testing.T, dir directionStats) {
	t.Helper()

	t.Logf("SUMMARY direction peakFull up p50=%.1f down p50=%.1f | drivelineStep up p50=%.3f down p50=%.3f",
		percentile(dir.upPeak, 0.5), percentile(dir.downPeak, 0.5),
		percentile(dir.upStep, 0.5), percentile(dir.downStep, 0.5))

	// How skewed the window-peak population is. A learned estimate far above the
	// median means a few outlier windows (brake or suspension events caught inside a
	// long window) are dragging the mean-tracking EWMA, not that the gearbox is harsh.
	t.Logf("SUMMARY peakskew down p50=%.1f p75=%.1f p90=%.1f max=%.1f | p90/p50=%.2f",
		percentile(dir.downPeak, 0.5), percentile(dir.downPeak, 0.75),
		percentile(dir.downPeak, 0.90), percentile(dir.downPeak, 1.0),
		percentile(dir.downPeak, 0.90)/math.Max(1, percentile(dir.downPeak, 0.5)))
}

func logLearnedVsTarget(t *testing.T, app *App, shifts []probeShift, dir directionStats) {
	t.Helper()

	t.Logf("SUMMARY learned characterUp=%.1f (target %.1f) characterDown=%.1f (target %.1f) max=%.0f samples=%d shifts=%d",
		app.gearShift.characterUp, percentile(dir.upPeak, 0.5),
		app.gearShift.characterDown, percentile(dir.downPeak, 0.5),
		gearShiftCharacterMax,
		app.gearShift.samplesUp+app.gearShift.samplesDown, len(shifts))

	logWarmUpCost(t, app, shifts)
}

// logWarmUpCost reports how long the learned character takes to arrive, and what it
// costs while it has not. The estimate is seeded at the gain floor and only rises, so
// every session opens below the vehicle's true character until enough shifts of each
// direction have been seen — and the per-direction split doubles the number of shifts
// needed, since each direction warms independently.
func logWarmUpCost(t *testing.T, app *App, shifts []probeShift) {
	t.Helper()

	if len(shifts) < 4 {
		return
	}

	finalUp := app.gearShift.characterUp
	finalDown := app.gearShift.characterDown

	var upSeen, downSeen, upWarm, downWarm int

	for _, shift := range shifts {
		if shift.down() {
			downSeen++

			if shift.jerkEWMAAt < 0.9*finalDown {
				downWarm = downSeen
			}

			continue
		}

		upSeen++

		if shift.jerkEWMAAt < 0.9*finalUp {
			upWarm = upSeen
		}
	}

	// Amplitude lost on the shifts played before the estimate arrived.
	quarter := len(shifts) / 4
	early := percentile(magnitudesOf(shifts[:quarter]), 0.5)
	late := percentile(magnitudesOf(shifts[len(shifts)/2:]), 0.5)

	deficit := 0.0
	if early > 0 {
		deficit = 20 * math.Log10(late/early)
	}

	t.Logf("SUMMARY warmup shifts-to-90%%: up=%d/%d down=%d/%d | first-quarter median %.4f vs steady %.4f (%.1f dB quiet)",
		upWarm, upSeen, downWarm, downSeen, early, late, deficit)
}

func magnitudesOf(shifts []probeShift) []float64 {
	out := make([]float64, 0, len(shifts))
	for _, shift := range shifts {
		out = append(out, shift.magnitudePlaye)
	}

	return out
}

func logSteadyState(t *testing.T, shifts []probeShift, mags []float64) {
	t.Helper()

	steady := mags[len(mags)/2:]

	var steadyUp, steadyDown []float64

	for _, shift := range shifts[len(shifts)/2:] {
		if shift.down() {
			steadyDown = append(steadyDown, shift.magnitudePlaye)
		} else {
			steadyUp = append(steadyUp, shift.magnitudePlaye)
		}
	}

	t.Logf("SUMMARY steady (2nd half) p50=%.4f span=%.1fdB | up p50=%.4f | down p50=%.4f",
		percentile(steady, 0.5), spanDB(steady),
		percentile(steadyUp, 0.5), percentile(steadyDown, 0.5))
}

func absAll(v []float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = math.Abs(x)
	}

	return out
}

func percentile(v []float64, fraction float64) float64 {
	if len(v) == 0 {
		return 0
	}

	s := append([]float64(nil), v...)
	sort.Float64s(s)

	idx := int(fraction * float64(len(s)-1))

	return s[idx]
}

// spanDB is the amplitude range covered, which is the quantity the complaint is
// really about: how much a shift can vary from the quietest to the loudest.
func spanDB(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}

	lo, hi := percentile(v, 0.0), percentile(v, 1.0)
	if lo <= 0 {
		return 0
	}

	return 20 * math.Log10(hi/lo)
}

func histogram(v []float64, floor float64) string {
	const buckets = 10

	counts := make([]int, buckets)

	for _, x := range v {
		frac := (x - floor) / math.Max(1e-9, 1-floor)
		b := int(frac * float64(buckets))
		b = max(0, min(buckets-1, b))
		counts[b]++
	}

	var builder strings.Builder

	for i, c := range counts {
		fmt.Fprintf(&builder, "[%.1f-%.1f]=%d ", float64(i)/buckets, float64(i+1)/buckets, c)
	}

	return builder.String()
}
