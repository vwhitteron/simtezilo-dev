package tyres_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/vwhitteron/simtezilo-dev/app/tyres"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

type TyreTestSuite struct {
	suite.Suite
}

func TestTyreTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(TyreTestSuite))
}

func (suite *TyreTestSuite) TestNewTyreAttributes() {
	// Arrange
	optimalCenter := float32(80.0)
	optimalWindow := float32(10.0)
	margin := float32(5.0)
	tyreTemps := models.CornerSet{
		FrontLeft:  75.0, // Optimal
		FrontRight: 95.0, // Hot
		RearLeft:   65.0, // Cold
		RearRight:  0.0,  // Invalid
	}

	// Act
	attributes := tyres.New(optimalCenter, optimalWindow, margin, tyreTemps)

	// Assert
	// Note: Internal thresholds are not directly accessible, which is fine for encapsulation

	// Check individual tyre conditions
	suite.Equal(tyres.ConditionOptimal.String(), attributes.Condition(tyres.PositionFrontLeft))
	suite.Equal(tyres.ConditionHot.String(), attributes.Condition(tyres.PositionFrontRight))
	suite.Equal(tyres.ConditionCold.String(), attributes.Condition(tyres.PositionRearLeft))
	suite.Equal(tyres.ConditionInvalid.String(), attributes.Condition(tyres.PositionRearRight))

	// Check individual tyre temperatures
	suite.InDelta(float32(75.0), attributes.Temperature(tyres.PositionFrontLeft), 0.001)
	suite.InDelta(float32(95.0), attributes.Temperature(tyres.PositionFrontRight), 0.001)
	suite.InDelta(float32(65.0), attributes.Temperature(tyres.PositionRearLeft), 0.001)
	suite.InDelta(float32(0.0), attributes.Temperature(tyres.PositionRearRight), 0.001)
}

func (suite *TyreTestSuite) TestIndividualTyreConditionCalculations() {
	// Test configuration: optimal = 80°C, window = 10°C, margin = 5°C
	// Cold: < 70°C, Optimal: 75-85°C, Hot: > 90°C
	optimalCenter := float32(80.0)
	optimalWindow := float32(10.0)
	margin := float32(5.0)

	testCases := []struct {
		name        string
		temperature float32
		expected    tyres.Condition
	}{
		{"Invalid temperature (zero)", 0.0, tyres.ConditionInvalid},
		{"Invalid temperature (negative)", -5.0, tyres.ConditionInvalid},
		{"Cold temperature", 60.0, tyres.ConditionCold},
		{"Just below cold threshold", 69.9, tyres.ConditionCold},
		{"Just at cold threshold", 70.0, tyres.ConditionOptimal}, // temp < coldThreshold, so 70.0 is NOT cold
		{"Just above cold threshold", 70.1, tyres.ConditionOptimal},
		{"Optimal minimum", 75.0, tyres.ConditionOptimal},
		{"Optimal center", 80.0, tyres.ConditionOptimal},
		{"Optimal maximum", 85.0, tyres.ConditionOptimal},
		{"Just below hot threshold", 90.0, tyres.ConditionOptimal}, // temp > hotThreshold, so 90.0 is NOT hot
		{"Just above hot threshold", 90.1, tyres.ConditionHot},
		{"Hot temperature", 95.0, tyres.ConditionHot},
		{"Very hot temperature", 110.0, tyres.ConditionHot},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Create attributes with a single tyre at the test temperature
			tyreTemps := models.CornerSet{
				FrontLeft:  testCase.temperature,
				FrontRight: 80.0, // Optimal
				RearLeft:   80.0, // Optimal
				RearRight:  80.0, // Optimal
			}

			// Act
			attributes := tyres.New(optimalCenter, optimalWindow, margin, tyreTemps)

			// Assert
			result := attributes.Condition(tyres.PositionFrontLeft)
			suite.Equal(testCase.expected.String(), result, "Temperature %.2f°C should be %s", testCase.temperature, testCase.expected)
		})
	}
}

func (suite *TyreTestSuite) TestAverageTemperature() {
	// Arrange
	tyreTemps := models.CornerSet{
		FrontLeft:  70.0,
		FrontRight: 80.0,
		RearLeft:   90.0,
		RearRight:  100.0,
	}
	attributes := tyres.New(80.0, 10.0, 5.0, tyreTemps)

	// Act
	average := attributes.AverageTemperature()

	// Assert
	expected := (70.0 + 80.0 + 90.0 + 100.0) / 4.0
	suite.InEpsilon(float32(expected), average, 0.001)
}

