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
	"github.com/vwhitteron/simtezilo-dev/app/i18n/translations"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	gttelemetry "github.com/zetetos/gt-telemetry"
)

// --- Mocks and stubs ---

type pitRadioMock struct {
	messages []pitradio.Message
	failSend bool
}

func (m *pitRadioMock) Connect() error                         { return nil }
func (m *pitRadioMock) Disconnect() error                      { return nil }
func (m *pitRadioMock) TextMessageDispatcher(_ zerolog.Logger) {}
func (m *pitRadioMock) MessageDispatcher(_ zerolog.Logger)     {}
func (m *pitRadioMock) Send(msg pitradio.Message) error {
	m.messages = append(m.messages, msg)

	if m.failSend {
		return errors.New("send failed")
	}

	return nil
}
func (m *pitRadioMock) PlayAudioFile(_ string) error { return nil }
func (m *pitRadioMock) PlayRadioCheck() error        { return nil }

// --- Tests ---

type PitRadioTestSuite struct {
	suite.Suite

	app      *App
	pitRadio pitRadioMock
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
		circuit:       &circuit.Circuit{},
		fuelRange:     fuelrange.New(zerolog.Nop()),
	}
}

func (suite *PitRadioTestSuite) TestAccentPopulatedInPitRadioMessage() {
	// Arrange
	configJSON := []byte(`{
		"app": {
			"language": "en",
			"accent": "ie"
		}
	}`)

	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())

	// Act - Test that the config returns the correct accent
	accent := suite.app.config.GetAppAccent()

	// Assert
	suite.Equal("ie", accent, "Expected accent to be loaded from config")

	// Act - Test sending a message with accent
	err := suite.app.pitRadio.Send(pitradio.Message{
		Text:   "Test message",
		Lang:   "en",
		Accent: accent,
	})

	// Assert
	suite.Require().NoError(err)
	suite.Require().Len(suite.pitRadio.messages, 1, "Expected one message to be sent")

	message := suite.pitRadio.messages[0]
	suite.Equal("Test message", message.Text)
	suite.Equal("en", message.Lang)
	suite.Equal("ie", message.Accent, "Expected accent to be set in message")
}

func (suite *PitRadioTestSuite) TestDefaultAccentWhenNotConfigured() {
	// Arrange - Config without accent field
	configJSON := []byte(`{
		"app": {
			"language": "en"
		}
	}`)

	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())

	// Act
	accent := suite.app.config.GetAppAccent()

	// Assert
	suite.Equal("us", accent, "Expected default accent to be 'us' when not configured")
}

// func (suite *PitRadioTestSuite) TestNoNotifyOnRaceStart() {
// 	// Arrange
// 	suite.app.pitRadioState.fuelNotifyPrewarnIssued = false
// 	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 99.9
// 	suite.app.gtClient.Telemetry.RawTelemetry.FuelCapacity = 100
// 	suite.app.lapDistance.lapDistanceMeters = 0
// 	want := "out of fuel"
// 	suite.app.i18n.Keys = map[translations.Key]string{
// 		translations.RadioOutOfFuel: want,
// 	}

// 	// Act
// 	suite.app.notifyFuelWarnings2()
// 	got := len(suite.pitRadio.messages)
// 	suite.Require().Equal(1, got, "Expected one message to be sent")

// 	// Assert
// 	suite.Equal(want, suite.pitRadio.messages[0], "Expected out of fuel message")
// }

// func (suite *PitRadioTestSuite) TestNotifyOutOfFuel() {
// 	// Arrange
// 	want := "out of fuel"

// 	suite.app.pitRadioState.fuelNotifyPrewarnIssued = false
// 	suite.app.gtClient.Telemetry.RawTelemetry.FuelLevel = 0
// 	suite.app.gtClient.Telemetry.RawTelemetry.FuelCapacity = 1
// 	suite.app.lapDistance.lapDistanceMeters = 1000
// 	suite.app.i18n.Keys = map[translations.Key]string{
// 		translations.RadioOutOfFuel: want,
// 	}

// 	// Act
// 	suite.app.notifyFuelWarnings2()
// 	got := len(suite.pitRadio.messages)
// 	suite.Require().Equal(1, got, "Expected one message to be sent")

// 	// Assert
// 	suite.Equal(want, suite.pitRadio.messages[0], "Expected out of fuel message")
// }

// func (suite *PitRadioTestSuite) TestNotifyFuelCritical() {
// 	// Arrange
// 	want := "fuel critical"

// 	configJSON := []byte(`{
// 		"pitRadio": {
// 			"fuelRangeSafetyMarginLaps": 0.3
// 		}
// 	}`)

// 	// FIXME
// 	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
// 	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
// 	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
// 	suite.app.fuelRange.distanceLapsMA = 1.0 // laps safe: 1.0-0.3 = 0.7
// 	suite.app.lapDistance.lapProgress = 0.1  // laps until box: 0.7-(1.0-0.1) = -0.2
// 	suite.app.lapDistance.lapDistanceMeters = 1000
// 	suite.app.i18n.Keys = map[translations.Key]string{
// 		translations.RadioFuelCritical: want,
// 	}

// 	// Act
// 	suite.app.notifyFuelWarnings2()
// 	got := len(suite.pitRadio.messages)
// 	suite.Require().Equal(1, got, "Expected one message to be sent")

// 	// Assert
// 	suite.Equal(want, suite.pitRadio.messages[0], "Expected fuel critical message")
// }

