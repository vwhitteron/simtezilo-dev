package app

import (
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

type tyreCondition string

const (
	tyreConditionInvalid tyreCondition = "invalid"
	tyreConditionOptimal tyreCondition = "optimal"
	tyreConditionHot     tyreCondition = "hot"
	tyreConditionCold    tyreCondition = "cold"
)

type tyrePosition int

const (
	tyrePositionFrontLeft tyrePosition = iota
	tyrePositionFrontRight
	tyrePositionRearLeft
	tyrePositionRearRight
)

type tyreTempState struct {
	frontLeft  tyreCondition
	frontRight tyreCondition
	rearLeft   tyreCondition
	rearRight  tyreCondition
}

type tyreState struct {
	lastTempNotifyState tyreTempState // Last notified tyre temperature state
	lastTempNotifyTime  time.Time     // Last time tyre temperature notification was sent
}

// notifyTyreTemperature sends tyre temperature notifications over the pit radio.
// Reports: all-tyres conditions (optimal/hot/cold) or individual/axle hot tyres only.
func (a *App) notifyTyreTemperature() {
	if !a.config.GetTyreTemperatureMonitoring() {
		return
	}

	if a.pitRadioState == nil {
		return
	}

	// Don't send tyre temp notifications too frequently (minimum 30 seconds between notifications)
	if time.Since(a.pitRadioState.tyreState.lastTempNotifyTime) < 30*time.Second {
		return
	}

	tyreTemps := a.gtClient.Telemetry.TyreTemperatureCelsius()
	currentStates := a.calculateTyreStates(tyreTemps)

	// Check for any invalid temperatures
	if currentStates.frontLeft == tyreConditionInvalid ||
		currentStates.frontRight == tyreConditionInvalid ||
		currentStates.rearLeft == tyreConditionInvalid ||
		currentStates.rearRight == tyreConditionInvalid {
		return
	}

	message := a.generateTyreConditionMessage(currentStates)
	if message == "" {
		return // No notification needed
	}

	// Only send notification if the state has changed
	if !a.hasTyreStateChanged(a.pitRadioState.tyreState.lastTempNotifyState, currentStates) {
		return
	}

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
				Msg("Send tyre temp message")

			return
		}

		a.pitRadioState.tyreState.lastTempNotifyState = currentStates
		a.pitRadioState.tyreState.lastTempNotifyTime = time.Now()

		a.log.Info().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Str("front_left", string(currentStates.frontLeft)).
			Str("front_right", string(currentStates.frontRight)).
			Str("rear_left", string(currentStates.rearLeft)).
			Str("rear_right", string(currentStates.rearRight)).
			Float32("front_left_temp", tyreTemps.FrontLeft).
			Float32("front_right_temp", tyreTemps.FrontRight).
			Float32("rear_left_temp", tyreTemps.RearLeft).
			Float32("rear_right_temp", tyreTemps.RearRight).
			Msg("Send tyre temp message")
	}
}

// calculateIndividualTyreCondition determines the condition of a single tyre.
func (a *App) calculateIndividualTyreCondition(temp float32) tyreCondition {
	if temp <= 0 {
		return tyreConditionInvalid
	}

	idealMin := a.config.GetTyreTemperatureIdealMin()
	idealMax := a.config.GetTyreTemperatureIdealMax()
	coldThreshold := a.config.GetTyreTemperatureColdThreshold()
	hotThreshold := a.config.GetTyreTemperatureHotThreshold()

	switch {
	case temp < coldThreshold:
		return tyreConditionCold
	case temp > hotThreshold:
		return tyreConditionHot
	case temp >= idealMin && temp <= idealMax:
		return tyreConditionOptimal
	default:
		// In warming-up range, not classified as optimal but not cold/hot either
		return tyreConditionOptimal // Treat as optimal for simplification
	}
}

