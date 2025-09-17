package app

import (
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/translations"
	telemetry "github.com/zetetos/gt-telemetry"
)

// --- Mocks and stubs ---

type pitRadioMock struct {
	messages []string
	failSend bool
}

func (m *pitRadioMock) Connect() error    { return nil }
func (m *pitRadioMock) Disconnect() error { return nil }
func (m *pitRadioMock) Send(msg string) error {
	m.messages = append(m.messages, msg)

	if m.failSend {
		return fmt.Errorf("send failed")
	}

	return nil
}

// --- Tests ---

type PitRadioTestSuite struct {
	suite.Suite
	app      *App
	pitRadio pitRadioMock
}

func TestPitRadioTestSuite(t *testing.T) {
	suite.Run(t, new(PitRadioTestSuite))
}

func (suite *PitRadioTestSuite) SetupTest() {
	gtClient, err := telemetry.NewGTClient(telemetry.GTClientOpts{})
	if err != nil {
		suite.FailNow("Failed to create GTClient", err)
	}

	suite.pitRadio = pitRadioMock{
		messages: []string{},
	}

	suite.app = &App{
		config:   nil,
		gtClient: gtClient,
		state: appState{
			current: raceState{},
			last:    raceState{},
		},
		pitRadio:      &suite.pitRadio,
		pitRadioState: &pitRadioState{},
		i18n:          &i18n.Language{Keys: map[translations.Key]string{}},
		circuit:       lapDistanceEstimation{},
	}
}

func (suite *PitRadioTestSuite) TestNoNotifyOnRaceStart() {
	// Arrange
	suite.app.pitRadioState.fuelNotifyPrewarnIssued = false
	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 99.9
	suite.app.gtClient.Telemetry.RawTelemetry.FuelCapacity = 100
	suite.app.circuit.lapDistanceMeters = 0
	want := "out of fuel"
	suite.app.i18n.Keys = map[translations.Key]string{
		translations.RadioOutOfFuel: want,
	}

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)
	suite.Require().Equal(1, got, "Expected one message to be sent")

	// Assert
	suite.Equal(want, suite.pitRadio.messages[0], "Expected out of fuel message")
}

func (suite *PitRadioTestSuite) TestNotifyOutOfFuel() {
	// Arrange
	want := "out of fuel"

	suite.app.pitRadioState.fuelNotifyPrewarnIssued = false
	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 0
	suite.app.gtClient.Telemetry.RawTelemetry.FuelCapacity = 1
	suite.app.circuit.lapDistanceMeters = 1000
	suite.app.i18n.Keys = map[translations.Key]string{
		translations.RadioOutOfFuel: want,
	}

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)
	suite.Require().Equal(1, got, "Expected one message to be sent")

	// Assert
	suite.Equal(want, suite.pitRadio.messages[0], "Expected out of fuel message")
}

func (suite *PitRadioTestSuite) TestNotifyFuelCritical() {
	// Arrange
	want := "fuel critical"

	configJSON := []byte(`{
		"pitRadio": {
			"fuelRangeSafetyMarginLaps": 0.3
		}
	}`)

	// FIXME
	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
	suite.app.fuelRange.distanceLapsMA = 1.0 // laps safe: 1.0-0.3 = 0.7
	suite.app.circuit.lapProgress = 0.1      // laps until box: 0.7-(1.0-0.1) = -0.2
	suite.app.circuit.lapDistanceMeters = 1000
	suite.app.i18n.Keys = map[translations.Key]string{
		translations.RadioFuelCritical: want,
	}

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)
	suite.Require().Equal(1, got, "Expected one message to be sent")

	// Assert
	suite.Equal(want, suite.pitRadio.messages[0], "Expected fuel critical message")
}

