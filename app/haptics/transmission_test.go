package haptics //nolint:testpackage // white-box testing

import (
	"io"
	"math"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
)

// SetupTest across these suites deliberately builds a plain, unopinionated App: it
// pins no gear-shift tuning values of its own. Each test below sets, with a literal
// inline, only the config values its own behaviour or assertions actually depend
// on, so a retuned default cannot silently change what a test asserts, and no test
// can hide a dependency on config.New's shipped defaults behind a shared constant.

// floorClearingCharacter is the smallest learned jerk character whose driveline
// magnitude just reaches the vehicle's gain floor, with the event term at zero (the
// case TestWarmUpOnlyRises exercises, since it leaves ratios unset). It inverts
// gearShiftMagnitudeFromDriveline's character*(1-stepBlend) term:
//
//	floor == ((character*gearShiftReferenceFrames/characterMax)*(1-stepBlend))^curve
func floorClearingCharacter(gainMinDB, characterMax, stepBlend, jerkCurveThousandths float64) float64 {
	curve := jerkCurveThousandths / 1000
	drive := math.Pow(signal.GainToPowerRatio(gainMinDB), 1/curve)
	characterNorm := drive / (1 - stepBlend)

	return characterNorm * characterMax / gearShiftReferenceFrames
}

// fakeTelemetry is a TelemetrySource under the test's control. The generator reads
// telemetry only through this interface, so a suite can drive the gear-ratio and rpm
// paths with no telemetry client. Those paths could not be reached at all while the
// generator read a *gttelemetry.Client field directly.
type fakeTelemetry struct {
	rpm      float32
	throttle float32
	ratios   []float32
}

func (f *fakeTelemetry) EngineRPM() float32 { return f.rpm }

func (f *fakeTelemetry) ThrottleOutputPercent() float32 { return f.throttle }

func (f *fakeTelemetry) Transmission() gttelemetry.Transmission {
	return gttelemetry.Transmission{GearRatios: f.ratios}
}

// newTransmissionFixture builds a generator wired to the caller's kinematics state,
// with no synth (refreshPulse is a no-op without one) and a fake telemetry source.
//
// It sets the gain floor directly rather than through SetVehicle, because these suites
// assert against an unseeded profile: SetVehicle seeds, and a seeded profile would
// mask what the tests measure.
func newTransmissionFixture(
	cfg *config.Config, kin *kinematics.State, tel *fakeTelemetry, revLimit uint16,
) *TransmissionGenerator {
	*kin = kinematics.NewKinematicsState()

	gen := NewTransmissionGenerator(cfg, nil, kin,
		func() TelemetrySource { return tel }, zerolog.New(io.Discard))

	gen.gainMin = cfg.GetSynthTransmissionGainMinRace()
	gen.revLimit = revLimit

	return gen
}

type GearShiftProfileTestSuite struct {
	suite.Suite

	gen *TransmissionGenerator
	kin kinematics.State
	tel fakeTelemetry
}

func TestGearShiftProfileTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(GearShiftProfileTestSuite))
}

func (suite *GearShiftProfileTestSuite) SetupTest() {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	// This suite covers the shared learning mechanism (peak collection, median,
	// settling, launch exclusion) independently of the driveline magnitude mapping,
	// which has its own suite. Neither the config nor the gain floor is tuned here:
	// each test that depends on a specific value sets it explicitly.
	suite.newGenerator(cfg, 0)
}

// The seed must map to exactly the gain floor, which is what guarantees the first
// shift of a session plays at the configured minimum and can only rise from there.
// The round trip through the played magnitude is covered by
// GearShiftDrivelineTestSuite.TestMissingRatiosFallBackToCharacter and
// GearShiftImpulseTestSuite's floor tests, which exercise the actual driveline
// mapping this seed feeds.
func (suite *GearShiftProfileTestSuite) TestSeedMapsToTheGainFloor() {
	suite.gen.cfg.SetHapticsTransmissionJerkCurve(750)
	suite.gen.gainMin = -4.50
	suite.gen.seedProfile()

	suite.InDelta(suite.floorSeed(), suite.gen.profile.characterUp, 0.001)
	suite.Zero(suite.gen.profile.samplesUp)
	suite.False(suite.gen.profile.measuring)
}

// A quieter floor must seed a quieter starting point, since the seed is derived
// from it rather than from a fixed per-vehicle-type constant.
func (suite *GearShiftProfileTestSuite) TestSeedTracksTheFloor() {
	suite.gen.gainMin = -4.50 // race floor
	suite.gen.seedProfile()
	raceSeed := suite.gen.profile.characterUp

	suite.gen.gainMin = -6.00 // street floor
	suite.gen.seedProfile()

	suite.Less(suite.gen.profile.characterUp, raceSeed,
		"a lower street floor should seed below the race floor")
}

// The point of seeding at the floor: warm-up only ever gets louder. A thump that is
// quieter than it should be goes unnoticed; one that is louder startles. Ratios are
// left unset, so the driveline event term is zero and the played magnitude tracks
// the learned character alone, exactly as it did through the jerk mapping this test
// used to exercise.
func (suite *GearShiftProfileTestSuite) TestWarmUpOnlyRises() {
	suite.gen.cfg.SetHapticsTransmissionStepBlend(0.5)
	suite.gen.cfg.SetHapticsTransmissionJerkCurve(750)
	suite.gen.gainMin = -4.50
	suite.gen.seedProfile()

	const warmUpShifts = 10

	// With ratios unset the driveline event term is zero, so the played magnitude
	// is (character*(1-stepBlend))^curve alone: see floorClearingCharacter for the
	// derivation. A peak double that threshold gives a car harsh enough to rise
	// unambiguously above the floor once warmed up, rather than only marginally so.
	warmUpPeak := 2 * floorClearingCharacter(-4.50, 1800, 0.5, 750)

	levels := make([]float64, 0, warmUpShifts+1)
	levels = append(levels, suite.gen.magnitudeFromDriveline())

	for range warmUpShifts {
		suite.measureShift(warmUpPeak, warmUpPeak, warmUpPeak, warmUpPeak)
		levels = append(levels, suite.gen.magnitudeFromDriveline())
	}

	for i := 1; i < len(levels); i++ {
		suite.GreaterOrEqual(levels[i], levels[i-1],
			"level must never drop during warm-up (step %d)", i)
	}

	suite.Greater(levels[len(levels)-1], levels[0],
		"a car harsher than its floor should end up louder than it started")
}

// A vehicle gentler than its own floor stays pinned there rather than drifting
// audibly downward.
func (suite *GearShiftProfileTestSuite) TestVehicleBelowFloorStaysAtFloor() {
	suite.gen.gainMin = -4.50
	suite.gen.seedProfile()

	floor := signal.GainToPowerRatio(suite.gen.gainMin)

	for range 10 {
		suite.measureShift(5, 5, 5, 5)
		suite.InDelta(floor, suite.gen.magnitudeFromDriveline(), 0.001)
	}
}

