package app //nolint:testpackage // white-box testing

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
	"github.com/vwhitteron/simtezilo-dev/app/circuit"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/fuelrange"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	gttelemetry "github.com/zetetos/gt-telemetry"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

// --- Mocks and stubs ---

type pitRadioMock struct {
	messages []pitradio.Message
	failSend bool
}

func (m *pitRadioMock) BackgroundTask() {}
func (m *pitRadioMock) Close() error    { return nil }
func (m *pitRadioMock) Send(msg pitradio.Message) error {
	m.messages = append(m.messages, msg)

	if m.failSend {
		return errors.New("send failed")
	}

	return nil
}

// createTestI18n creates an i18n instance with test language data.
func createTestI18n() *i18n.I18n {
	// Create I18n instance with default English language
	// This will use the actual language files from the system
	langCode := "en"

	testI18n, err := i18n.New(&langCode, zerolog.Nop())
	if err != nil {
		panic("Failed to create test i18n: " + err.Error())
	}

	return testI18n
}

// fuelRangeMock implements fuelrange.Estimator interface for testing.
type fuelRangeMock struct {
	distanceMeters float64
	distanceLaps   float64
	usageRate      float64
}

// Ensure fuelRangeMock implements fuelrange.Estimator interface.
var _ fuelrange.Estimator = (*fuelRangeMock)(nil)

func (m *fuelRangeMock) Reset()                      {}
func (m *fuelRangeMock) ResetEstimate()              {}
func (m *fuelRangeMock) SetLive(_ bool)              {}
func (m *fuelRangeMock) Update(_ float64, _ float32) {}

func (m *fuelRangeMock) DistanceMeters() float64 {
	return m.distanceMeters
}

func (m *fuelRangeMock) DistanceLaps(lengthMeters float64) float64 {
	if m.distanceLaps > 0 {
		return m.distanceLaps
	}

	if lengthMeters > 0 && m.distanceMeters > 0 {
		return m.distanceMeters / lengthMeters
	}

	return 0
}

func (m *fuelRangeMock) UsageRatePerKm() float64 {
	return m.usageRate
}

// SetFuelRange allows tests to configure fuel range values.
func (m *fuelRangeMock) SetFuelRange(distanceMeters, distanceLaps, usageRatePerKm float64) {
	m.distanceMeters = distanceMeters
	m.distanceLaps = distanceLaps
	m.usageRate = usageRatePerKm
}

// circuitMock implements circuit.Manager interface for testing.
type circuitMock struct {
	name                 string
	lengthMeters         float64
	lapProgress          float64
	lapProgressRemaining float64
}

// Ensure circuitMock implements circuit.Manager interface.
var _ circuit.Manager = (*circuitMock)(nil)

func (m *circuitMock) Reset()                {}
func (m *circuitMock) ResetLapProgress()     {}
func (m *circuitMock) Name() string          { return m.name }
func (m *circuitMock) LengthMeters() float64 { return m.lengthMeters }

func (m *circuitMock) LapProgress() float64 {
	return m.lapProgress
}

// UpdateCircuit implements the circuit.Manager interface.
func (m *circuitMock) UpdateCircuit(_ float64, _ int16, _ models.Coordinate, _ models.CoordinateType) bool {
	// Mock implementation - just return false for simplicity
	return false
}

func (m *circuitMock) LapProgressRemaining() float64 {
	if m.lapProgressRemaining > 0 {
		return m.lapProgressRemaining
	}

	return 1.0 - m.lapProgress
}

// --- Helper Functions ---

func createBasicConfig() *config.Config {
	configJSON := []byte(`{
		"app": {
			"language": "en",
			"accent": "us"
		},
		"pitRadio": {
			"fuelRangeSafetyMarginLaps": 0.3,
			"fuelPreWarnNotifyLaps": 2.0,
			"fuelStrategyNotifyLaps": 5.0
		}
	}`)

	return config.NewFromJSON(configJSON, zerolog.Nop())
}

// --- Tests ---

type PitRadioTestSuite struct {
	suite.Suite

	app       *App
	pitRadio  pitRadioMock
	i18n      *i18n.I18n
	fuelRange fuelRangeMock
	circuit   circuitMock
}

func TestPitRadioTestSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(PitRadioTestSuite))
}

