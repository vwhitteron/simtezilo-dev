package predictivelap //nolint:testpackage // white-box testing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// testFrameInterval is 1/60 s. It is not an exact number of nanoseconds
// (16666666ns), so expected durations are expressed as a multiple of it rather
// than as round seconds — the truncation is harmless (it cancels in the delta,
// which uses the same clock for both laps).
const testFrameInterval = time.Second / 60

type LapClockTestSuite struct {
	suite.Suite

	clock *LapClock
}

func TestLapClockTestSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(LapClockTestSuite))
}

func (suite *LapClockTestSuite) SetupTest() {
	suite.clock = NewLapClock(testFrameInterval)
}

func (suite *LapClockTestSuite) TestNotStartedReturnsFalse() {
	_, ok := suite.clock.Elapsed()
	suite.False(ok, "Elapsed should return ok=false before StartLap")
}

func (suite *LapClockTestSuite) TestStartLapThenAdvanceAccumulates() {
	suite.clock.StartLap()
	suite.clock.Advance(60, true) // 60 frames at 60 Hz = 1 second

	elapsed, ok := suite.clock.Elapsed()
	suite.True(ok)
	suite.Equal(60*testFrameInterval, elapsed)
}

func (suite *LapClockTestSuite) TestPausedFramesAreExcluded() {
	suite.clock.StartLap()
	suite.clock.Advance(30, true)  // 30 live frames
	suite.clock.Advance(30, false) // 30 paused frames — must not count
	suite.clock.Advance(30, true)  // 30 more live frames

	elapsed, ok := suite.clock.Elapsed()
	suite.True(ok)
	// Only 60 live frames should have accumulated.
	suite.Equal(60*testFrameInterval, elapsed)
}

func (suite *LapClockTestSuite) TestDroppedFramesAreCounted() {
	suite.clock.StartLap()
	suite.clock.Advance(120, true) // 120 frames = 2 seconds (simulates a dropped-packet gap)

	elapsed, ok := suite.clock.Elapsed()
	suite.True(ok)
	suite.Equal(120*testFrameInterval, elapsed)
}

func (suite *LapClockTestSuite) TestResetClears() {
	suite.clock.StartLap()
	suite.clock.Advance(60, true)
	suite.clock.Reset()

	_, ok := suite.clock.Elapsed()
	suite.False(ok, "Elapsed should return ok=false after Reset")
}

func (suite *LapClockTestSuite) TestStartLapRestartsFromZero() {
	suite.clock.StartLap()
	suite.clock.Advance(60, true)
	suite.clock.StartLap() // restart mid-lap

	elapsed, ok := suite.clock.Elapsed()
	suite.True(ok)
	suite.Equal(time.Duration(0), elapsed)
}