func (suite *PitRadioTestSuite) TestNotifyBoxForFuel() {
	// Arrange
	want := "box for fuel"

	configJSON := []byte(`{
		"pitRadio": {
			"fuelRangeSafetyMarginLaps": 0.3
		}
	}`)

	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
	suite.app.fuelRange.distanceLapsMA = 1.2 // laps safe: 1.2-0.3 = 0.9
	suite.app.circuit.lapProgress = 0.2      // laps until box: 0.9-(1.0-0.2) = 0.1
	suite.app.circuit.lapDistanceMeters = 1000
	suite.app.i18n.Keys = map[translations.Key]string{
		translations.RadioBoxForFuel: want,
	}

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)
	suite.Require().Equal(1, got, "Expected one message to be sent")

	// Assert
	suite.Equal(want, suite.pitRadio.messages[0], "Expected box for fuel message")
}

func (suite *PitRadioTestSuite) TestNotifyFuelPreWarn() {
	// Arrange
	want := "refuel in 3 laps"

	configJSON := []byte(`{
		"pitRadio": {
			"fuelPreWarnNotifyLaps": 4.0,
			"fuelRangeSafetyMarginLaps": 0.3,
		}
	}`)

	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
	suite.app.fuelRange.distanceLapsMA = 4.0 // laps safe: 4.0-0.3 = 3.7
	suite.app.circuit.lapProgress = 0.2      // laps until box: 3.7-(1.0-0.3) = 3.0
	suite.app.circuit.lapDistanceMeters = 1000
	suite.app.i18n.Keys = map[translations.Key]string{
		translations.RadioFuelPreWarn: "refuel in %d laps",
	}

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)
	suite.Require().Equal(1, got, "Expected one message to be sent")

	// Assert
	suite.Equal(want, suite.pitRadio.messages[0], "Expected box for fuel message")
}

func (suite *PitRadioTestSuite) TestNotifyFuelStrategyUpdateSentOnNotifyLaps() {
	// Arrange
	wantFmt := "range %d, %d remaining"
	want := fmt.Sprintf(wantFmt, 9, 10)

	configJSON := []byte(`{
		"pitRadio": {
			"fuelStrategyNotifyLaps": 5.0,
			"fuelRangeSafetyMarginLaps": 0.3,
		}
	}`)

	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 10
	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = 20 // 10 laps remaining
	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 5
	suite.app.fuelRange.distanceLapsMA = 10.1 // laps safe: 10.1-0.3 = 9.8
	suite.app.circuit.lapDistanceMeters = 1000
	suite.app.i18n.Keys = map[translations.Key]string{
		translations.RadioFuelRange: wantFmt,
	}

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)
	suite.Require().Equal(1, got, "Expected one message to be sent")

	// Assert
	suite.Equal(want, suite.pitRadio.messages[0], "Expected box for fuel message")
}

func (suite *PitRadioTestSuite) TestNotifyFuelStrategyUpdateNotSentOnNonNotifyLaps() {
	// Arrange
	want := 0

	configJSON := []byte(`{
		"pitRadio": {
			"fuelStrategyNotifyLaps": 6.0,
			"fuelRangeSafetyMarginLaps": 0.3
		}
	}`)

	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 13
	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = 20 // 8 laps remaining
	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 6
	suite.app.fuelRange.distanceLapsMA = 8.1 // laps safe: 8.1-0.3 = 7.8
	suite.app.circuit.lapDistanceMeters = 1000

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)

	// Assert
	suite.Equal(want, got, "Expected no messages to be sent")
}

type testFuelStrategy struct {
	currentLap uint16
	fuelRange  float64
	notifyLaps uint16
}

func (suite *PitRadioTestSuite) setupOutOfFuelStrategyNotification(raceLaps uint16, currentLap uint16, fuelRange float64) {
	configJSON := []byte(`{
		"pitRadio": {
			"fuelStrategyNotifyLaps": 6.0,
			"fuelRangeSafetyMarginLaps": 0.3
		}
	}`)

	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())

	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = currentLap
	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = raceLaps
	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 6
	suite.app.fuelRange.distanceLapsMA = 8.1 // laps safe: 8.1-0.3 = 7.8
	suite.app.circuit.lapDistanceMeters = 1000
}