func (suite *GearShiftProfileTestSuite) TestMeasurementTakesWindowPeak() {
	suite.gen.seedProfile()

	// The largest jerk of a fast shift is the torque re-application, which lands
	// at the end of the window rather than at the leading edge.
	suite.measureShift(50, 80, 120, 240)

	suite.Equal(1, suite.gen.profile.samplesUp)
	suite.False(suite.gen.profile.measuring)
	suite.InDelta(suite.blended(240), suite.gen.profile.characterUp, 0.001)
}

func (suite *GearShiftProfileTestSuite) TestMeasurementIgnoresJerkBeyondWindow() {
	suite.gen.seedProfile()

	suite.measureShift(30, 40, 35, 30)
	suite.Require().False(suite.gen.profile.measuring)

	settled := suite.gen.profile.characterUp
	suite.InDelta(suite.blended(40), settled, 0.001)

	// A large jerk well after the shift (a kerb strike, say) must not be folded in.
	suite.setSurgeJerk(900)
	suite.gen.TickMeasurement()

	suite.InDelta(settled, suite.gen.profile.characterUp, 0.001)
}

// The estimate must actually arrive at the vehicle's character within its sample
// budget, rather than asymptotically approaching a value it never reaches.
func (suite *GearShiftProfileTestSuite) TestReachesCharacterWithinSampleBudget() {
	suite.gen.seedProfile()

	const target = 180.0

	for range gearShiftLearningSamples {
		suite.measureShift(target, target, target, target)
	}

	suite.InDelta(target, suite.gen.profile.characterUp, target*0.1,
		"the character should be within 10%% of true once the samples are in")
	suite.True(suite.gen.profile.settled(false))
}

// Shift harshness is a fixed property of a gearbox, so once the samples are in the
// character is frozen: no later shift, however atypical, may move it. This is what
// stops the effect drifting over a long stint.
func (suite *GearShiftProfileTestSuite) TestSettledCharacterIsFrozen() {
	suite.gen.seedProfile()

	for range gearShiftLearningSamples {
		suite.measureShift(40, 40, 40, 40)
	}

	settled := suite.gen.profile.characterUp
	suite.Require().True(suite.gen.profile.settled(false))

	for range 10 {
		suite.measureShift(200, 200, 200, 200)
	}

	suite.InDelta(settled, suite.gen.profile.characterUp, 0.001,
		"a settled character must not drift")
	suite.Equal(gearShiftLearningSamples, suite.gen.profile.samplesUp,
		"no further samples should be taken once settled")
}

// Freezing is per direction: a car may settle its upshifts long before its
// downshifts if the driver has been using one more than the other.
func (suite *GearShiftProfileTestSuite) TestDirectionsSettleIndependently() {
	suite.gen.seedProfile()

	for range gearShiftLearningSamples {
		suite.measureShiftDirection(false, 200, 200)
	}

	suite.True(suite.gen.profile.settled(false))
	suite.False(suite.gen.profile.settled(true),
		"downshifts must still be learning")

	before := suite.gen.profile.characterDown

	suite.measureShiftDirection(true, 200, 200)
	suite.Greater(suite.gen.profile.characterDown, before,
		"an unsettled direction must still be learning")
}

func (suite *GearShiftProfileTestSuite) TestPreemptedMeasurementIsFoldedIn() {
	suite.gen.seedProfile()

	// Arm a measurement and advance it partway, as a rapid multi-downshift would.
	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps + 1
	suite.setSurgeJerk(100)
	suite.gen.armMeasurement(suite.kin.GetSurgeJerk(), false)

	suite.setSurgeJerk(180)
	suite.gen.TickMeasurement()
	suite.Require().True(suite.gen.profile.measuring)

	// A second shift arrives before the window closes.
	suite.gen.completeMeasurement()

	suite.Equal(1, suite.gen.profile.samplesUp)
	suite.False(suite.gen.profile.measuring)
	suite.InDelta(suite.blended(180), suite.gen.profile.characterUp, 0.001,
		"partial measurement should still contribute its peak")
}

func (suite *GearShiftProfileTestSuite) TestTickIsNoOpWhenNotMeasuring() {
	suite.gen.seedProfile()

	before := suite.gen.profile.characterUp

	suite.setSurgeJerk(500)
	suite.gen.TickMeasurement()

	suite.Zero(suite.gen.profile.samplesUp)
	suite.InDelta(before, suite.gen.profile.characterUp, 0.001)
}

// A standing start applies full drive torque to a stationary car, producing a jerk
// several times any gear change. Folding it in would set the estimate far too high
// and pin the effect at full scale for the following shifts.
func (suite *GearShiftProfileTestSuite) TestLaunchIsExcludedFromLearning() {
	suite.gen.seedProfile()

	before := suite.gen.profile.characterUp

	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps - 1
	suite.setSurgeJerk(515)

	suite.gen.armMeasurement(suite.kin.GetSurgeJerk(), false)

	suite.False(suite.gen.profile.measuring, "a launch must not arm a measurement")

	// Frames after the launch must not be folded in either.
	suite.setSurgeJerk(515)
	suite.gen.TickMeasurement()

	suite.Zero(suite.gen.profile.samplesUp)
	suite.InDelta(before, suite.gen.profile.characterUp, 0.001,
		"the estimate should still be the untouched seed")
}

func (suite *GearShiftProfileTestSuite) TestRollingShiftIsLearnedFrom() {
	suite.gen.seedProfile()

	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps + 1
	suite.setSurgeJerk(180)

	suite.gen.armMeasurement(suite.kin.GetSurgeJerk(), false)

	suite.True(suite.gen.profile.measuring, "a rolling shift must arm a measurement")

	for range 32 {
		suite.gen.TickMeasurement()
	}

	suite.Equal(1, suite.gen.profile.samplesUp)
	suite.InDelta(suite.blended(180), suite.gen.profile.characterUp, 0.001)
}

func (suite *GearShiftProfileTestSuite) TestEstimateStaysFiniteWithoutMeasurements() {
	suite.gen.seedProfile()

	suite.False(math.IsNaN(suite.gen.profile.characterUp))
	suite.False(math.IsInf(suite.gen.profile.characterUp, 0))
	suite.Positive(suite.gen.profile.characterUp)
}

// A window long enough to catch a clutched gearbox's late torque re-application is
// also long enough to catch brake and suspension events that are not the gearbox.
// A minority of such windows must not move the character at all, which is what
// taking the median rather than the mean buys.
func (suite *GearShiftProfileTestSuite) TestOutlierWindowsCannotDefineTheVehicle() {
	suite.gen.seedProfile()

	// A Supra RZ profile: a genuine character around 70, with two of its eight
	// windows reaching the 350-445 m/s^3 its threshold braking produces.
	peaks := []float64{68, 445, 72, 65, 380, 74, 66, 70}
	for _, peak := range peaks {
		suite.measureShift(peak, peak)
	}

	suite.InDelta(70, suite.gen.profile.characterUp, 10.0,
		"a quarter of the windows being outliers must not move the median")

	suite.Less(suite.gen.profile.characterUp, 100.0,
		"outlier windows must not drag the character to race-car levels")
}