// calculateTyreStates determines the condition of all individual tyres.
func (a *App) calculateTyreStates(tyreTemps models.CornerSet) tyreTempState {
	return tyreTempState{
		frontLeft:  a.calculateIndividualTyreCondition(tyreTemps.FrontLeft),
		frontRight: a.calculateIndividualTyreCondition(tyreTemps.FrontRight),
		rearLeft:   a.calculateIndividualTyreCondition(tyreTemps.RearLeft),
		rearRight:  a.calculateIndividualTyreCondition(tyreTemps.RearRight),
	}
}

func getAllTyresCondition(states tyreTempState) (condition tyreCondition, allSame bool) {
	if states.frontLeft == states.frontRight &&
		states.frontRight == states.rearLeft &&
		states.rearLeft == states.rearRight {
		return states.frontLeft, true
	}

	return tyreConditionInvalid, false
}

// generateTyreConditionMessage generates appropriate tyre condition messages.
// Reports all-tyres conditions (optimal/hot/cold) or individual hot tyres.
func (a *App) generateTyreConditionMessage(states tyreTempState) string {
	// Check if all tyres have the same condition
	commonCondition, allSame := getAllTyresCondition(states)

	if allSame {
		switch commonCondition {
		case tyreConditionOptimal:
			return a.i18n.GetString(languagedb.RadioTyresOptimalTemp)
		case tyreConditionHot:
			return a.i18n.GetString(languagedb.RadioTyresOverTemp)
		case tyreConditionCold:
			return a.i18n.GetString(languagedb.RadioTyresUnderTemp)
		case tyreConditionInvalid:
			// shouldn't reach here, continue to individual checks
		}
	}

	// Not all same - collect messages for individual/axle hot tyres only
	messages := a.collectHotTyreMessages(states)
	if len(messages) == 0 {
		return ""
	}

	return strings.Join(messages, ", ")
}

// collectHotTyreMessages collects messages for hot tyres (individual or axle-level).
func (a *App) collectHotTyreMessages(states tyreTempState) []string {
	var messages []string

	// Check front axle
	if states.frontLeft == tyreConditionHot && states.frontRight == tyreConditionHot {
		// Both front tyres hot - report as general hot message
		messages = append(messages, a.i18n.GetString(languagedb.RadioTyresOverTemp))
	} else {
		// Check individual front tyres
		if states.frontLeft == tyreConditionHot {
			messages = append(messages, a.getHotTyreMessage(tyrePositionFrontLeft))
		}

		if states.frontRight == tyreConditionHot {
			messages = append(messages, a.getHotTyreMessage(tyrePositionFrontRight))
		}
	}

	// Check rear axle
	if states.rearLeft == tyreConditionHot && states.rearRight == tyreConditionHot {
		// Both rear tyres hot - report as general hot message
		messages = append(messages, a.i18n.GetString(languagedb.RadioTyresOverTemp))
	} else {
		// Check individual rear tyres
		if states.rearLeft == tyreConditionHot {
			messages = append(messages, a.getHotTyreMessage(tyrePositionRearLeft))
		}

		if states.rearRight == tyreConditionHot {
			messages = append(messages, a.getHotTyreMessage(tyrePositionRearRight))
		}
	}

	return messages
}

// getHotTyreMessage generates a message for a specific hot tyre.
func (a *App) getHotTyreMessage(position tyrePosition) string {
	var formatKey languagedb.Key

	switch position {
	case tyrePositionFrontLeft:
		formatKey = languagedb.RadioTyreOverTempFL
	case tyrePositionFrontRight:
		formatKey = languagedb.RadioTyreOverTempFR
	case tyrePositionRearLeft:
		formatKey = languagedb.RadioTyreOverTempRL
	case tyrePositionRearRight:
		formatKey = languagedb.RadioTyreOverTempRR
	default:
		return ""
	}

	return a.i18n.GetString(formatKey)
}

// hasTyreStateChanged compares the previous and current tyre states to determine if a notification should be sent.
func (a *App) hasTyreStateChanged(previousState, currentState tyreTempState) bool {
	return previousState.frontLeft != currentState.frontLeft ||
		previousState.frontRight != currentState.frontRight ||
		previousState.rearLeft != currentState.rearLeft ||
		previousState.rearRight != currentState.rearRight
}
