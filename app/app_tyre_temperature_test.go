package app //nolint:testpackage // white-box testing

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	gttelemetry "github.com/zetetos/gt-telemetry"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

type TyreTemperatureTestSuite struct {
	suite.Suite

	app      *App
	pitRadio pitRadioMock
}

func TestTyreTemperatureTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(TyreTemperatureTestSuite))
}

func (suite *TyreTemperatureTestSuite) SetupTest() {
	gtClient, err := gttelemetry.New(gttelemetry.Options{})
	if err != nil {
		suite.FailNow("Failed to create GTClient", err)
	}

	suite.pitRadio = pitRadioMock{
		messages: []pitradio.Message{},
	}

	configJSON := []byte(`{
		"app": {
			"language": "en",
			"accent": "us"
		},
		"pitRadio": {
			"tyreTemperatureMonitoring": true,
			"tyreTemperatureOptimalCelsius": 81,
			"tyreTemperatureOperatingWindow": 6,
			"tyreTemperatureMarginCelsius": 3
		}
	}`)

	suite.app = &App{
		config:   config.NewFromJSON(configJSON, zerolog.Nop()),
		gtClient: gtClient,
		state: appState{
			current: raceState{lapNumber: 5},
			last:    raceState{},
		},
		pitRadio:      &suite.pitRadio,
		pitRadioState: &pitRadioState{},
		i18n:          createTestI18n(),
	}
}

func (suite *TyreTemperatureTestSuite) TestCalculationsForIndividualTyreConditions() {
	// Arrange
	suite.setupTyreTemperatureTestConfig()

	testCases := []struct { // Cold: < 70°C, Optimal: 75-85°C, Hot: > 90°C
		name        string
		temperature float32
		expected    tyreCondition
	}{
		{"Invalid temperature (zero)", 0.0, tyreConditionInvalid},
		{"Invalid temperature (negative)", -5.0, tyreConditionInvalid},
		{"Cold temperature", 60.0, tyreConditionCold},
		{"Just below cold threshold", 69.9, tyreConditionCold},
		{"Just at cold threshold", 70.0, tyreConditionOptimal}, // temp < coldThreshold, so 70.0 is NOT cold
		{"Just above cold threshold", 70.1, tyreConditionOptimal},
		{"Optimal minimum", 75.0, tyreConditionOptimal},
		{"Optimal center", 80.0, tyreConditionOptimal},
		{"Optimal maximum", 85.0, tyreConditionOptimal},
		{"Just below hot threshold", 90.0, tyreConditionOptimal}, // temp > hotThreshold, so 90.0 is NOT hot
		{"Just above hot threshold", 90.1, tyreConditionHot},
		{"Hot temperature", 95.0, tyreConditionHot},
		{"Very hot temperature", 110.0, tyreConditionHot},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Act
			result := suite.app.calculateIndividualTyreCondition(testCase.temperature)

			// Assert
			suite.Equal(testCase.expected, result, "Temperature %.2f°C should be %s", testCase.temperature, testCase.expected)
		})
	}
}

func (suite *TyreTemperatureTestSuite) TestCalculationsForAllTyreStates() {
	// Arrange
	suite.setupTyreTemperatureTestConfig()

	tyreTemps := models.CornerSet{
		FrontLeft:  65.0, // Cold (< 70°C)
		FrontRight: 80.0, // Optimal (75-85°C)
		RearLeft:   95.0, // Hot (> 90°C)
		RearRight:  0.0,  // Invalid
	}

	// Act
	result := suite.app.calculateTyreStates(tyreTemps)

	// Assert
	suite.Equal(tyreConditionCold, result.frontLeft, "Front left should be cold at 65°C")
	suite.Equal(tyreConditionOptimal, result.frontRight, "Front right should be optimal at 80°C")
	suite.Equal(tyreConditionHot, result.rearLeft, "Rear left should be hot at 95°C")
	suite.Equal(tyreConditionInvalid, result.rearRight, "Rear right should be invalid at 0°C")
}

func (suite *TyreTemperatureTestSuite) TestNoNotificationWhenRateLimited() {
	// Arrange
	suite.app.pitRadioState.tyreState.lastTempNotifyTime = time.Now().Add(-25 * time.Second)

	// Act
	suite.app.notifyTyreTemperature()

	// Assert
	suite.Empty(suite.pitRadio.messages)
}