// The median must not blunt a vehicle that genuinely is harsh: when the peaks really
// are large, the character follows them.
func (suite *GearShiftProfileTestSuite) TestGenuinelyHarshVehicleIsLearned() {
	suite.gen.seedProfile()

	for range gearShiftLearningSamples {
		suite.measureShift(205, 205)
	}

	suite.InDelta(205, suite.gen.profile.characterUp, 10.0,
		"a sustained genuine character should be reached in full")
}

// floorSeed is the jerk the seed should resolve to: the value mapping to exactly
// the configured gain floor.
func (suite *GearShiftProfileTestSuite) floorSeed() float64 {
	curve := suite.gen.cfg.GetHapticsTransmissionJerkCurve() / 1000
	characterMax := gearShiftCharacterMax

	seed := characterMax * math.Pow(signal.GainToPowerRatio(suite.gen.gainMin), 1/curve)

	return seed / gearShiftReferenceFrames
}

// blended is the character after collecting the given peaks, i.e. their median
// together with the floor seed.
func (suite *GearShiftProfileTestSuite) blended(peaks ...float64) float64 {
	return medianCharacter(peaks, suite.floorSeed())
}

// setSurgeJerk drives the value GetSurgeJerk will report on the next read. The
// haptic path only ever reads the magnitude, so the sign here is irrelevant.
func (suite *GearShiftProfileTestSuite) setSurgeJerk(jerk float64) {
	suite.kin.Current.SurgeJerk = jerk
}

// measureShift runs one complete shift measurement: the leading edge plus the
// per-frame ticks that close the window.
// measureShift drives one complete measurement window. The supplied jerks fill the
// leading frames and the remainder of the window is padded with zero, so the window
// always closes regardless of how it is configured.
func (suite *GearShiftProfileTestSuite) measureShift(jerks ...float64) {
	suite.measureShiftDirection(false, jerks...)
}

func (suite *GearShiftProfileTestSuite) measureShiftDirection(down bool, jerks ...float64) {
	suite.Require().NotEmpty(jerks)

	suite.setSurgeJerk(jerks[0])

	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps + 1
	suite.gen.armMeasurement(suite.kin.GetSurgeJerk(), down)

	measureFrames := gearShiftMaxMeasureFrames

	for i := 1; i <= measureFrames; i++ {
		jerk := 0.0
		if i < len(jerks) {
			jerk = jerks[i]
		}

		suite.setSurgeJerk(jerk)
		suite.gen.TickMeasurement()
	}
}

// GearShiftDrivelineTestSuite covers the driveline source: the per-shift engine-braking
// term and the per-direction learned character.
type GearShiftDrivelineTestSuite struct {
	suite.Suite

	gen *TransmissionGenerator
	kin kinematics.State
	tel fakeTelemetry
}

func TestGearShiftDrivelineTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(GearShiftDrivelineTestSuite))
}

func (suite *GearShiftDrivelineTestSuite) SetupTest() {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	suite.newGenerator(cfg, 9000)
}

// The event term is what gives a shift its per-gear identity: a big ratio jump must
// read as a bigger event than a small one at the same engine speed.
func (suite *GearShiftDrivelineTestSuite) TestBigRatioJumpBeatsSmallOne() {
	suite.shift(3, 2, 1.35, 1.85, 7000)
	big := suite.gen.drivelineStep()

	suite.shift(6, 5, 0.85, 0.95, 7000)
	small := suite.gen.drivelineStep()

	suite.Greater(big, small, "a wider ratio step should be a larger driveline event")
}

// The same gear pair near the limiter changes far more engine braking than it does
// off idle, so revs must scale the event.
func (suite *GearShiftDrivelineTestSuite) TestHighRevsBeatLowRevs() {
	suite.shift(3, 2, 1.35, 1.85, 8100)
	high := suite.gen.drivelineStep()

	suite.shift(3, 2, 1.35, 1.85, 3600)
	low := suite.gen.drivelineStep()

	suite.Greater(high, low, "the same shift near the limiter should be a larger event")
	suite.InDelta(high*(3600.0/8100.0), low, 0.001, "the event should scale linearly with revs")
}

// The whole point of deriving the event from ratios: a downshift into a hairpin must
// out-magnitude a top-gear upshift, with no look-ahead.
func (suite *GearShiftDrivelineTestSuite) TestDownshiftOutweighsHighGearUpshift() {
	suite.gen.seedProfile()

	suite.shift(3, 2, 1.35, 1.85, 8100)
	downshift := suite.gen.magnitudeFromDriveline()

	suite.shift(5, 6, 0.95, 0.85, 5000)
	upshift := suite.gen.magnitudeFromDriveline()

	suite.Greater(downshift, upshift)
}

// Reverse (gear 0) and neutral (gear 15) have no usable ratio. They must neither
// panic nor invent an event, falling back to the learned character alone.
func (suite *GearShiftDrivelineTestSuite) TestReverseAndNeutralYieldNoEvent() {
	for _, gear := range []int{0, 15} {
		suite.shift(1, gear, 2.90, 0, 2000)
		suite.Zero(suite.gen.drivelineStep())

		suite.NotPanics(func() { _ = suite.gen.magnitudeFromDriveline() })
		suite.False(suite.gen.isDownshift(),
			"gear %d is a sentinel, not a lower gear", gear)
	}
}

// A vehicle whose telemetry carries no gear ratios must still produce a usable
// shift, driven by the learned character alone.
func (suite *GearShiftDrivelineTestSuite) TestMissingRatiosFallBackToCharacter() {
	suite.gen.seedProfile()

	suite.shift(3, 2, 0, 0, 7000)

	suite.Zero(suite.gen.drivelineStep())
	suite.InDelta(signal.GainToPowerRatio(suite.gen.gainMin),
		suite.gen.magnitudeFromDriveline(), 0.001)
}

// Upshift and downshift character are learned independently, which is what lets a
// gated box be gentle upward and violent downward at the same time.
func (suite *GearShiftDrivelineTestSuite) TestDirectionsLearnIndependently() {
	suite.gen.seedProfile()

	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps + 1

	measure := func(down bool, peak float64) {
		suite.kin.Current.SurgeJerk = peak
		suite.gen.armMeasurement(peak, down)

		for range 32 {
			suite.kin.Current.SurgeJerk = 0
			suite.gen.TickMeasurement()
		}
	}

	// An R92CP-like profile: gentle clutched upshifts, violent downshifts.
	for range 20 {
		measure(false, 72)
		measure(true, 205)
	}

	suite.Greater(suite.gen.profile.characterDown, suite.gen.profile.characterUp*2,
		"a gated box's downshift character must not be averaged away by its upshifts")

	suite.shift(3, 2, 1.35, 1.85, 7000)
	down := suite.gen.magnitudeFromDriveline()

	suite.shift(2, 3, 1.85, 1.35, 7000)
	up := suite.gen.magnitudeFromDriveline()

	suite.Greater(down, up, "the learned asymmetry must reach the played magnitude")
}

