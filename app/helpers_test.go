package app

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/suite"
)

type HelpersTestSuite struct {
	suite.Suite
}

func TestHelpersTestSuite(t *testing.T) {
	suite.Run(t, new(HelpersTestSuite))
}
func (suite *HelpersTestSuite) TestGearNameReturnsCorrectNameForValidGear() {
	gearNumbers := []int{-100, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15}

	for gearNum := range gearNumbers {
		suite.Run("Gear "+strconv.Itoa(gearNum), func() {
			// Arrange
			wantName := gearNames[gearNum]

			// Act
			result := gearName(gearNum)

			// Assert
			suite.Equal(wantName, result, "Gear name should match expected value")
		})
	}
}
