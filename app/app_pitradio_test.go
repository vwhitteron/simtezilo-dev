package app

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
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

func (suite *PitRadioTestSuite) TestNotifyOutOfFuel() {
	// Arrange
	suite.app.pitRadioState.fuelNotifyPrewarnIssued = false
	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 0
	suite.app.gtClient.Telemetry.RawTelemetry.FuelCapacity = 1
	suite.app.circuit.lapDistanceMeters = 1000
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

func (suite *PitRadioTestSuite) TestNotifyFuelCritical() {
	// Arrange
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
	// const app.fuelRangeSafetyMarginLaps = 0.2
	suite.app.fuelRange.distanceLapsMA = 1.0 // laps safe: 1.0-0.2 = 0.8
	suite.app.circuit.lapProgress = 0.1      // laps until box: 0.8-(1.0-0.1) = -0.1
	suite.app.circuit.lapDistanceMeters = 1000
	want := "fuel critical"
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
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
	// const app.fuelRangeSafetyMarginLaps = 0.2
	suite.app.fuelRange.distanceLapsMA = 1.1 // laps safe: 1.1-0.2 = 0.9
	suite.app.circuit.lapProgress = 0.2      // laps until box: 0.9-(1.0-0.2) = 0.1
	suite.app.circuit.lapDistanceMeters = 1000
	want := "box for fuel"
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
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
	// const app.fuelPreWarnNotifyLaps = 3.0
	// const app.fuelRangeSafetyMarginLaps = 0.2
	suite.app.fuelRange.distanceLapsMA = 4.0 // laps safe: 4.0-0.2 = 3.8
	suite.app.circuit.lapProgress = 0.2      // laps until box: 3.8-(1.0-0.2) = 3.0
	suite.app.circuit.lapDistanceMeters = 1000
	want := "refuel in 3 laps"
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

func (suite *PitRadioTestSuite) TestNotifyFuelStrategyUpdate() {
	// Arrange
	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 5
	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = 20 // 17 laps remaining
	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 2
	// const app.fuelStrategyNotifyLaps = 5
	// const app.fuelRangeSafetyMarginLaps = 0.2
	suite.app.fuelRange.distanceLapsMA = 10.6 // laps safe: 10.6-0.2 = 10.4
	suite.app.circuit.lapProgress = 0.7       // laps until box: 10.4-(1.0-0.7) = 10.1
	suite.app.circuit.lapDistanceMeters = 1000
	want := "range 10, 15 remaining"
	suite.app.i18n.Keys = map[translations.Key]string{
		translations.RadioFuelRange: "range %d, %d remaining",
	}

	// Act
	suite.app.notifyFuelWarnings()
	got := len(suite.pitRadio.messages)
	suite.Require().Equal(1, got, "Expected one message to be sent")

	// Assert
	suite.Equal(want, suite.pitRadio.messages[0], "Expected box for fuel message")
}