// The magnitude is relative to the transmission channel, so trimming the channel
// must not also move the effect: PlayEffect applies the channel gain itself.
func (suite *GearShiftDrivelineTestSuite) TestMagnitudeIsIndependentOfChannelGain() {
	suite.gen.seedProfile()

	suite.shift(3, 2, 1.35, 1.85, 7000)
	atUnityGain := suite.gen.magnitudeFromDriveline()

	suite.gen.cfg.SetSynthTransmissionGain(-6.0)
	suite.gen.SetVehicle(vehicle.Characteristics{VehicleType: vehicle.TypeRace, RevLimit: 9000})
	suite.shift(3, 2, 1.35, 1.85, 7000)

	suite.InDelta(atUnityGain, suite.gen.magnitudeFromDriveline(), 0.001,
		"channel trim must be applied once, by the mixer, not folded in here")
}

// Whatever the inputs, the played magnitude stays inside the vehicle's window.
func (suite *GearShiftDrivelineTestSuite) TestMagnitudeStaysWithinTheWindow() {
	suite.gen.seedProfile()

	floor := signal.GainToPowerRatio(suite.gen.gainMin)

	suite.gen.profile.characterDown = 100000
	suite.shift(6, 1, 0.85, 3.50, 9000)

	magnitude := suite.gen.magnitudeFromDriveline()
	suite.LessOrEqual(magnitude, 1.0)
	suite.GreaterOrEqual(magnitude, floor)

	suite.gen.profile.characterDown = 0
	suite.shift(3, 2, 1.35, 1.36, 100)

	suite.GreaterOrEqual(suite.gen.magnitudeFromDriveline(), floor)
}

// GearShiftResyncWindowTestSuite covers the measurement window's re-engagement
// terminator: the window now ends when the driveline re-syncs to the post-shift
// ratio, plus a hold-off, rather than always running to the configured frame cap.
type GearShiftResyncWindowTestSuite struct {
	suite.Suite

	gen *TransmissionGenerator
	kin kinematics.State
	tel fakeTelemetry
}

func TestGearShiftResyncWindowTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(GearShiftResyncWindowTestSuite))
}

func (suite *GearShiftResyncWindowTestSuite) SetupTest() {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	suite.newGenerator(cfg, 0)
}

// TestWindowClosesOnReEngagement checks the two halves of the terminator together:
// the hold-off must still be running the instant re-sync is declared (otherwise
// the torque re-application that follows synchronisation is never measured), and
// it must not run any longer than gearShiftResyncHoldFrames beyond that (otherwise
// re-sync bought nothing over the old fixed-length window).
func (suite *GearShiftResyncWindowTestSuite) TestWindowClosesOnReEngagement() {
	suite.armAt(false, 2.0, 1.5, 6000, 30)
	target := suite.inSync()

	// Out-of-tolerance frames before the minimum has elapsed; the re-sync test is
	// not even consulted yet, so these must not close the window either way.
	for range gearShiftMinMeasureFrames {
		suite.tickAt(50, target*2, 30)
	}

	suite.Require().True(suite.gen.profile.measuring)
	suite.Require().False(suite.gen.profile.resynced)

	suite.tickAt(50, target, 30) // in tolerance, syncFrames=1: transient, not yet re-synced
	suite.True(suite.gen.profile.measuring)
	suite.False(suite.gen.profile.resynced)

	suite.tickAt(50, target, 30) // second consecutive in-tolerance frame: re-synced
	suite.True(suite.gen.profile.measuring,
		"the hold-off must still be running immediately after re-sync is declared")
	suite.True(suite.gen.profile.resynced)
	suite.Equal(gearShiftResyncHoldFrames, suite.gen.profile.framesLeft,
		"re-sync should shorten the window to exactly the hold-off, not end it outright")

	for i := range gearShiftResyncHoldFrames - 1 {
		suite.tickAt(50, target, 30)
		suite.True(suite.gen.profile.measuring, "hold-off frame %d should not close early", i)
	}

	suite.tickAt(50, target, 30)
	suite.False(suite.gen.profile.measuring,
		"the window should complete exactly at the hold-off boundary")

	suite.Less(suite.gen.profile.framesElapsed, 32,
		"re-engagement should close the window well before the frame cap")
}

// TestJerkAfterTheHoldOffIsExcluded is the regression this window design exists to
// prevent: on a Porsche 963 a single large brake-event jerk landing after
// synchronisation was picked up as the shift's peak, because the old window ran to
// a fixed frame count regardless of when the driveline actually re-engaged.
func (suite *GearShiftResyncWindowTestSuite) TestJerkAfterTheHoldOffIsExcluded() {
	suite.gen.seedProfile()

	suite.armAt(true, 2.0, 2.8, 6000, 30)
	target := suite.inSync()

	suite.tickAt(120, target*3, 30) // the shift's genuine peak, out of tolerance

	for range gearShiftMinMeasureFrames - 1 {
		suite.tickAt(20, target*3, 30) // still out of tolerance, still below the minimum
	}

	suite.tickAt(20, target, 30) // in tolerance, syncFrames=1
	suite.tickAt(20, target, 30) // in tolerance, syncFrames=2 -> resynced
	suite.Require().True(suite.gen.profile.resynced)

	// Run the hold-off out to completion.
	for suite.gen.profile.measuring {
		suite.tickAt(20, target, 30)
	}

	suite.Require().False(suite.gen.profile.measuring)

	recorded := suite.gen.profile.character(true)
	suite.InDelta(suite.blended(120), recorded, 0.001,
		"the recorded character should reflect the genuine peak, not a later spike")

	// A brake or kerb event arriving after the window has closed must not be
	// folded in: tickGearShiftMeasurement is a no-op once measuring is false.
	suite.tickAt(800, target, 30)
	suite.InDelta(recorded, suite.gen.profile.character(true), 0.001,
		"jerk after the hold-off must not move the recorded character")
}

// TestJerkWithinTheHoldOffIsCaptured is the other side of the regression test
// above: a clutched gearbox reaches its peak as the driver completes the pedal
// release, which follows synchronisation rather than accompanying it, so a large
// jerk arriving inside the hold-off must still count toward the character.
func (suite *GearShiftResyncWindowTestSuite) TestJerkWithinTheHoldOffIsCaptured() {
	suite.gen.seedProfile()

	suite.armAt(true, 2.0, 2.8, 6000, 30)
	target := suite.inSync()

	for range gearShiftMinMeasureFrames - 1 {
		suite.tickAt(20, target*3, 30) // out of tolerance, below the minimum
	}

	suite.tickAt(20, target, 30) // in tolerance, syncFrames=1
	suite.tickAt(20, target, 30) // in tolerance, syncFrames=2 -> resynced
	suite.Require().True(suite.gen.profile.resynced)

	// A few frames into the hold-off, the clutch peak arrives.
	suite.tickAt(20, target, 30)
	suite.tickAt(20, target, 30)
	suite.tickAt(300, target, 30)

	for suite.gen.profile.measuring {
		suite.tickAt(20, target, 30)
	}

	suite.InDelta(suite.blended(300), suite.gen.profile.character(true), 0.001,
		"a peak arriving inside the hold-off must be captured")
}

