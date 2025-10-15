package app

import (
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

// updateCircuit checks for circuit changes and resets odometer and fuel range if needed.
func (a *App) updateCircuit() {
	lap := a.state.current.lapNumber
	lapTime := a.state.current.lastLapTime
	odometer := a.odometer.Read()
	coordinates := a.gtClient.Telemetry.PositionalMapCoordinates()

	if didUpdate := a.circuit.UpdateCircuit(odometer, lap, lapTime, coordinates, models.CoordinateTypeCircuit); didUpdate {
		a.state.last.lastLapTime = 0
	}
}

// notifyCircuitChange sends a circuit change notification over the pit radio.
func (a *App) notifyCircuitChange() {
	if a.pitRadioState == nil {
		return
	}

	circuitName := a.circuit.Name()
	if circuitName == "" || circuitName == "unknown" {
		return
	}

	if circuitName == a.pitRadioState.circuitName {
		return
	}

	a.pitRadioState.circuitName = circuitName

	message := "Circuit updated to " + circuitName
	if a.pitRadio != nil {
		err := a.pitRadio.Send(pitradio.Message{
			MessageType: pitradio.TextMessage,
			Text:        message,
			Lang:        a.i18n.LanguageCode(),
			Accent:      a.config.GetAppAccent(),
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Str("message", message).
				Msg("Send circuit change message")

			return
		}

		a.log.Debug().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Msg("Send circuit change message")
	}
}