func (suite *TyreTemperatureTestSuite) TestNoNotificationWhenTyreMonitoringDisabled() {
	// Arrange
	suite.setupDisabledTyreTemperatureConfig()

	// Act
	suite.app.notifyTyreTemperature()

	// Assert
	suite.Empty(suite.pitRadio.messages)
}

func (suite *TyreTemperatureTestSuite) TestDetectionOfTyreStateChanges() {
	// Arrange
	baseState := allTyresCondition(tyreConditionOptimal)

	testCases := []struct {
		name           string
		currentState   tyreTempState
		expectedChange bool
		description    string
	}{
		{
			name:           "Identical states",
			currentState:   allTyresCondition(tyreConditionOptimal),
			expectedChange: false,
			description:    "Should return false when states are identical",
		},
		{
			name:           "Front left changed",
			currentState:   individualTyreCondition(tyrePositionFrontLeft, tyreConditionHot, tyreConditionOptimal),
			expectedChange: true,
			description:    "Should return true when front left tyre condition changes",
		},
		{
			name:           "Front right changed",
			currentState:   individualTyreCondition(tyrePositionFrontRight, tyreConditionCold, tyreConditionOptimal),
			expectedChange: true,
			description:    "Should return true when front right tyre condition changes",
		},
		{
			name:           "Rear left changed",
			currentState:   individualTyreCondition(tyrePositionRearLeft, tyreConditionHot, tyreConditionOptimal),
			expectedChange: true,
			description:    "Should return true when rear left tyre condition changes",
		},
		{
			name:           "Rear right changed",
			currentState:   individualTyreCondition(tyrePositionRearRight, tyreConditionCold, tyreConditionOptimal),
			expectedChange: true,
			description:    "Should return true when rear right tyre condition changes",
		},
		{
			name:           "Multiple changes",
			currentState:   axleConditions(tyreConditionHot, tyreConditionCold),
			expectedChange: true,
			description:    "Should return true when multiple tyres change condition",
		},
		{
			name:           "All tyres changed to same condition",
			currentState:   allTyresCondition(tyreConditionHot),
			expectedChange: true,
			description:    "Should return true when all tyres change to different condition",
		},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Act
			result := suite.app.hasTyreStateChanged(baseState, testCase.currentState)

			// Assert
			suite.Equal(testCase.expectedChange, result, testCase.description)
		})
	}
}

func (suite *TyreTemperatureTestSuite) TestNotificationOfColdTyreStates() {
	// Arrange
	testCases := []struct {
		name            string
		tyreStates      tyreTempState
		expectedMessage string
		description     string
	}{
		{
			name:            "All tyres cold",
			tyreStates:      allTyresCondition(tyreConditionCold),
			expectedMessage: "Tyres under temp",
			description:     "Should report under temp when all tyres are cold",
		},
		{
			name:            "Individual cold tyre",
			tyreStates:      individualTyreCondition(tyrePositionFrontLeft, tyreConditionCold, tyreConditionOptimal),
			expectedMessage: "",
			description:     "Should not report individual cold tyres",
		},
		{
			name:            "Cold axle (front)",
			tyreStates:      axleConditions(tyreConditionCold, tyreConditionOptimal),
			expectedMessage: "",
			description:     "Should not report axle cold conditions",
		},
		{
			name:            "Cold axle (rear)",
			tyreStates:      axleConditions(tyreConditionOptimal, tyreConditionCold),
			expectedMessage: "",
			description:     "Should not report axle cold conditions",
		},
		{
			name:            "Individual hot tyre",
			tyreStates:      individualTyreCondition(tyrePositionFrontLeft, tyreConditionHot, tyreConditionOptimal),
			expectedMessage: "Front left tyre over temp",
			description:     "Should report specific tyre when individual tyre is hot",
		},
		{
			name:            "Hot axle (rear)",
			tyreStates:      axleConditions(tyreConditionOptimal, tyreConditionHot),
			expectedMessage: "Tyres over temp",
			description:     "Should report over temp when axle is hot",
		},
		{
			name:            "Hot axle (front)",
			tyreStates:      axleConditions(tyreConditionHot, tyreConditionOptimal),
			expectedMessage: "Tyres over temp",
			description:     "Should report over temp when axle is hot",
		},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Act
			message := suite.app.generateTyreConditionMessage(testCase.tyreStates)

			// Assert
			if testCase.expectedMessage == "" {
				suite.Empty(message, testCase.description)
			} else {
				suite.Equal(testCase.expectedMessage, message, testCase.description)
			}
		})
	}
}