// TestNoEarlyCloseBeforeMinimumFrames checks the floor on the other side of
// gearShiftMinMeasureFrames: engine speed passes through the post-shift target
// transiently on its way to the flare, so without the floor a downshift could
// close its window before the event it is meant to measure has happened.
func (suite *GearShiftResyncWindowTestSuite) TestNoEarlyCloseBeforeMinimumFrames() {
	suite.armAt(false, 2.0, 1.5, 6000, 30)
	target := suite.inSync()

	for i := range gearShiftMinMeasureFrames - 1 {
		suite.tickAt(50, target, 30)
		suite.True(suite.gen.profile.measuring,
			"frame %d is below the minimum and must not close the window", i)
		suite.False(suite.gen.profile.resynced)
	}
}

// TestSingleInToleranceFrameDoesNotEndTheWindow checks that a lone in-tolerance
// frame, of the kind produced by engine speed transiently crossing the target
// during the flare, is not mistaken for settling at the new ratio.
func (suite *GearShiftResyncWindowTestSuite) TestSingleInToleranceFrameDoesNotEndTheWindow() {
	suite.armAt(false, 2.0, 1.5, 6000, 30)
	target := suite.inSync()
	offTarget := target * 2

	const measureFrames = 32
	for frame := range measureFrames {
		rpm := target
		if frame%2 == 1 {
			rpm = offTarget
		}

		suite.tickAt(50, rpm, 30)

		if frame < measureFrames-1 {
			suite.True(suite.gen.profile.measuring,
				"a single in-tolerance frame must not end the window (frame %d)", frame)
		}
	}

	suite.False(suite.gen.profile.measuring, "the window should still close at the cap")
	suite.False(suite.gen.profile.resynced)
}

// TestFallsBackToTheFrameCapWithoutAPrediction covers every way the shift can give
// no usable prediction (see gearShiftSyncTarget): the frame cap is the only
// terminator left, and it must neither hang nor panic.
func (suite *GearShiftResyncWindowTestSuite) TestFallsBackToTheFrameCapWithoutAPrediction() {
	cases := []struct {
		name                           string
		ratioFrom, ratioTo, rpm, speed float64
	}{
		{"zero ratioFrom", 0, 1.5, 6000, 30},
		{"zero ratioTo", 2.0, 0, 6000, 30},
		{"zero pre-shift speed", 2.0, 1.5, 6000, 0},
		{"zero pre-shift rpm", 2.0, 1.5, 0, 30},
	}

	for _, tcase := range cases {
		suite.SetupTest()

		suite.armAt(false, tcase.ratioFrom, tcase.ratioTo, tcase.rpm, tcase.speed)
		suite.Require().Zero(suite.gen.profile.syncTarget, tcase.name)

		suite.NotPanics(func() {
			for range 32 {
				suite.tickAt(50, 6000, 30)
			}
		}, tcase.name)

		suite.False(suite.gen.profile.measuring,
			"%s: a shift with no usable prediction must still close at the configured cap", tcase.name)
	}
}

// TestReEngagementIsFoundWhileBraking is the reason the target is rpm/groundSpeed
// rather than a fixed rpm: a downshift is usually taken under heavy braking, and
// over the tens of frames a measurement runs the car sheds enough speed for a
// fixed rpm target to drift out of tolerance and never match.
func (suite *GearShiftResyncWindowTestSuite) TestReEngagementIsFoundWhileBraking() {
	suite.armAt(true, 2.0, 2.8, 6000, 40)
	target := suite.gen.profile.syncTarget

	// Ratio staying on target under heavy braking: rpm falls proportionally with
	// ground speed, so rpm/speed holds at the target even as both fall sharply.
	speed := 40.0
	resynced := false

	for i := 0; i < 20 && suite.gen.profile.measuring; i++ {
		speed -= 1.5
		suite.tickAt(50, target*speed, speed)

		if suite.gen.profile.resynced {
			resynced = true

			break
		}
	}

	suite.True(resynced,
		"ratio staying on target under braking must still be detected as re-engagement")

	// Contrast: rpm held fixed while speed falls means the ratio drifts away from
	// target instead of settling on it, and must not be detected within the cap.
	suite.SetupTest()
	suite.armAt(true, 3.0, 2.0, 6000, 40) // target = (6000/40)*(2.0/3.0) = 100

	speed = 40.0
	for range 32 {
		speed -= 1.5
		suite.tickAt(50, 6000, speed)
	}

	suite.False(suite.gen.profile.resynced,
		"a fixed rpm target must drift out of tolerance under braking rather than settle")
	suite.False(suite.gen.profile.measuring,
		"with no resync the window must still close at the cap")
}

// TestZeroSpeedFrameDoesNotCountTowardReEngagement checks that a stopped car
// cannot satisfy the re-engagement test: dividing rpm by a zero ground speed is
// meaningless, and gearShiftHasResynced treats it as unusable rather than as a
// coincidental match.
func (suite *GearShiftResyncWindowTestSuite) TestZeroSpeedFrameDoesNotCountTowardReEngagement() {
	suite.armAt(false, 2.0, 1.5, 6000, 30)
	target := suite.inSync()

	for range gearShiftMinMeasureFrames {
		suite.tickAt(50, target, 30)
	}

	suite.Require().Equal(1, suite.gen.profile.syncFrames,
		"the first in-tolerance frame at the minimum should start the count")

	suite.tickAt(50, target, 0)
	suite.Zero(suite.gen.profile.syncFrames, "a zero-speed frame must reset the re-engagement count")
	suite.False(suite.gen.profile.resynced)
}

// floorSeed is the jerk the seed should resolve to: the value mapping to exactly
// the configured gain floor.
func (suite *GearShiftResyncWindowTestSuite) floorSeed() float64 {
	curve := suite.gen.cfg.GetHapticsTransmissionJerkCurve() / 1000
	characterMax := gearShiftCharacterMax

	seed := characterMax * math.Pow(signal.GainToPowerRatio(suite.gen.gainMin), 1/curve)

	return seed / gearShiftReferenceFrames
}

// blended is the character after collecting the given peaks, i.e. their median
// together with the floor seed.
func (suite *GearShiftResyncWindowTestSuite) blended(peaks ...float64) float64 {
	return medianCharacter(peaks, suite.floorSeed())
}

// armAt sets up a shift from ratioFrom to ratioTo at the given pre-shift rpm and
// speed, then arms the measurement. syncTarget becomes (rpm/speed)*(ratioTo/ratioFrom).
func (suite *GearShiftResyncWindowTestSuite) armAt(down bool, ratioFrom, ratioTo, rpm, speed float64) {
	suite.gen.profile.lastRatio = ratioFrom
	suite.gen.profile.curRatio = ratioTo
	suite.gen.profile.lastRPM = rpm
	suite.gen.profile.curRPM = rpm

	suite.kin.Last.GroundSpeed = speed
	// The launch-speed exclusion is not what these cases are testing, so the
	// arming speed is kept clear of it independently of the pre-shift speed under
	// test (which some cases deliberately set to zero).
	suite.kin.Current.GroundSpeed = math.Max(speed, gearShiftLaunchSpeedMps+1)

	suite.gen.armMeasurement(0, down)
}

// tickAt advances one frame with the given jerk, engine rpm and ground speed.
func (suite *GearShiftResyncWindowTestSuite) tickAt(jerk, rpm, speed float64) {
	suite.kin.Current.SurgeJerk = jerk
	suite.kin.Current.GroundSpeed = speed
	suite.gen.profile.curRPM = rpm

	suite.gen.TickMeasurement()
}