func (suite *PitRadioTestSuite) SetupTest() {
	gtClient, err := gttelemetry.New(gttelemetry.Options{})
	if err != nil {
		suite.FailNow("Failed to create GTClient", err)
	}

	suite.pitRadio = pitRadioMock{
		messages: []pitradio.Message{},
	}

	suite.i18n = createTestI18n()

	suite.fuelRange = fuelRangeMock{
		distanceMeters: 10000,
		distanceLaps:   0, // Will be calculated based on circuit length
	}

	suite.circuit = circuitMock{
		name:                 "Mock Circuit",
		lengthMeters:         1000,
		lapProgress:          0.0,
		lapProgressRemaining: 1.0,
	}

	// Use interface types with proper mock implementations
	suite.app = &App{
		config:   nil,
		gtClient: gtClient,
		state: appState{
			current: raceState{},
			last:    raceState{},
		},
		pitRadio:      &suite.pitRadio,
		pitRadioState: &pitRadioState{},
		i18n:          suite.i18n,
		circuit:       &suite.circuit,   // Use circuit mock
		fuelRange:     &suite.fuelRange, // Use fuelRange mock
	}
}

func (suite *PitRadioTestSuite) TestAccentPopulatedInPitRadioMessage() {
	// Arrange
	configJSON := []byte(`{"app": {"language": "en", "accent": "ie"}}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())

	// Act
	err := suite.app.pitRadio.Send(pitradio.Message{
		MessageType: pitradio.TextMessage,
		Text:        "Test message",
		Lang:        "en",
		Accent:      suite.app.config.GetAppAccent(),
	})

	// Assert
	suite.Require().NoError(err)
	suite.Require().Len(suite.pitRadio.messages, 1)

	message := suite.pitRadio.messages[0]
	suite.Equal("Test message", message.Text)
	suite.Equal("en", message.Lang)
	suite.Equal("ie", message.Accent)
}

func (suite *PitRadioTestSuite) TestDefaultAccentWhenNotConfigured() {
	// Arrange
	configJSON := []byte(`{"app": {"language": "en"}}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())

	// Act
	accent := suite.app.config.GetAppAccent()

	// Assert
	suite.Equal("us", accent)
}

func (suite *PitRadioTestSuite) TestNoNotifyOnRaceStart() {
	// Arrange
	suite.setupBasicRace(1, 10)
	suite.setupCircuit(0.1)
	suite.setupFuelRange(15.0) // Plenty of fuel
	suite.clearPitRadioState()

	// Act
	suite.app.notifyFuelWarnings()

	// Assert
	suite.Empty(suite.pitRadio.messages)
}

func (suite *PitRadioTestSuite) TestNotifyOutOfFuel() {
	// Arrange
	suite.setupBasicRace(8, 10)
	suite.setupCircuit(0.5)
	suite.setupFuelRange(0.0) // No fuel left
	suite.clearPitRadioState()
	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 0 // Empty tank

	// Act
	suite.app.notifyFuelWarnings()

	// Assert
	suite.Len(suite.pitRadio.messages, 1)
	message := suite.pitRadio.messages[0]
	suite.Equal(pitradio.TextMessage, message.MessageType)
	suite.NotEmpty(message.Text)
	suite.Equal("en", message.Lang)
	suite.True(suite.app.pitRadioState.fuelNotifyEmptyIssued)
}

func (suite *PitRadioTestSuite) TestNotifyFuelCritical() {
	// Arrange
	suite.setupBasicRace(8, 10)
	suite.setupCircuit(0.5)
	suite.setupFuelRange(0.4) // Critical fuel level
	suite.clearPitRadioState()

	// Act
	suite.app.notifyFuelWarnings()

	// Assert
	suite.Len(suite.pitRadio.messages, 1)
	message := suite.pitRadio.messages[0]
	suite.Equal(pitradio.TextMessage, message.MessageType)
	suite.NotEmpty(message.Text)
	suite.Equal("en", message.Lang)
	suite.Equal("us", message.Accent)
	suite.Equal(int16(8), suite.app.pitRadioState.lastNotifiedLapFuelCritical)
}

func (suite *PitRadioTestSuite) TestNotifyBoxForFuel() {
	// Arrange
	suite.setupBasicRace(8, 10)
	suite.setupCircuit(0.7)
	suite.setupFuelRange(1.5) // Just enough to trigger box warning
	suite.clearPitRadioState()
	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 7 // Ensure warning not sent this lap

	// Act
	suite.app.notifyFuelWarnings()

	// Assert
	suite.Len(suite.pitRadio.messages, 1)
	message := suite.pitRadio.messages[0]
	suite.Equal(pitradio.TextMessage, message.MessageType)
	suite.NotEmpty(message.Text)
	suite.Equal("en", message.Lang)
}

func (suite *PitRadioTestSuite) TestNotifyFuelPreWarn() {
	// Arrange
	suite.setupBasicRace(5, 10)
	suite.setupCircuit(0.5)
	suite.setupFuelRange(3.0) // Triggers pre-warning condition
	suite.clearPitRadioState()

	// Act
	suite.app.notifyFuelWarnings()

	// Assert
	suite.Len(suite.pitRadio.messages, 1)
	message := suite.pitRadio.messages[0]
	suite.Equal(pitradio.TextMessage, message.MessageType)
	suite.NotEmpty(message.Text)
	suite.Equal("en", message.Lang)
	suite.True(suite.app.pitRadioState.fuelNotifyPrewarnIssued)
}

