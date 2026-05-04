package tyres_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/vwhitteron/simtezilo-dev/app/tyres"
	"github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// mockConfigProvider implements tyres.ConfigProvider for testing.
type mockConfigProvider struct {
	mu      sync.RWMutex
	optimal float32
	window  float32
	margin  float32
}

func (m *mockConfigProvider) GetPitRadioTyreTemperatureOptimalCelsius() float32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.optimal
}

func (m *mockConfigProvider) GetPitRadioTyreTemperatureOperatingWindow() float32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.window
}

func (m *mockConfigProvider) GetPitRadioTyreTemperatureMarginCelsius() float32 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.margin
}

func (m *mockConfigProvider) setWindow(w float32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.window = w
}

type TyreTestSuite struct {
	suite.Suite
}

func TestTyreTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(TyreTestSuite))
}

func (suite *TyreTestSuite) TestNewTyreAttributes() {
	// Arrange
	cfg := &mockConfigProvider{optimal: 80.0, window: 10.0, margin: 5.0}
	tyreTemps := models.CornerSet{
		FrontLeft:  75.0, // Optimal
		FrontRight: 95.0, // Hot
		RearLeft:   65.0, // Cold
		RearRight:  0.0,  // Invalid
	}

	// Act
	attributes := tyres.New(cfg, tyreTemps)

	// Assert
	suite.Equal(tyres.ConditionOptimal, attributes.ConditionAtPosition(tyres.PositionFrontLeft))
	suite.Equal(tyres.ConditionHot, attributes.ConditionAtPosition(tyres.PositionFrontRight))
	suite.Equal(tyres.ConditionCold, attributes.ConditionAtPosition(tyres.PositionRearLeft))
	suite.Equal(tyres.ConditionInvalid, attributes.ConditionAtPosition(tyres.PositionRearRight))

	suite.InDelta(float32(75.0), attributes.TemperatureAtPosition(tyres.PositionFrontLeft), 0.001)
	suite.InDelta(float32(95.0), attributes.TemperatureAtPosition(tyres.PositionFrontRight), 0.001)
	suite.InDelta(float32(65.0), attributes.TemperatureAtPosition(tyres.PositionRearLeft), 0.001)
	suite.InDelta(float32(0.0), attributes.TemperatureAtPosition(tyres.PositionRearRight), 0.001)
}

func (suite *TyreTestSuite) TestIndividualTyreConditionCalculations() {
	// Test configuration: optimal = 80°C, window = 10°C, margin = 5°C
	// Cold: < 70°C, Optimal: 75-85°C, Hot: > 90°C
	cfg := &mockConfigProvider{optimal: 80.0, window: 10.0, margin: 5.0}

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
			// Arrange
			tyreTemps := models.CornerSet{
				FrontLeft:  testCase.temperature,
				FrontRight: 80.0, // Optimal
				RearLeft:   80.0, // Optimal
				RearRight:  80.0, // Optimal
			}

			// Act
			attributes := tyres.New(cfg, tyreTemps)

			// Assert
			result := attributes.ConditionAtPosition(tyres.PositionFrontLeft)
			suite.Equal(testCase.expected, result, "Temperature %.2f°C should be %s", testCase.temperature, testCase.expected)
		})
	}
}

func (suite *TyreTestSuite) TestAverageTemperature() {
	// Arrange
	cfg := &mockConfigProvider{optimal: 80.0, window: 10.0, margin: 5.0}
	tyreTemps := models.CornerSet{
		FrontLeft:  70.0,
		FrontRight: 80.0,
		RearLeft:   90.0,
		RearRight:  100.0,
	}
	attributes := tyres.New(cfg, tyreTemps)

	// Act
	average := attributes.GeneralTemperature()

	// Assert
	expected := (70.0 + 80.0 + 90.0 + 100.0) / 4.0
	suite.InEpsilon(float32(expected), average, 0.001)
}

func (suite *TyreTestSuite) TestPositionsInCondition() {
	// Arrange
	cfg := &mockConfigProvider{optimal: 80.0, window: 10.0, margin: 5.0}
	tyreTemps := models.CornerSet{
		FrontLeft:  65.0, // Cold
		FrontRight: 80.0, // Optimal
		RearLeft:   95.0, // Hot
		RearRight:  0.0,  // Invalid
	}
	attributes := tyres.New(cfg, tyreTemps)

	// Act & Assert
	suite.Len(attributes.PositionsInCondition(tyres.ConditionCold), 1)
	suite.Len(attributes.PositionsInCondition(tyres.ConditionOptimal), 1)
	suite.Len(attributes.PositionsInCondition(tyres.ConditionHot), 1)
	suite.Len(attributes.PositionsInCondition(tyres.ConditionInvalid), 1)
}

func (suite *TyreTestSuite) TestConditionOptimal() {
	cfg := &mockConfigProvider{optimal: 80.0, window: 10.0, margin: 5.0}

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
			attributes := tyres.New(cfg, testCase.temps)

			// Assert
			suite.Equal(testCase.expected, attributes.ConditionOptimal())
		})
	}
}