// gearShiftResyncTestSpeed is the ground speed every
// GearShiftResyncWindowTestSuite case arms and drives at.
const gearShiftResyncTestSpeed = 30

// inSync returns the rpm that puts the driveline exactly on target at the
// suite's fixed test speed.
func (suite *GearShiftResyncWindowTestSuite) inSync() float64 {
	return suite.gen.profile.syncTarget * gearShiftResyncTestSpeed
}

// GearShiftImpulseTestSuite covers the duration side of the learned profile: the
// re-engagement time collected alongside the jerk peak, and gearShiftImpulse, which
// combines the two into the quantity that ranks a slow gentle gearbox above a fast
// one. It runs on the driveline source, the default, so its magnitude assertions
// exercise the same path a real vehicle uses.
type GearShiftImpulseTestSuite struct {
	suite.Suite

	gen *TransmissionGenerator
	kin kinematics.State
	tel fakeTelemetry
}

func TestGearShiftImpulseTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(GearShiftImpulseTestSuite))
}

func (suite *GearShiftImpulseTestSuite) SetupTest() {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	suite.newGenerator(cfg, 9000)
}

// TestDurationIsLearnedFromResyncAndMedianed checks that the duration sample folded
// in on completion is the frame at which re-sync was declared, not the frame the
// window happened to close on, and that repeated shifts land the median on that
// value: a modern paddle box that always re-engages at the same offset must settle
// there rather than drift toward the frame cap the hold-off keeps running past.
func (suite *GearShiftImpulseTestSuite) TestDurationIsLearnedFromResyncAndMedianed() {
	suite.gen.seedProfile()

	const resyncOffset = 8

	for range gearShiftLearningSamples {
		suite.driveShiftToResyncAt(false, resyncOffset, 30)
	}

	suite.True(suite.gen.profile.settled(false))
	suite.InDelta(float64(resyncOffset), suite.gen.profile.durationUp, 0.001)
}

// TestCappedWindowRecordsNoDuration is the case the completeGearShiftMeasurement
// comment on the duration path exists for: a window that never re-syncs still has a
// genuine jerk peak worth learning from, but no re-engagement time to report.
// Folding the frame cap in as if it were a measured duration would report every such
// shift as the slowest gearbox in the fleet, which is not what happened — the
// driveline simply never satisfied the re-sync test within the window.
func (suite *GearShiftImpulseTestSuite) TestCappedWindowRecordsNoDuration() {
	suite.gen.seedProfile()

	suite.armAt(false, 2.0, 1.5, 6000, 30)

	// Held permanently out of tolerance, so re-sync is never declared and the
	// window can only close at the configured frame cap.
	offTarget := suite.inSync(30) * 3

	for range 32 {
		suite.tickAt(120, offTarget, 30)
	}

	suite.Require().False(suite.gen.profile.measuring)
	suite.Require().False(suite.gen.profile.resynced)

	suite.Equal(1, suite.gen.profile.samplesUp,
		"the jerk peak must still be recorded even though the window hit the cap")
	suite.Zero(suite.gen.profile.durationSamplesUp,
		"a window that hit the cap without re-syncing must not contribute a duration sample")
}

// TestDirectionsLearnDurationIndependently mirrors the character learning: a
// gearbox can complete its upshifts and downshifts in different times, and each
// direction's duration must be attributed only to the shifts it was measured from.
func (suite *GearShiftImpulseTestSuite) TestDirectionsLearnDurationIndependently() {
	suite.gen.seedProfile()

	for range gearShiftLearningSamples {
		suite.driveShiftToResyncAt(false, 6, 30)
	}

	for range gearShiftLearningSamples {
		suite.driveShiftToResyncAt(true, 9, 40)
	}

	suite.InDelta(6.0, suite.gen.profile.durationUp, 0.001)
	suite.InDelta(9.0, suite.gen.profile.durationDown, 0.001)
}

// TestImpulseRanksSlowGentleAboveFastGentle is the case gearShiftImpulse exists for:
// a Nissan R92CP's clutched upshift (character 63, learned duration 11 frames)
// spreads its event over more than twice a Toyota Supra RZ's duration (character
// 31, duration 9), so the impulse gap between them must be wider than the character
// gap alone would produce, and a genuinely modern box (character 241, duration 5)
// must still out-rank both despite its much shorter duration.
func (suite *GearShiftImpulseTestSuite) TestImpulseRanksSlowGentleAboveFastGentle() {
	r92cp := suite.impulseFor(63, 11)
	supraRZ := suite.impulseFor(31, 9)
	modern := suite.impulseFor(241, 5)

	suite.Greater(r92cp, supraRZ,
		"a slower gearbox must not be under-ranked just because its instantaneous peak is smaller")
	suite.Greater(modern, r92cp, "a genuinely sharper, quicker gearbox must still come out on top")
	suite.Greater(modern, supraRZ)
}

// TestImpulseReachesThePlayedMagnitude checks that the duration term is not merely
// computed but actually reaches determineGearShiftMagnitude: with everything else
// held equal, a longer learned duration must play louder than a shorter one.
func (suite *GearShiftImpulseTestSuite) TestImpulseReachesThePlayedMagnitude() {
	const character = 100.0

	suite.gen.profile.characterUp = character
	suite.gen.profile.durationUp = gearShiftSharpFrames
	suite.shift(2, 3, 1.85, 1.35, 7000)
	shortDuration := suite.gen.magnitudeFromDriveline()

	suite.gen.profile.characterUp = character
	suite.gen.profile.durationUp = gearShiftHeavyFrames
	suite.shift(2, 3, 1.85, 1.35, 7000)
	longDuration := suite.gen.magnitudeFromDriveline()

	suite.Greater(longDuration, shortDuration,
		"the same character stretched over a longer measured duration must play louder")
}

// TestMagnitudeStaysWithinTheWindow mirrors GearShiftDrivelineTestSuite's assertion
// of the same name: however extreme the learned character and duration, the played
// magnitude must stay inside the vehicle's configured window.
func (suite *GearShiftImpulseTestSuite) TestMagnitudeStaysWithinTheWindow() {
	floor := signal.GainToPowerRatio(suite.gen.gainMin)

	suite.gen.profile.characterDown = 100000
	suite.gen.profile.durationDown = gearShiftHeavyFrames * 100
	suite.shift(6, 1, 0.85, 3.50, 9000)

	magnitude := suite.gen.magnitudeFromDriveline()
	suite.LessOrEqual(magnitude, 1.0)
	suite.GreaterOrEqual(magnitude, floor)

	suite.gen.profile.characterDown = 0
	suite.gen.profile.durationDown = 0
	suite.shift(3, 2, 1.35, 1.36, 100)

	suite.GreaterOrEqual(suite.gen.magnitudeFromDriveline(), floor)
}