func (suite *TyreTemperatureTestSuite) TestValidityOfConfigurationValues() {
	// Arrange
	suite.setupTyreTemperatureTestConfig()

	// Act - no action required

	// Assert
	// Feature enabled
	suite.True(suite.app.config.GetTyreTemperatureMonitoring())

	// Base configuration values
	suite.InEpsilon(float32(80.0), suite.app.config.GetTyreTemperatureOptimalCelsius(), 0.001)
	suite.InEpsilon(float32(10.0), suite.app.config.GetTyreTemperatureOperatingWindow(), 0.001)
	suite.InEpsilon(float32(5.0), suite.app.config.GetTyreTemperatureMarginCelsius(), 0.001)

	// Calculated values
	suite.InEpsilon(float32(75.0), suite.app.config.GetTyreTemperatureIdealMin(), 0.001)      // 80 - (10/2)
	suite.InEpsilon(float32(85.0), suite.app.config.GetTyreTemperatureIdealMax(), 0.001)      // 80 + (10/2)
	suite.InEpsilon(float32(70.0), suite.app.config.GetTyreTemperatureColdThreshold(), 0.001) // 80 - (10/2) - 5
	suite.InEpsilon(float32(90.0), suite.app.config.GetTyreTemperatureHotThreshold(), 0.001)  // 80 + (10/2) + 5
}

func (suite *TyreTemperatureTestSuite) TestTyreConditionNotificationMessages() {
	// Arrange
	testCases := []struct {
		name            string
		tyreStates      tyreTempState
		expectedMessage string
		description     string
	}{
		{
			name:            "All tyres optimal",
			tyreStates:      allTyresCondition(tyreConditionOptimal),
			expectedMessage: "Tyres optimal",
			description:     "Should report optimal when all tyres are in optimal range",
		},
		{
			name:            "All tyres hot",
			tyreStates:      allTyresCondition(tyreConditionHot),
			expectedMessage: "Tyres over temp",
			description:     "Should report over temp when all tyres are hot",
		},
		{
			name:            "All tyres cold",
			tyreStates:      allTyresCondition(tyreConditionCold),
			expectedMessage: "Tyres under temp",
			description:     "Should report under temp when all tyres are cold",
		},
		{
			name:            "Front axle hot",
			tyreStates:      axleConditions(tyreConditionHot, tyreConditionOptimal),
			expectedMessage: "Tyres over temp",
			description:     "Should report over temp when front axle is hot",
		},
		{
			name:            "Front axle cold",
			tyreStates:      axleConditions(tyreConditionCold, tyreConditionOptimal),
			expectedMessage: "",
			description:     "Should not report axle cold conditions",
		},
		{
			name:            "Rear axle hot",
			tyreStates:      axleConditions(tyreConditionOptimal, tyreConditionHot),
			expectedMessage: "Tyres over temp",
			description:     "Should report over temp when rear axle is hot",
		},
		{
			name:            "Rear axle cold",
			tyreStates:      axleConditions(tyreConditionOptimal, tyreConditionCold),
			expectedMessage: "",
			description:     "Should not report axle cold conditions",
		},
		{
			name:            "Individual front left hot",
			tyreStates:      individualTyreCondition(tyrePositionFrontLeft, tyreConditionHot, tyreConditionOptimal),
			expectedMessage: "Front left tyre over temp",
			description:     "Should report specific tyre when individual tyre is hot",
		},
		{
			name:            "Individual front right hot",
			tyreStates:      individualTyreCondition(tyrePositionFrontRight, tyreConditionHot, tyreConditionOptimal),
			expectedMessage: "Front right tyre over temp",
			description:     "Should report specific tyre when individual tyre is hot",
		},
		{
			name:            "Individual rear right cold",
			tyreStates:      individualTyreCondition(tyrePositionRearRight, tyreConditionCold, tyreConditionOptimal),
			expectedMessage: "",
			description:     "Should not report individual cold tyres",
		},
		{
			name:            "Individual rear left cold",
			tyreStates:      individualTyreCondition(tyrePositionRearLeft, tyreConditionCold, tyreConditionOptimal),
			expectedMessage: "",
			description:     "Should not report individual cold tyres",
		},
		{
			name:            "Individual rear left hot",
			tyreStates:      individualTyreCondition(tyrePositionRearLeft, tyreConditionHot, tyreConditionOptimal),
			expectedMessage: "Rear left tyre over temp",
			description:     "Should report specific tyre when individual rear tyre is hot",
		},
		{
			name:            "Individual rear right hot",
			tyreStates:      individualTyreCondition(tyrePositionRearRight, tyreConditionHot, tyreConditionOptimal),
			expectedMessage: "Rear right tyre over temp",
			description:     "Should report specific tyre when individual rear tyre is hot",
		},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Act
			message := suite.app.generateTyreConditionMessage(testCase.tyreStates)

			// Assert
			suite.Equal(testCase.expectedMessage, message, testCase.description)
		})
	}
}

