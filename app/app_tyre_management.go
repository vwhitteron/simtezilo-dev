package app

import (
	"slices"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/tyres"
)

type tyreState struct {
	lastTempNotifyState tyres.Tyre // Last notified tyre temperature state
	lastTempNotifyTime  time.Time  // Last time tyre temperature notification was sent
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
	tyreAttrs := tyres.New(
		a.config.GetTyreTemperatureOptimalCelsius(),
		a.config.GetTyreTemperatureOperatingWindow(),
		a.config.GetTyreTemperatureMarginCelsius(),
		tyreTemps,
	)

	if tyreAttrs.ContainsCondition(tyres.ConditionInvalid) {
		return
	}

	message := a.generateTyreConditionMessage(tyreAttrs)
	if message == "" {
		return // No notification needed
	}

	// Only send notification if the state has changed
	if tyreAttrs.Equal(a.pitRadioState.tyreState.lastTempNotifyState) {
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

		a.pitRadioState.tyreState.lastTempNotifyState = tyreAttrs
		a.pitRadioState.tyreState.lastTempNotifyTime = time.Now()

		a.log.Info().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Str("state_fl", tyreAttrs.Condition(tyres.PositionFrontLeft)).
			Str("state_fr", tyreAttrs.Condition(tyres.PositionFrontRight)).
			Str("state_rl", tyreAttrs.Condition(tyres.PositionRearLeft)).
			Str("state_rr", tyreAttrs.Condition(tyres.PositionRearRight)).
			Float32("temp_avg", (tyreTemps.FrontLeft+tyreTemps.FrontRight+tyreTemps.RearLeft+tyreTemps.RearRight)/4).
			Float32("temp_fl", tyreAttrs.Temperature(tyres.PositionFrontLeft)).
			Float32("temp_fr", tyreAttrs.Temperature(tyres.PositionFrontRight)).
			Float32("temp_rl", tyreAttrs.Temperature(tyres.PositionRearLeft)).
			Float32("temp_rr", tyreAttrs.Temperature(tyres.PositionRearRight)).
			Msg("Send tyre temp message")
	}
}

// generateTyreConditionMessage generates tyre condition messages based on various compbinations of tyre state.
func (a *App) generateTyreConditionMessage(states tyres.Tyre) string {
	if states.ConditionOptimal() {
		return a.i18n.GetString(languagedb.RadioTyresOptimalTemp)
	}

	conditionMap := states.Conditions()

	// Check if all 4 tyres share the same condition
	for condition, position := range conditionMap {
		if len(position) == 4 {
			switch condition { //nolint:exhaustive // Ignore optimal/invalid cases
			case tyres.ConditionHot:
				return a.i18n.GetString(languagedb.RadioTyresOverTemp)
			case tyres.ConditionCold:
				return a.i18n.GetString(languagedb.RadioTyresUnderTemp)
			}
		}
	}

	// Report individual/axle hot tyres only
	messages := a.collectHotTyreMessages(conditionMap)
	if len(messages) == 0 {
		return ""
	}

	return strings.Join(messages, ", ")
}

// collectHotTyreMessages collects messages for hot tyres (individual or axle-level).
func (a *App) collectHotTyreMessages(conditionMap tyres.ConditionMap) []string {
	var messages []string

	hotTyres, hasHotTyres := conditionMap[tyres.ConditionHot]
	if !hasHotTyres || len(hotTyres) == 0 {
		return messages
	}

	// Check for axle-level hot conditions first using the new constants for cleaner logic
	frontAxleHot := slices.Contains(hotTyres, tyres.PositionFrontLeft) && slices.Contains(hotTyres, tyres.PositionFrontRight)
	rearAxleHot := slices.Contains(hotTyres, tyres.PositionRearLeft) && slices.Contains(hotTyres, tyres.PositionRearRight)

	// Track which tyres we've already handled in axle messages
	handledTyres := make(map[tyres.Position]bool)

	if frontAxleHot {
		messages = append(messages, a.getHotTyreMessage(tyres.PositionFront))
		handledTyres[tyres.PositionFrontLeft] = true
		handledTyres[tyres.PositionFrontRight] = true
	} else if rearAxleHot {
		messages = append(messages, a.getHotTyreMessage(tyres.PositionRear))
		handledTyres[tyres.PositionRearLeft] = true
		handledTyres[tyres.PositionRearRight] = true
	}

	// Handle individual hot tyres that weren't part of an axle message
	for _, position := range hotTyres {
		if !handledTyres[position] {
			messages = append(messages, a.getHotTyreMessage(position))
		}
	}

	return messages
}

// getHotTyreMessage generates a message for a specific hot tyre.
func (a *App) getHotTyreMessage(position tyres.Position) string {
	var formatKey languagedb.Key

	switch position {
	case tyres.PositionFront:
		formatKey = languagedb.RadioTyresOverTempFront
	case tyres.PositionRear:
		formatKey = languagedb.RadioTyresOverTempRear
	case tyres.PositionFrontLeft:
		formatKey = languagedb.RadioTyreOverTempFL
	case tyres.PositionFrontRight:
		formatKey = languagedb.RadioTyreOverTempFR
	case tyres.PositionRearLeft:
		formatKey = languagedb.RadioTyreOverTempRL
	case tyres.PositionRearRight:
		formatKey = languagedb.RadioTyreOverTempRR
	default:
		return ""
	}

	return a.i18n.GetString(formatKey)
}