func (suite *TyreTestSuite) TestOptimalWithWideWindow() {
	// Settings: centre=81, window=15, margin=3
	// Thresholds: optimalLower=73.5, optimalUpper=88.5, cold<70.5, hot>91.5
	cfg := &mockConfigProvider{optimal: 81.0, window: 15.0, margin: 3.0}

	testCases := []struct {
		name              string
		temps             models.CornerSet
		expectedOptimal   bool
		expectedCondition tyres.Condition
	}{
		{
			name: "All tyres at centre - should be optimal",
			temps: models.CornerSet{
				FrontLeft:  81.0,
				FrontRight: 81.0,
				RearLeft:   81.0,
				RearRight:  81.0,
			},
			expectedOptimal:   true,
			expectedCondition: tyres.ConditionOptimal,
		},
		{
			name: "All tyres at 76 - should be optimal",
			temps: models.CornerSet{
				FrontLeft:  76.0,
				FrontRight: 76.0,
				RearLeft:   76.0,
				RearRight:  76.0,
			},
			expectedOptimal:   true,
			expectedCondition: tyres.ConditionOptimal,
		},
		{
			name: "All tyres at 84 - should be optimal",
			temps: models.CornerSet{
				FrontLeft:  84.0,
				FrontRight: 84.0,
				RearLeft:   84.0,
				RearRight:  84.0,
			},
			expectedOptimal:   true,
			expectedCondition: tyres.ConditionOptimal,
		},
		{
			name: "Mixed temps 76-84 - should be optimal",
			temps: models.CornerSet{
				FrontLeft:  76.0,
				FrontRight: 84.0,
				RearLeft:   79.0,
				RearRight:  82.0,
			},
			expectedOptimal:   true,
			expectedCondition: tyres.ConditionOptimal,
		},
		{
			name: "All at lower bound 73.5 - should be optimal",
			temps: models.CornerSet{
				FrontLeft:  73.5,
				FrontRight: 73.5,
				RearLeft:   73.5,
				RearRight:  73.5,
			},
			expectedOptimal:   true,
			expectedCondition: tyres.ConditionOptimal,
		},
		{
			name: "All at upper bound 88.5 - should be optimal",
			temps: models.CornerSet{
				FrontLeft:  88.5,
				FrontRight: 88.5,
				RearLeft:   88.5,
				RearRight:  88.5,
			},
			expectedOptimal:   true,
			expectedCondition: tyres.ConditionOptimal,
		},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Arrange
			attributes := tyres.New(cfg, testCase.temps)

			// Act
			isOptimal := attributes.ConditionOptimal()
			generalCondition := attributes.GeneralCondition()

			// Assert
			suite.Equal(testCase.expectedOptimal, isOptimal, "ConditionOptimal()")
			suite.Equal(testCase.expectedCondition, generalCondition, "GeneralCondition()")

			// Also verify each individual tyre is optimal
			suite.Equal(tyres.ConditionOptimal, attributes.ConditionAtPosition(tyres.PositionFrontLeft), "FL condition")
			suite.Equal(tyres.ConditionOptimal, attributes.ConditionAtPosition(tyres.PositionFrontRight), "FR condition")
			suite.Equal(tyres.ConditionOptimal, attributes.ConditionAtPosition(tyres.PositionRearLeft), "RL condition")
			suite.Equal(tyres.ConditionOptimal, attributes.ConditionAtPosition(tyres.PositionRearRight), "RR condition")
		})
	}
}

func (suite *TyreTestSuite) TestConfigWatcherUpdatesThresholds() {
	// Arrange: Create tyres with default settings (centre=81, window=6, margin=3)
	// Thresholds: optimalLower=78, optimalUpper=84, cold<75, hot>87
	cfg := &mockConfigProvider{optimal: 81.0, window: 6.0, margin: 3.0}
	attributes := tyres.New(cfg, models.CornerSet{
		FrontLeft:  76.0,
		FrontRight: 84.0,
		RearLeft:   79.0,
		RearRight:  82.0,
	})

	// With window=6, 76°C average (80.25) is within 78-84, but individual FL at 76 is marginal.
	// GeneralCondition should be optimal since avg is in range and no tyre is hot/cold.
	suite.True(attributes.ConditionOptimal(), "should be optimal with initial config")

	// Act: Simulate user changing config to window=15 at runtime
	cfg.setWindow(15.0)

	// Wait for the config watcher to pick up the change
	time.Sleep(300 * time.Millisecond)

	// Verify new temps at edges of the wider window are now optimal
	attributes.SetTemperatures(models.CornerSet{
		FrontLeft:  74.0, // Below old optimalLower=78, within new optimalLower=73.5
		FrontRight: 88.0, // Above old optimalUpper=84, within new optimalUpper=88.5
		RearLeft:   81.0,
		RearRight:  81.0,
	})

	// Assert - with updated thresholds from watcher, all temps are within optimal range
	suite.True(attributes.ConditionOptimal(), "ConditionOptimal() should be true after config watcher update")
	suite.Equal(tyres.ConditionOptimal, attributes.GeneralCondition(), "GeneralCondition() should be optimal")
	suite.Equal(tyres.ConditionOptimal, attributes.ConditionAtPosition(tyres.PositionFrontLeft), "FL at 74 should be optimal with window=15")
	suite.Equal(tyres.ConditionOptimal, attributes.ConditionAtPosition(tyres.PositionFrontRight), "FR at 88 should be optimal with window=15")
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