// ********************************************************************************************************************
// Test helper functions
// ********************************************************************************************************************

// setupTyreTemperatureTestConfig provides configuration withexplicit, well-documented temperature values.
func (suite *TyreTemperatureTestSuite) setupTyreTemperatureTestConfig() {
	// Test configuration with explicit, easy-to-understand temperature values:
	// - Optimal: 80°C (center point)
	// - Operating Window: 10°C (80°C ± 5°C = 75-85°C optimal range)
	// - Margin: 5°C (additional margin for hot/cold thresholds)
	//
	// This results in clear temperature boundaries:
	// - Cold: < 70°C (75 - 5)
	// - Optimal: 75-85°C
	// - Hot: > 90°C (85 + 5)
	configJSON := []byte(`{
		"app": {
			"language": "en",
			"accent": "us"
		},
		"pitRadio": {
			"tyreTemperatureMonitoring": true,
			"tyreTemperatureOptimalCelsius": 80,
			"tyreTemperatureOperatingWindow": 10,
			"tyreTemperatureMarginCelsius": 5
		}
	}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
}

// setupDisabledTyreTemperatureConfig provides configuration with tyre temperature monitoring disabled.
func (suite *TyreTemperatureTestSuite) setupDisabledTyreTemperatureConfig() {
	configJSON := []byte(`{
		"app": {
			"language": "en",
			"accent": "us"
		},
		"pitRadio": {
			"tyreTemperatureMonitoring": false,
			"tyreTemperatureOptimalCelsius": 80,
			"tyreTemperatureOperatingWindow": 10,
			"tyreTemperatureMarginCelsius": 5
		}
	}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
}

// createTyreState creates a tyreTempState with specified conditions for each tyre.
func createTyreState(frontLeft, frontRight, rearLeft, rearRight tyreCondition) tyreTempState {
	return tyreTempState{
		frontLeft:  frontLeft,
		frontRight: frontRight,
		rearLeft:   rearLeft,
		rearRight:  rearRight,
	}
}

// allTyresCondition creates a tyreTempState with all tyres set to the same condition.
func allTyresCondition(condition tyreCondition) tyreTempState {
	return createTyreState(condition, condition, condition, condition)
}

// axleConditions creates a tyreTempState with front and rear axles set to specified conditions.
func axleConditions(frontCondition, rearCondition tyreCondition) tyreTempState {
	return createTyreState(frontCondition, frontCondition, rearCondition, rearCondition)
}

// individualTyreCondition creates a tyreTempState with one tyre set to a specific condition and others to a default condition.
func individualTyreCondition(position tyrePosition, condition tyreCondition, defaultCondition tyreCondition) tyreTempState { //nolint:unparam // usefule test helper arg
	switch position {
	case tyrePositionFrontLeft:
		return createTyreState(condition, defaultCondition, defaultCondition, defaultCondition)
	case tyrePositionFrontRight:
		return createTyreState(defaultCondition, condition, defaultCondition, defaultCondition)
	case tyrePositionRearLeft:
		return createTyreState(defaultCondition, defaultCondition, condition, defaultCondition)
	case tyrePositionRearRight:
		return createTyreState(defaultCondition, defaultCondition, defaultCondition, condition)
	default:
		return createTyreState(defaultCondition, defaultCondition, defaultCondition, defaultCondition)
	}
}