func (suite *TyreTestSuite) TestConditions() {
	// Arrange
	tyreTemps := models.CornerSet{
		FrontLeft:  65.0, // Cold
		FrontRight: 80.0, // Optimal
		RearLeft:   95.0, // Hot
		RearRight:  0.0,  // Invalid
	}
	attributes := tyres.New(80.0, 10.0, 5.0, tyreTemps)

	// Act
	conditionMap := attributes.Conditions()

	// Assert
	// Check that we have the expected conditions
	coldTyres, hasCold := conditionMap[tyres.ConditionCold]
	suite.True(hasCold)
	suite.Contains(coldTyres, tyres.PositionFrontLeft)

	optimalTyres, hasOptimal := conditionMap[tyres.ConditionOptimal]
	suite.True(hasOptimal)
	suite.Contains(optimalTyres, tyres.PositionFrontRight)

	hotTyres, hasHot := conditionMap[tyres.ConditionHot]
	suite.True(hasHot)
	suite.Contains(hotTyres, tyres.PositionRearLeft)

	invalidTyres, hasInvalid := conditionMap[tyres.ConditionInvalid]
	suite.True(hasInvalid)
	suite.Contains(invalidTyres, tyres.PositionRearRight)
}

func (suite *TyreTestSuite) TestContainsCondition() {
	// Arrange
	tyreTemps := models.CornerSet{
		FrontLeft:  65.0, // Cold
		FrontRight: 80.0, // Optimal
		RearLeft:   95.0, // Hot
		RearRight:  0.0,  // Invalid
	}
	attributes := tyres.New(80.0, 10.0, 5.0, tyreTemps)

	// Act & Assert
	suite.True(attributes.ContainsCondition(tyres.ConditionCold))
	suite.True(attributes.ContainsCondition(tyres.ConditionOptimal))
	suite.True(attributes.ContainsCondition(tyres.ConditionHot))
	suite.True(attributes.ContainsCondition(tyres.ConditionInvalid))
}

func (suite *TyreTestSuite) TestConditionOptimal() {
	testCases := []struct {
		name     string
		temps    models.CornerSet
		expected bool
	}{
		{
			name: "All tyres optimal",
			temps: models.CornerSet{
				FrontLeft:  80.0,
				FrontRight: 82.0,
				RearLeft:   78.0,
				RearRight:  83.0,
			},
			expected: true,
		},
		{
			name: "One tyre hot - not optimal",
			temps: models.CornerSet{
				FrontLeft:  80.0,
				FrontRight: 95.0, // Hot
				RearLeft:   78.0,
				RearRight:  83.0,
			},
			expected: false,
		},
		{
			name: "Average too low - not optimal",
			temps: models.CornerSet{
				FrontLeft:  65.0, // Cold but average brings it down
				FrontRight: 75.0,
				RearLeft:   75.0,
				RearRight:  75.0,
			},
			expected: false,
		},
		{
			name: "Average too high - not optimal",
			temps: models.CornerSet{
				FrontLeft:  85.0,
				FrontRight: 85.0,
				RearLeft:   85.0,
				RearRight:  89.0, // High but not hot, average brings it up
			},
			expected: false,
		},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Act
			attributes := tyres.New(80.0, 10.0, 5.0, testCase.temps)

			// Assert
			suite.Equal(testCase.expected, attributes.ConditionOptimal())
		})
	}
}

func (suite *TyreTestSuite) TestEqual() {
	// Arrange
	temps1 := models.CornerSet{
		FrontLeft:  70.0,
		FrontRight: 80.0,
		RearLeft:   90.0,
		RearRight:  100.0,
	}
	temps2 := models.CornerSet{
		FrontLeft:  70.0,
		FrontRight: 80.0,
		RearLeft:   90.0,
		RearRight:  100.0,
	}
	temps3 := models.CornerSet{
		FrontLeft:  75.0, // Different
		FrontRight: 80.0,
		RearLeft:   90.0,
		RearRight:  100.0,
	}

	attributes1 := tyres.New(80.0, 10.0, 5.0, temps1)
	attributes2 := tyres.New(80.0, 10.0, 5.0, temps2)
	attributes3 := tyres.New(80.0, 10.0, 5.0, temps3)

	// Act & Assert
	suite.True(attributes1.Equal(attributes2), "Identical attributes should be equal")
	suite.False(attributes1.Equal(attributes3), "Different attributes should not be equal")
}

func (suite *TyreTestSuite) TestPositionString() {
	testCases := []struct {
		position tyres.Position
		expected string
	}{
		{tyres.PositionFront, "front"},
		{tyres.PositionRear, "rear"},
		{tyres.PositionFrontLeft, "front left"},
		{tyres.PositionFrontRight, "front right"},
		{tyres.PositionRearLeft, "rear left"},
		{tyres.PositionRearRight, "rear right"},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.expected, func() {
			// Act
			result := testCase.position.String()

			// Assert
			suite.Equal(testCase.expected, result)
		})
	}
}

func (suite *TyreTestSuite) TestConditionString() {
	testCases := []struct {
		condition tyres.Condition
		expected  string
	}{
		{tyres.ConditionInvalid, "invalid"},
		{tyres.ConditionOptimal, "optimal"},
		{tyres.ConditionHot, "hot"},
		{tyres.ConditionCold, "cold"},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.expected, func() {
			// Act
			result := testCase.condition.String()

			// Assert
			suite.Equal(testCase.expected, result)
		})
	}
}
