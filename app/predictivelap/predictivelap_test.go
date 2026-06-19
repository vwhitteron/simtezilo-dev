package predictivelap //nolint:testpackage // white-box testing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type PredictiveLapTestSuite struct {
	suite.Suite

	pred *PredictiveLap
}

func TestPredictiveLapTestSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(PredictiveLapTestSuite))
}

func (suite *PredictiveLapTestSuite) SetupTest() {
	suite.pred = New()
}

func (suite *PredictiveLapTestSuite) TestNoReferenceYet() {
	_, ok := suite.pred.Delta(0.5, 30*time.Second)
	suite.False(ok)
}

func (suite *PredictiveLapTestSuite) TestFasterCurrentLapIsPositive() {
	suite.recordLap(60 * time.Second) // reference: 60s lap, 30s at halfway

	// Current lap is ahead: only 28s elapsed at halfway -> +2s.
	secs, ok := suite.pred.Delta(0.5, 28*time.Second)
	suite.True(ok)
	suite.InDelta(2.0, secs, 0.2)
}

func (suite *PredictiveLapTestSuite) TestSlowerCurrentLapIsNegative() {
	suite.recordLap(60 * time.Second)

	// Current lap is behind: 33s elapsed at halfway -> -3s.
	secs, ok := suite.pred.Delta(0.5, 33*time.Second)
	suite.True(ok)
	suite.InDelta(-3.0, secs, 0.2)
}

func (suite *PredictiveLapTestSuite) TestOnlyFasterLapBecomesReference() {
	suite.recordLap(60 * time.Second)

	// A slower lap must not replace the reference.
	suite.recordLap(70 * time.Second)

	// Reference should still be the 60s lap (30s at halfway).
	secs, ok := suite.pred.Delta(0.5, 30*time.Second)
	suite.True(ok)
	suite.InDelta(0.0, secs, 0.2)
}

func (suite *PredictiveLapTestSuite) TestResetClearsReference() {
	suite.recordLap(60 * time.Second)
	suite.pred.Reset()

	_, ok := suite.pred.Delta(0.5, 30*time.Second)
	suite.False(ok)
}

func (suite *PredictiveLapTestSuite) TestInvalidProgressIgnored() {
	// Out-of-range / non-finite progress must not panic or record.
	suite.pred.Record(-0.1, time.Second)
	suite.pred.Record(1.5, time.Second)
	suite.pred.CompleteLap(60 * time.Second)

	// Endpoints (0 and 1) were never recorded, but interpolation still has no
	// samples, so there is no reference data to compare against.
	_, ok := suite.pred.Delta(0.5, 30*time.Second)
	suite.False(ok)
}

// recordLap simulates driving a lap whose elapsed time at progress p is
// p*lapTime, recording samples at fine progress intervals, then completing it.
func (suite *PredictiveLapTestSuite) recordLap(lapTime time.Duration) {
	for i := range 1001 {
		progress := float64(i) / 1000.0
		elapsed := time.Duration(progress * float64(lapTime))
		suite.pred.Record(progress, elapsed)
	}

	suite.pred.CompleteLap(lapTime)
}