// func (suite *PitRadioTestSuite) TestNotifyBoxForFuel() {
// 	// Arrange
// 	want := "box for fuel"

// 	configJSON := []byte(`{
// 		"pitRadio": {
// 			"fuelRangeSafetyMarginLaps": 0.3
// 		}
// 	}`)

// 	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
// 	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
// 	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
// 	suite.app.fuelRange.distanceLapsMA = 1.2 // laps safe: 1.2-0.3 = 0.9
// 	suite.app.lapDistance.lapProgress = 0.2  // laps until box: 0.9-(1.0-0.2) = 0.1
// 	suite.app.lapDistance.lapDistanceMeters = 1000
// 	suite.app.i18n.Keys = map[translations.Key]string{
// 		translations.RadioBoxForFuel: want,
// 	}

// 	// Act
// 	suite.app.notifyFuelWarnings2()
// 	got := len(suite.pitRadio.messages)
// 	suite.Require().Equal(1, got, "Expected one message to be sent")

// 	// Assert
// 	suite.Equal(want, suite.pitRadio.messages[0], "Expected box for fuel message")
// }

// func (suite *PitRadioTestSuite) TestNotifyFuelPreWarn() {
// 	// Arrange
// 	want := "refuel in 3 laps"

// 	configJSON := []byte(`{
// 		"pitRadio": {
// 			"fuelPreWarnNotifyLaps": 4.0,
// 			"fuelRangeSafetyMarginLaps": 0.3,
// 		}
// 	}`)

// 	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
// 	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 2
// 	suite.app.pitRadioState.lastNotifiedLapFuelWarning = 1
// 	suite.app.fuelRange.distanceLapsMA = 4.0 // laps safe: 4.0-0.3 = 3.7
// 	suite.app.lapDistance.lapProgress = 0.2  // laps until box: 3.7-(1.0-0.3) = 3.0
// 	suite.app.lapDistance.lapDistanceMeters = 1000
// 	suite.app.i18n.Keys = map[translations.Key]string{
// 		translations.RadioFuelPreWarn: "refuel in %d laps",
// 	}

// 	// Act
// 	suite.app.notifyFuelWarnings2()
// 	got := len(suite.pitRadio.messages)
// 	suite.Require().Equal(1, got, "Expected one message to be sent")

// 	// Assert
// 	suite.Equal(want, suite.pitRadio.messages[0], "Expected box for fuel message")
// }

// func (suite *PitRadioTestSuite) TestNotifyFuelStrategyUpdateSentOnNotifyLaps() {
// 	// Arrange
// 	wantFmt := "range %d, %d remaining"
// 	want := fmt.Sprintf(wantFmt, 9, 10)

// 	configJSON := []byte(`{
// 		"pitRadio": {
// 			"fuelStrategyNotifyLaps": 5.0,
// 			"fuelRangeSafetyMarginLaps": 0.3,
// 		}
// 	}`)

// 	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
// 	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 10
// 	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = 20 // 10 laps remaining
// 	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 5
// 	suite.app.fuelRange.distanceLapsMA = 10.1 // laps safe: 10.1-0.3 = 9.8
// 	suite.app.lapDistance.lapDistanceMeters = 1000
// 	suite.app.i18n.Keys = map[translations.Key]string{
// 		translations.RadioFuelRange: wantFmt,
// 	}

// 	// Act
// 	suite.app.notifyFuelWarnings2()
// 	got := len(suite.pitRadio.messages)
// 	suite.Require().Equal(1, got, "Expected one message to be sent")

// 	// Assert
// 	suite.Equal(want, suite.pitRadio.messages[0], "Expected box for fuel message")
// }

// func (suite *PitRadioTestSuite) TestNotifyFuelStrategyUpdateNotSentOnNonNotifyLaps() {
// 	// Arrange
// 	want := 0

// 	configJSON := []byte(`{
// 		"pitRadio": {
// 			"fuelStrategyNotifyLaps": 6.0,
// 			"fuelRangeSafetyMarginLaps": 0.3
// 		}
// 	}`)

// 	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())
// 	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = 13
// 	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = 20 // 8 laps remaining
// 	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 6
// 	suite.app.fuelRange.distanceLapsMA = 8.1 // laps safe: 8.1-0.3 = 7.8
// 	suite.app.lapDistance.lapDistanceMeters = 1000

// 	// Act
// 	suite.app.notifyFuelWarnings2()
// 	got := len(suite.pitRadio.messages)

// 	// Assert
// 	suite.Equal(want, got, "Expected no messages to be sent")
// }

// func (suite *PitRadioTestSuite) setupOutOfFuelStrategyNotification(raceLaps uint16, currentLap uint16, fuelRange float64) {
// 	configJSON := []byte(`{
// 		"pitRadio": {
// 			"fuelStrategyNotifyLaps": 6.0,
// 			"fuelRangeSafetyMarginLaps": 0.3
// 		}
// 	}`)

// 	suite.app.config = config.NewFromJSON(configJSON, zerolog.Nop())

// 	suite.app.gtClient.Telemetry.RawTelemetry.CurrentLap = currentLap
// 	suite.app.gtClient.Telemetry.RawTelemetry.RaceLaps = raceLaps
// 	suite.app.pitRadioState.lastNotifiedLapFuelStrategy = 6
// 	suite.app.fuelRange.distanceLapsMA = 8.1 // laps safe: 8.1-0.3 = 7.8
// 	suite.app.circuit.lapDistanceMeters = 1000
// }