// TestMagnitudeIsIndependentOfChannelGain mirrors GearShiftDrivelineTestSuite's
// assertion of the same name: PlayEffect applies the channel gain, so trimming it
// must not also move the magnitude this computes.
func (suite *GearShiftImpulseTestSuite) TestMagnitudeIsIndependentOfChannelGain() {
	// setTransmissionGain below reseeds the whole profile, so the initial calculation
	// must start from a seeded profile too, otherwise it compares a zero-value
	// profile against a freshly seeded one instead of the same floor twice.
	suite.gen.seedProfile()
	suite.gen.profile.characterUp = 150
	suite.gen.profile.durationUp = 9

	suite.shift(3, 2, 1.35, 1.85, 7000)
	atUnityGain := suite.gen.magnitudeFromDriveline()

	suite.gen.cfg.SetSynthTransmissionGain(-6.0)
	suite.gen.SetVehicle(vehicle.Characteristics{VehicleType: vehicle.TypeRace, RevLimit: 9000})
	suite.shift(3, 2, 1.35, 1.85, 7000)

	suite.InDelta(atUnityGain, suite.gen.magnitudeFromDriveline(), 0.001,
		"channel trim must be applied once, by the mixer, not folded in here")
}

// TestPulseShapeIsMonotonic checks that gearShiftPulseShape never reverses
// direction: a gearbox measured slower must never be mapped to a sharper waveform
// than one measured faster.
func (suite *GearShiftImpulseTestSuite) TestPulseShapeIsMonotonic() {
	prevHz, prevSeconds := math.Inf(1), math.Inf(-1)

	for frames := 0.0; frames <= 20; frames++ {
		hz, seconds := gearShiftPulseShape(frames)

		suite.LessOrEqual(hz, prevHz, "pulse frequency must not increase as duration grows")
		suite.GreaterOrEqual(seconds, prevSeconds, "pulse length must not shrink as duration grows")

		prevHz, prevSeconds = hz, seconds
	}
}

// TestStandingGearSelectionPlaysOnlyTheFloor covers the standstill case, where the
// only thing physically happening is the dog rings or synchros engaging — which is
// exactly what the per-vehicle gain floor represents.
//
// The event term already goes to zero at rest, because reverse and neutral have no
// usable ratio, but that alone was not enough: a zero event still scales the
// character by (1-depth), and the sub-unity volume curve lifts the result back up
// again. A stationary Porsche 963 measured 0.390 against a 0.355 floor, so standing
// gear selections came out nearly as strong as moving ones.
func (suite *GearShiftImpulseTestSuite) TestStandingGearSelectionPlaysOnlyTheFloor() {
	floor := signal.GainToPowerRatio(suite.gen.gainMin)

	// A harsh, fully learned gearbox, so nothing but the speed gate can hold it down.
	suite.gen.profile.characterUp = 240
	suite.gen.profile.durationUp = 5

	suite.shift(1, 2, 2.57, 2.06, 7000)
	suite.Greater(suite.gen.magnitudeFromDriveline(), floor,
		"a rolling shift must rise above the floor")

	suite.kin.Current.GroundSpeed = 0
	suite.InDelta(floor, suite.gen.magnitudeFromDriveline(), 0.0001,
		"a gear selected at a standstill must play the floor and nothing more")

	// The gate is the same threshold the learner already uses to exclude launches.
	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps - 0.1
	suite.InDelta(floor, suite.gen.magnitudeFromDriveline(), 0.0001)

	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps + 0.1
	suite.Greater(suite.gen.magnitudeFromDriveline(), floor)
}

// TestPulseShapeExactEndpointsAndMidpoint pins the mapping's two reference points
// and the midpoint arithmetic between them, computed here rather than guessed: at
// duration 8 the interpolation position is (8-5)/(11-5) = 0.5, so pulseHz lands
// exactly halfway between 30 and 22 (26 Hz) and lengthSeconds exactly halfway
// between 0.100 and 0.120 (0.110 s).
//
// The endpoints are asserted against the constants rather than literals so a retune
// moves them together, but the midpoint is spelled out: it is the arithmetic that
// would silently break if the interpolation were ever rewritten.
func (suite *GearShiftImpulseTestSuite) TestPulseShapeExactEndpointsAndMidpoint() {
	pulseHz, seconds := gearShiftPulseShape(gearShiftSharpFrames)
	suite.InDelta(gearShiftSharpPulseHz, pulseHz, 0.001)
	suite.InDelta(gearShiftSharpSeconds, seconds, 0.0001)

	pulseHz, seconds = gearShiftPulseShape(gearShiftHeavyFrames)
	suite.InDelta(gearShiftHeavyPulseHz, pulseHz, 0.001)
	suite.InDelta(gearShiftHeavySeconds, seconds, 0.0001)

	pulseHz, seconds = gearShiftPulseShape(8.0)
	suite.InDelta(26.0, pulseHz, 0.001)
	suite.InDelta(0.110, seconds, 0.0001)
}

// TestPulseShapeClampsRatherThanExtrapolating checks the boundary behaviour outside
// the measured range: a gearbox faster than the sharp end or slower than the heavy
// end still exists (an unmeasured car may briefly report an implausible duration
// mid-learning), and it must be clamped to the nearest defined waveform rather than
// extrapolated into a frequency or length nobody tuned.
func (suite *GearShiftImpulseTestSuite) TestPulseShapeClampsRatherThanExtrapolating() {
	hz, seconds := gearShiftPulseShape(0)
	suite.InDelta(gearShiftSharpPulseHz, hz, 0.001)
	suite.InDelta(gearShiftSharpSeconds, seconds, 0.0001)

	hz, seconds = gearShiftPulseShape(100)
	suite.InDelta(gearShiftHeavyPulseHz, hz, 0.001)
	suite.InDelta(gearShiftHeavySeconds, seconds, 0.0001)
}

// impulseFor computes gearShiftImpulse for an upshift with the given learned
// character and duration, without disturbing any other test's profile state.
func (suite *GearShiftImpulseTestSuite) impulseFor(character, duration float64) float64 {
	suite.gen.profile.characterUp = character
	suite.gen.profile.durationUp = duration

	return suite.gen.impulse(false)
}

// driveShiftToResyncAt arms and runs a measurement in the given direction so that
// re-sync is declared at exactly framesElapsed == resyncOffset, then runs the
// hold-off out to completion. resyncOffset must be at least
// gearShiftMinMeasureFrames+2, since the two consecutive in-tolerance frames the
// re-sync test requires cannot both land before the minimum window has elapsed.
func (suite *GearShiftImpulseTestSuite) driveShiftToResyncAt(down bool, resyncOffset int, speed float64) {
	suite.Require().GreaterOrEqual(resyncOffset, gearShiftMinMeasureFrames+2)

	suite.armAt(down, 2.0, 1.5, 6000, speed)
	target := suite.inSync(speed)

	for range resyncOffset - 2 {
		suite.tickAt(50, target*3, speed) // out of tolerance
	}

	suite.tickAt(50, target, speed) // in tolerance, syncFrames=1
	suite.tickAt(50, target, speed) // in tolerance, syncFrames=2 -> resynced at resyncOffset
	suite.Require().True(suite.gen.profile.resynced)
	suite.Require().Equal(resyncOffset, suite.gen.profile.resyncAt)

	for suite.gen.profile.measuring {
		suite.tickAt(50, target, speed)
	}
}

