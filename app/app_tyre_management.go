package app

import (
	"slices"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
	"github.com/vwhitteron/simtezilo-dev/app/tyres"
)

type tyreState struct {
	lastTempNotifyCondition tyres.Condition // Last notified tyre temperature condition
	lastTempNotifyTime      time.Time       // Last time tyre temperature notification was sent
}

// notifyTyreTemperature sends tyre temperature notifications over the pit radio.
// Reports: all-tyres conditions (optimal/hot/cold) or individual/axle hot tyres only.
func (a *App) notifyTyreTemperature() {
	if !a.config.GetTyreMonitoringEnabled() {
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

	if len(tyreAttrs.PositionsInCondition(tyres.ConditionInvalid)) > 0 {
		return
	}

	tyreCondition := tyreAttrs.GeneralCondition()

	// Only send notification if the state has changed
	if tyreCondition == a.pitRadioState.tyreState.lastTempNotifyCondition {
		return
	}

	message := a.generateTyreConditionMessage(tyreAttrs)
	if message == "" {
		return // No notification needed
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

		a.pitRadioState.tyreState.lastTempNotifyCondition = tyreCondition
		a.pitRadioState.tyreState.lastTempNotifyTime = time.Now()

		a.log.Info().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Str("condition", tyreCondition.String()).
			Str("cond_fl", tyreAttrs.ConditionAtPosition(tyres.PositionFrontLeft).String()).
			Str("cond_fr", tyreAttrs.ConditionAtPosition(tyres.PositionFrontRight).String()).
			Str("cond_rl", tyreAttrs.ConditionAtPosition(tyres.PositionRearLeft).String()).
			Str("cond_rr", tyreAttrs.ConditionAtPosition(tyres.PositionRearRight).String()).
			Float32("temp_avg", (tyreTemps.FrontLeft+tyreTemps.FrontRight+tyreTemps.RearLeft+tyreTemps.RearRight)/4).
			Float32("temp_fl", tyreAttrs.TemperatureAtPosition(tyres.PositionFrontLeft)).
			Float32("temp_fr", tyreAttrs.TemperatureAtPosition(tyres.PositionFrontRight)).
			Float32("temp_rl", tyreAttrs.TemperatureAtPosition(tyres.PositionRearLeft)).
			Float32("temp_rr", tyreAttrs.TemperatureAtPosition(tyres.PositionRearRight)).
			Msg("Send tyre temp message")
	}
}

// generateTyreConditionMessage generates tyre condition messages based on various combinations of tyre state.
func (a *App) generateTyreConditionMessage(states tyres.Tyre) string {
	condition := states.GeneralCondition()

	if message := a.getSimpleConditionMessage(condition); message != "" {
		return message
	}

	if condition == tyres.ConditionHot {
		return a.generateHotTyreMessage(states)
	}

	return ""
}

// getSimpleConditionMessage returns messages for simple tyre conditions that don't require detailed reporting.
func (a *App) getSimpleConditionMessage(condition tyres.Condition) string {
	switch condition { //nolint:exhaustive // Only handling simple cases
	case tyres.ConditionInvalid, tyres.ConditionMarginal:
		return ""
	case tyres.ConditionOptimal:
		return a.i18n.GetString(languagedb.RadioTyresOptimalTemp)
	case tyres.ConditionCold:
		return a.i18n.GetString(languagedb.RadioTyresUnderTemp)
	default:
		return ""
	}
}

// generateHotTyreMessage generates detailed messages for hot tyre conditions.
func (a *App) generateHotTyreMessage(states tyres.Tyre) string {
	hotTyres := states.PositionsInCondition(tyres.ConditionHot)
	baseMessage := a.i18n.GetString(languagedb.RadioTyresOverTemp)

	// Report general tyres overheating if 3+ tyres are hot
	if len(hotTyres) >= 3 {
		return baseMessage
	}

	// Generate detailed message for specific hot tyres
	return a.buildDetailedHotTyreMessage(states, hotTyres, baseMessage)
}

// buildDetailedHotTyreMessage builds a detailed message for specific hot tyre positions.
func (a *App) buildDetailedHotTyreMessage(states tyres.Tyre, hotTyres []tyres.Position, baseMessage string) string {
	message := baseMessage + ": "
	positionsCalled := a.addAxleGroupsToMessage(&message, states)
	a.addIndividualTyresToMessage(&message, hotTyres, positionsCalled)

	return message
}

// addAxleGroupsToMessage adds front/rear axle groups to the message and returns positions already called.
func (a *App) addAxleGroupsToMessage(message *string, states tyres.Tyre) []tyres.Position {
	frontAxle := states.ConditionAtPosition(tyres.PositionFront)
	rearAxle := states.ConditionAtPosition(tyres.PositionRear)

	if frontAxle == tyres.ConditionHot {
		*message += a.i18n.GetString(languagedb.RadioFront)

		return []tyres.Position{tyres.PositionFrontLeft, tyres.PositionFrontRight}
	}

	if rearAxle == tyres.ConditionHot {
		*message += a.i18n.GetString(languagedb.RadioRear)

		return []tyres.Position{tyres.PositionRearLeft, tyres.PositionRearRight}
	}

	return []tyres.Position{}
}

// addIndividualTyresToMessage adds individual hot tyres not already reported as part of an axle group.
func (a *App) addIndividualTyresToMessage(message *string, hotTyres []tyres.Position, positionsCalled []tyres.Position) {
	for _, position := range hotTyres {
		if slices.Contains(positionsCalled, position) {
			continue
		}

		if len(positionsCalled) > 0 {
			*message += ", "
		}

		*message += a.getPositionName(position)
		positionsCalled = append(positionsCalled, position)
	}
}

// getPositionName returns the localized name for a tyre position.
func (a *App) getPositionName(position tyres.Position) string {
	switch position { //nolint:exhaustive // front/rear handled above
	case tyres.PositionFrontLeft:
		return a.i18n.GetString(languagedb.RadioFrontLeft)
	case tyres.PositionFrontRight:
		return a.i18n.GetString(languagedb.RadioFrontRight)
	case tyres.PositionRearLeft:
		return a.i18n.GetString(languagedb.RadioRearLeft)
	case tyres.PositionRearRight:
		return a.i18n.GetString(languagedb.RadioRearRight)
	default:
		return ""
	}
}
