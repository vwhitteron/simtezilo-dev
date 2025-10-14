package app //nolint:testpackage // white-box testing

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/tyres"
	gttelemetry "github.com/zetetos/gt-telemetry"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

type TyreIntegrationTestSuite struct {
	suite.Suite

	app      *App
	pitRadio pitRadioMock
}

func TestTyreIntegrationTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(TyreIntegrationTestSuite))
}

func (suite *TyreIntegrationTestSuite) SetupTest() {
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

func (suite *TyreIntegrationTestSuite) TestNoNotificationWhenRateLimited() {
	// Arrange
	suite.app.pitRadioState.tyreState.lastTempNotifyTime = time.Now().Add(-25 * time.Second)

	// Act
	suite.app.notifyTyreTemperature()

	// Assert
	suite.Empty(suite.pitRadio.messages)
}

func (suite *TyreIntegrationTestSuite) TestNoNotificationWhenTyreMonitoringDisabled() {
	// Arrange
	suite.setupDisabledTyreTemperatureConfig()

	// Act
	suite.app.notifyTyreTemperature()

	// Assert
	suite.Empty(suite.pitRadio.messages)
}

func (suite *TyreIntegrationTestSuite) TestTyreConditionNotificationMessages() {
	// Arrange
	testCases := []struct {
		name            string
		temps           models.CornerSet
		expectedMessage string
		description     string
	}{
		{
			name: "All tyres optimal",
			temps: models.CornerSet{
				FrontLeft:  81.0,
				FrontRight: 81.0,
				RearLeft:   81.0,
				RearRight:  81.0,
			},
			expectedMessage: "Tyres optimal",
			description:     "Should report optimal when all tyres are in optimal range",
		},
		{
			name: "All tyres hot",
			temps: models.CornerSet{
				FrontLeft:  95.0,
				FrontRight: 95.0,
				RearLeft:   95.0,
				RearRight:  95.0,
			},
			expectedMessage: "Tyres over temp",
			description:     "Should report over temp when all tyres are hot",
		},
		{
			name: "All tyres cold",
			temps: models.CornerSet{
				FrontLeft:  65.0,
				FrontRight: 65.0,
				RearLeft:   65.0,
				RearRight:  65.0,
			},
			expectedMessage: "Tyres under temp",
			description:     "Should report under temp when all tyres are cold",
		},
		{
			name: "Individual front left hot",
			temps: models.CornerSet{
				FrontLeft:  95.0, // Hot
				FrontRight: 81.0, // Optimal
				RearLeft:   81.0, // Optimal
				RearRight:  81.0, // Optimal
			},
			expectedMessage: "Front left tyre over temp",
			description:     "Should report specific tyre when individual tyre is hot",
		},
		{
			name: "Individual front right hot",
			temps: models.CornerSet{
				FrontLeft:  81.0, // Optimal
				FrontRight: 95.0, // Hot
				RearLeft:   81.0, // Optimal
				RearRight:  81.0, // Optimal
			},
			expectedMessage: "Front right tyre over temp",
			description:     "Should report specific tyre when individual tyre is hot",
		},
		{
			name: "Individual rear left hot",
			temps: models.CornerSet{
				FrontLeft:  81.0, // Optimal
				FrontRight: 81.0, // Optimal
				RearLeft:   95.0, // Hot
				RearRight:  81.0, // Optimal
			},
			expectedMessage: "Rear left tyre over temp",
			description:     "Should report specific tyre when individual rear tyre is hot",
		},
		{
			name: "Individual rear right hot",
			temps: models.CornerSet{
				FrontLeft:  81.0, // Optimal
				FrontRight: 81.0, // Optimal
				RearLeft:   81.0, // Optimal
				RearRight:  95.0, // Hot
			},
			expectedMessage: "Rear right tyre over temp",
			description:     "Should report specific tyre when individual rear tyre is hot",
		},
		{
			name: "Front axle hot",
			temps: models.CornerSet{
				FrontLeft:  95.0, // Hot
				FrontRight: 95.0, // Hot
				RearLeft:   81.0, // Optimal
				RearRight:  81.0, // Optimal
			},
			expectedMessage: "Front tyres over temp",
			description:     "Should report front-specific message when front axle is hot",
		},
		{
			name: "Rear axle hot",
			temps: models.CornerSet{
				FrontLeft:  81.0, // Optimal
				FrontRight: 81.0, // Optimal
				RearLeft:   95.0, // Hot
				RearRight:  95.0, // Hot
			},
			expectedMessage: "Rear tyres over temp",
			description:     "Should report rear-specific message when rear axle is hot",
		},
		{
			name: "Individual cold tyre - no message",
			temps: models.CornerSet{
				FrontLeft:  65.0, // Cold
				FrontRight: 81.0, // Optimal
				RearLeft:   81.0, // Optimal
				RearRight:  81.0, // Optimal
			},
			expectedMessage: "",
			description:     "Should not report individual cold tyres",
		},
		{
			name: "Cold axle - no message",
			temps: models.CornerSet{
				FrontLeft:  65.0, // Cold
				FrontRight: 65.0, // Cold
				RearLeft:   81.0, // Optimal
				RearRight:  81.0, // Optimal
			},
			expectedMessage: "",
			description:     "Should not report axle cold conditions",
		},
	}

	for _, testCase := range testCases {
		suite.Run(testCase.name, func() {
			// Create tyre attributes from the test temperatures
			attributes := tyres.New(
				suite.app.config.GetTyreTemperatureOptimalCelsius(),
				suite.app.config.GetTyreTemperatureOperatingWindow(),
				suite.app.config.GetTyreTemperatureMarginCelsius(),
				testCase.temps,
			)

			// Act
			message := suite.app.generateTyreConditionMessage(attributes)

			// Assert
			suite.Equal(testCase.expectedMessage, message, testCase.description)
		})
	}
}

// ********************************************************************************************************************
// Test helper functions
// ********************************************************************************************************************

// setupTyreTemperatureTestConfig provides configuration with explicit, well-documented temperature values.
func (suite *TyreIntegrationTestSuite) setupTyreTemperatureTestConfig() {
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
func (suite *TyreIntegrationTestSuite) setupDisabledTyreTemperatureConfig() {
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