// shift positions the state as a change between two gears at a given engine speed,
// using representative close-ratio values. Mirrors
// GearShiftDrivelineTestSuite.shift.
func (suite *GearShiftImpulseTestSuite) shift(from, to int, ratioFrom, ratioTo, rpm float64) {
	suite.kin.Last.TransmissionGear = from
	suite.kin.Current.TransmissionGear = to
	suite.gen.profile.lastRatio = ratioFrom
	suite.gen.profile.lastRPM = rpm
	suite.gen.profile.curRatio = ratioTo

	// A rolling shift. Below gearShiftLaunchSpeedMps the magnitude collapses to the
	// gain floor by design, since selecting a gear at a standstill moves no driveline
	// energy, and every case here is about what a moving car plays.
	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps + 1
}

// armAt sets up a shift from ratioFrom to ratioTo at the given pre-shift rpm and
// speed, then arms the measurement. syncTarget becomes (rpm/speed)*(ratioTo/ratioFrom).
// Mirrors GearShiftResyncWindowTestSuite.armAt.
func (suite *GearShiftImpulseTestSuite) armAt(down bool, ratioFrom, ratioTo, rpm, speed float64) {
	suite.gen.profile.lastRatio = ratioFrom
	suite.gen.profile.curRatio = ratioTo
	suite.gen.profile.lastRPM = rpm
	suite.gen.profile.curRPM = rpm

	suite.kin.Last.GroundSpeed = speed
	suite.kin.Current.GroundSpeed = math.Max(speed, gearShiftLaunchSpeedMps+1)

	suite.gen.armMeasurement(0, down)
}

// tickAt advances one frame with the given jerk, engine rpm and ground speed.
// Mirrors GearShiftResyncWindowTestSuite.tickAt.
func (suite *GearShiftImpulseTestSuite) tickAt(jerk, rpm, speed float64) {
	suite.kin.Current.SurgeJerk = jerk
	suite.kin.Current.GroundSpeed = speed
	suite.gen.profile.curRPM = rpm

	suite.gen.TickMeasurement()
}

// inSync returns the rpm that puts the driveline exactly on target at a speed.
// Mirrors GearShiftResyncWindowTestSuite.inSync.
func (suite *GearShiftImpulseTestSuite) inSync(speed float64) float64 {
	return suite.gen.profile.syncTarget * speed
}

// Each suite wires its own generator to its own kinematics state and fake telemetry.
// The bodies are identical; Go has no way to share one method across four suite types
// without an embedded base, and embedding would hide the fields the tests read.

func (suite *GearShiftProfileTestSuite) newGenerator(cfg *config.Config, revLimit uint16) {
	suite.gen = newTransmissionFixture(cfg, &suite.kin, &suite.tel, revLimit)
}

func (suite *GearShiftResyncWindowTestSuite) newGenerator(cfg *config.Config, revLimit uint16) {
	suite.gen = newTransmissionFixture(cfg, &suite.kin, &suite.tel, revLimit)
}

func (suite *GearShiftImpulseTestSuite) newGenerator(cfg *config.Config, revLimit uint16) {
	suite.gen = newTransmissionFixture(cfg, &suite.kin, &suite.tel, revLimit)
}

// The driveline sampler reads the telemetry client, so it could not be tested at all
// while the generator held a *gttelemetry.Client. These cases cover the guards that
// keep a sentinel gear index out of the ratio slice, which is the reason the generator
// does not use Transformer.CurrentGearRatio.

func (suite *GearShiftDrivelineTestSuite) TestAdvanceDrivelineSamplesTheSelectedGear() {
	suite.tel.ratios = []float32{3.2, 2.1, 1.6, 1.312, 1.0}
	suite.tel.rpm = 6500
	suite.kin.Current.TransmissionGear = 4

	suite.gen.AdvanceDriveline()

	suite.InDelta(1.312, suite.gen.profile.curRatio, 0.0001)
	suite.InDelta(6500, suite.gen.profile.curRPM, 0.0001)
}

// Each frame must push the current sample down to the previous one, because the
// magnitude path reads the pair rather than the telemetry client.
func (suite *GearShiftDrivelineTestSuite) TestAdvanceDrivelineShiftsCurrentToLast() {
	suite.tel.ratios = []float32{3.2, 2.1, 1.6, 1.312, 1.0}

	suite.tel.rpm = 6500
	suite.kin.Current.TransmissionGear = 4
	suite.gen.AdvanceDriveline()

	suite.tel.rpm = 4800
	suite.kin.Current.TransmissionGear = 5
	suite.gen.AdvanceDriveline()

	suite.InDelta(1.312, suite.gen.profile.lastRatio, 0.0001)
	suite.InDelta(6500, suite.gen.profile.lastRPM, 0.0001)
	suite.InDelta(1.0, suite.gen.profile.curRatio, 0.0001)
	suite.InDelta(4800, suite.gen.profile.curRPM, 0.0001)
}

// Reverse (0) and neutral (15) are sentinel indices, not positions in the ratio
// slice. Indexing the slice with either one would read the wrong ratio or panic.
func (suite *GearShiftDrivelineTestSuite) TestSentinelGearsHaveNoRatio() {
	suite.tel.ratios = []float32{3.2, 2.1, 1.6, 1.312, 1.0}

	for _, gear := range []int{0, 15} {
		suite.kin.Current.TransmissionGear = gear

		suite.Zero(suite.gen.currentGearRatio(),
			"gear %d is a sentinel, not a ratio index", gear)
	}
}

// A gear beyond the reported ratios, or one whose ratio is unset, yields no ratio
// rather than an out-of-range read.
func (suite *GearShiftDrivelineTestSuite) TestMissingRatioYieldsZero() {
	suite.tel.ratios = []float32{3.2, 2.1, 0}

	suite.kin.Current.TransmissionGear = 3
	suite.Zero(suite.gen.currentGearRatio(), "an unset ratio is not usable")

	suite.kin.Current.TransmissionGear = 6
	suite.Zero(suite.gen.currentGearRatio(), "gear beyond the reported ratios")
}

// shift positions the state as a change between two gears at a given engine speed,
// using representative close-ratio values.
func (suite *GearShiftDrivelineTestSuite) shift(from, to int, ratioFrom, ratioTo, rpm float64) {
	suite.kin.Last.TransmissionGear = from
	suite.kin.Current.TransmissionGear = to
	suite.gen.profile.lastRatio = ratioFrom
	suite.gen.profile.lastRPM = rpm
	suite.gen.profile.curRatio = ratioTo

	// A rolling shift. Below gearShiftLaunchSpeedMps the magnitude collapses to the
	// gain floor by design, since selecting a gear at a standstill moves no driveline
	// energy, and every case here is about what a moving car plays.
	suite.kin.Current.GroundSpeed = gearShiftLaunchSpeedMps + 1
}

func (suite *GearShiftDrivelineTestSuite) newGenerator(cfg *config.Config, revLimit uint16) {
	suite.gen = newTransmissionFixture(cfg, &suite.kin, &suite.tel, revLimit)
}