func (suite *PitRadioTestSuite) TestNotifyFuelStrategyUpdateSentOnNotifyLaps() {
	// Arrange
	suite.setupBasicRace(10, 25) // Lap 10 is divisible by 5
	suite.setupCircuit(0.0)
	suite.setupFuelRange(12.0) // Less fuel than remaining laps
	suite.clearPitRadioState()
	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 5

	// Act
	suite.app.notifyFuelWarnings()

	// Assert
	suite.Len(suite.pitRadio.messages, 1)
	message := suite.pitRadio.messages[0]
	suite.Equal(pitradio.TextMessage, message.MessageType)
	suite.NotEmpty(message.Text)
	suite.Equal("en", message.Lang)
	suite.Equal(int16(10), suite.app.pitRadioState.lastNotifiedLapFuelStrategy)
}

func (suite *PitRadioTestSuite) TestNotifyFuelStrategyUpdateNotSentOnNonNotifyLaps() {
	// Arrange
	suite.setupBasicRace(13, 25) // Lap 13 is NOT divisible by 5
	suite.setupCircuit(0.0)
	suite.setupFuelRange(10.0) // Less fuel than remaining laps
	suite.clearPitRadioState()
	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 12

	// Act
	suite.app.notifyFuelWarnings()

	// Assert
	suite.Empty(suite.pitRadio.messages)
	suite.Equal(int16(12), suite.app.pitRadioState.lastNotifiedLapFuelStrategy)
}

// TestFuelRangeDataIsPopulated tests that fuel range interface can be properly configured with mock data.
func (suite *PitRadioTestSuite) TestFuelRangeDataIsPopulated() {
	// Arrange - Set up test data for fuel range validation
	expectedRangeMeters := 15000.0
	expectedUsageRate := 1.67
	circuitLength := 5000.0

	// Act - Configure fuel range with test data
	suite.fuelRange.SetFuelRange(expectedRangeMeters, 0, expectedUsageRate)

	// Assert - Verify fuel range data is correct
	suite.NotNil(suite.app.fuelRange, "Fuel range should be available")
	suite.InEpsilon(expectedRangeMeters, suite.app.fuelRange.DistanceMeters(), 0.001, "Distance should match")
	suite.InEpsilon(expectedRangeMeters/circuitLength, suite.app.fuelRange.DistanceLaps(circuitLength), 0.001, "Laps should be calculated correctly")
	suite.InEpsilon(expectedUsageRate, suite.app.fuelRange.UsageRatePerKm(), 0.001, "Usage rate should match")
}

func (suite *PitRadioTestSuite) TestFuelWarningsWithRealData() {
	// Arrange - Set up critical fuel scenario with mocks
	configJSON := []byte(`{"pitRadio": {"fuelRangeSafetyMarginLaps": 0.5}}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.setupBasicRace(8, 10) // Lap 8 of 10
	suite.setupCircuit(0.5)
	suite.setupFuelRange(1.2) // 1.2 laps remaining

	// Act - Check for fuel warnings
	suite.app.notifyFuelWarnings()

	// Assert - Should generate critical fuel warning
	suite.NotEmpty(suite.pitRadio.messages, "Should generate critical fuel warning")

	if len(suite.pitRadio.messages) > 0 {
		message := suite.pitRadio.messages[0]
		suite.Equal(pitradio.TextMessage, message.MessageType)
		suite.NotEmpty(message.Text, "Message should have text")
	}
}

func (suite *PitRadioTestSuite) TestNotifyCircuitChange() {
	// Arrange - Set up circuit change scenario
	configJSON := []byte(`{"app": {"language": "en", "accent": "us"}}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.app.pitRadioState.circuitName = "Old Circuit"
	suite.circuit.name = "New Circuit"

	// Act - Notify of circuit change
	suite.app.notifyCircuitChange()

	// Assert - Should send notification and update state
	suite.Len(suite.pitRadio.messages, 1, "Should send circuit change notification")
	suite.Equal("New Circuit", suite.app.pitRadioState.circuitName, "Should update stored circuit name")

	if len(suite.pitRadio.messages) > 0 {
		suite.Contains(suite.pitRadio.messages[0].Text, "New Circuit", "Should mention new circuit")
	}
}

func (suite *PitRadioTestSuite) TestNotifyCircuitChangeNotSentForSameCircuit() {
	// Arrange - Same circuit name
	suite.app.pitRadioState.circuitName = "Same Circuit"
	suite.circuit.name = "Same Circuit"

	// Act - Try to notify circuit change
	suite.app.notifyCircuitChange()

	// Assert - Should not send notification
	suite.Empty(suite.pitRadio.messages, "Should not send notification for same circuit")
}

func (suite *PitRadioTestSuite) TestPitRadioSendError() {
	// Arrange - Set up send failure scenario
	suite.pitRadio.failSend = true
	configJSON := []byte(`{"pitRadio": {"fuelRangeSafetyMarginLaps": 0.3}}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.setupBasicRace(8, 10)
	suite.setupCircuit(0.5)
	suite.setupFuelRange(0.2) // Very low fuel

	// Act - Try to send fuel warning (will fail)
	suite.app.notifyFuelWarnings()

	// Assert - Should handle error gracefully
	suite.Len(suite.pitRadio.messages, 1, "Should attempt to send message despite error")
}

func (suite *PitRadioTestSuite) TestDuplicateWarningsSuppressed() {
	// Arrange - Set up critical fuel scenario
	configJSON := []byte(`{"pitRadio": {"fuelRangeSafetyMarginLaps": 0.3}}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.setupBasicRace(8, 10)
	suite.setupCircuit(0.5)
	suite.setupFuelRange(0.2) // Very low fuel

	// Act - Send warning twice on same lap
	suite.app.notifyFuelWarnings()
	firstMessageCount := len(suite.pitRadio.messages)
	suite.app.notifyFuelWarnings()

	// Assert - Should not send duplicate
	suite.Len(suite.pitRadio.messages, firstMessageCount, "Should not send duplicate warnings on same lap")
}

func (suite *PitRadioTestSuite) TestPitRadioStateReset() {
	// Arrange - Set up existing state
	suite.app.pitRadioState.fuelNotifyEmptyIssued = true
	suite.app.pitRadioState.fuelNotifyPrewarnIssued = true
	suite.app.pitRadioState.lastNotifiedLapFuelCritical = 5
	suite.setupBasicRace(10, 20)
	suite.app.gtClient.Telemetry.RawTelemetry.GridPosition = 3

	// Act - Reset pit radio state
	suite.app.resetPitRadioState()

	// Assert - All state should be reset
	suite.False(suite.app.pitRadioState.fuelNotifyEmptyIssued, "Empty fuel flag should be reset")
	suite.False(suite.app.pitRadioState.fuelNotifyPrewarnIssued, "Pre-warn flag should be reset")
	suite.Equal(int16(0), suite.app.pitRadioState.lastNotifiedLapFuelCritical, "Critical lap counter should be reset")
	suite.Equal(int16(10), suite.app.pitRadioState.lastNotifiedLapNumber, "Should update lap number")
	suite.Equal(int16(3), suite.app.pitRadioState.lastNotifiedGridPosition, "Should update grid position")
}

func (suite *PitRadioTestSuite) TestFuelWarningPriority() {
	// Arrange - Set up multiple warning conditions (empty fuel wins)
	configJSON := []byte(`{"pitRadio": {"fuelRangeSafetyMarginLaps": 0.3, "fuelPreWarnNotifyLaps": 2.0, "fuelStrategyNotifyLaps": 5.0}}`)
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.setupBasicRace(10, 20) // Lap 10 (divisible by 5 = strategy lap)
	suite.setupCircuit(0.5)
	suite.setupFuelRange(0.2)                                 // Critical fuel
	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 0.0 // Empty - highest priority

	// Act - Check fuel warnings
	suite.app.notifyFuelWarnings()

	// Assert - Should prioritize empty fuel over other warnings
	suite.Len(suite.pitRadio.messages, 1, "Should send exactly one message")
	suite.True(suite.app.pitRadioState.fuelNotifyEmptyIssued, "Should mark empty fuel as notified")
}

// setupBasicRace configures a standard race scenario.
func (suite *PitRadioTestSuite) setupBasicRace(currentLap, totalLaps int16) {
	suite.app.config = createBasicConfig()
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = currentLap
	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = totalLaps
	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 50.0
	suite.app.gtClient.Telemetry.RawTelemetry.FuelCapacity = 100.0
}

// setupCircuit configures the circuit mock with given parameters.
func (suite *PitRadioTestSuite) setupCircuit(lapProgress float64) {
	suite.circuit.name = "Test Circuit"
	suite.circuit.lengthMeters = 5000.0
	suite.circuit.lapProgress = lapProgress
	suite.circuit.lapProgressRemaining = 1.0 - lapProgress
}

// setupFuelRange configures the fuel range mock with given parameters.
func (suite *PitRadioTestSuite) setupFuelRange(rangeLaps float64) {
	rangeMeters := rangeLaps * suite.circuit.lengthMeters
	suite.fuelRange.SetFuelRange(rangeMeters, rangeLaps, 2.0)
}

// clearPitRadioState resets all pit radio notification flags.
func (suite *PitRadioTestSuite) clearPitRadioState() {
	suite.app.pitRadioState = &pitRadioState{}
	suite.pitRadio.messages = []pitradio.Message{}
}
