package app

import (
	"fmt"
	"strings"
	"time"
)

const positionDebounceTime = 3 * time.Second
const messagePause = 3 * time.Second

func (a *App) resetPitRadioState() {
	a.pitRadioState = &pitRadioState{
		lastNotifiedLapNumber:  a.gtClient.Telemetry.CurrentLap(),
		lastNotifiedLapTime:    a.gtClient.Telemetry.LastLaptime(),
		lastNotifiedPosition:   a.gtClient.Telemetry.StartingPosition(), // TODO: switch to GridPosition when gt-telemetry updated
		positionNotifyDebounce: time.Now().Add(24 * time.Hour),

		// Initialize fuel monitoring
		lastFuelLevel:      a.gtClient.Telemetry.FuelLevelPercent(),
		fuelUsageHistory:   make([]float32, 0, 5),
		maxFuelHistorySize: 5,
		isTrackingDistance: true,
	}

	// Initialize lap distance tracking
	a.pitRadioState.lastPosition.X = a.gtClient.Telemetry.PositionalMapCoordinates().X
	a.pitRadioState.lastPosition.Y = a.gtClient.Telemetry.PositionalMapCoordinates().Y
	a.pitRadioState.lastPosition.Z = a.gtClient.Telemetry.PositionalMapCoordinates().Z
	a.pitRadioState.lapDistanceTracked = 0

	a.log.Info().
		Str("component", "app").
		Str("state", fmt.Sprintf("%+v", a.state)).
		Msg("state reset")
}

func (a *App) sendPitRadioMessage() {
	if a.pitRadio == nil {
		return
	}

	if !a.telemetryIsActive() {
		return
	}

	if a.timeOfDayHasReset() {
		a.resetPitRadioState()
	}

	if a.pitRadioState == nil {
		a.resetPitRadioState()

		return
	}

	// Update fuel monitoring
	a.updateFuelMonitoring()

	// Check for fuel warnings
	a.checkFuelWarnings()

	if a.positionHasChanged() {
		a.notifyPosition()
	}

	if a.isNewLap() {
		a.notifyLapTime()

		time.Sleep(messagePause)

		a.notifyLapNumber()

		a.pitRadioState.lastNotifiedLapNumber = a.state.current.currentLapNumber
		a.pitRadioState.lastNotifiedLapTime = a.state.current.lastLapTime
	}
}

// isNewLap checks for new lap using the dedicated pit radio state tracker
func (a *App) isNewLap() bool {
	if a.pitRadioState == nil {
		return false
	}

	if a.state.current.currentLapNumber <= 0 {
		return false
	}

	// Check if current lap is greater than the last tracked lap for pit radio
	// and ensure we have valid lap data
	return a.state.current.currentLapNumber > a.pitRadioState.lastNotifiedLapNumber
}

// positionHasChanged checks if the grid position has changed since the last update
func (a *App) positionHasChanged() bool {
	if a.pitRadioState == nil {
		return false
	}

	if a.state.current.currentLapNumber <= 0 {
		return false
	}

	position := a.gtClient.Telemetry.StartingPosition()

	if position <= 0 {
		return false
	}

	if position == a.pitRadioState.lastNotifiedPosition {
		return false
	} else if a.pitRadioState.currentPosition != position {
		a.pitRadioState.currentPosition = position
		a.pitRadioState.positionNotifyDebounce = time.Now().Add(positionDebounceTime)

		a.log.Debug().
			Str("component", "discord").
			Int16("new_position", position).
			Int16("old_position", a.pitRadioState.lastNotifiedPosition).
			Msg("Position change")
	}

	// Debounce position changes until time delay reached
	if time.Now().Before(a.pitRadioState.positionNotifyDebounce) {
		return false
	}

	// Reset debounce timer
	a.pitRadioState.positionNotifyDebounce = time.Now().Add(24 * time.Hour)
	a.pitRadioState.lastNotifiedPosition = a.pitRadioState.currentPosition

	return true
}

func (a *App) notifyPosition() {
	message := fmt.Sprintf("P%d", a.pitRadioState.currentPosition)

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Failed to send position change message")
		} else {
			a.log.Debug().
				Str("component", "discord").
				Str("message", message).
				Int16("lap", a.state.current.currentLapNumber).
				Msg("Position change message sent")
		}
	}
}

// notifyLapTime sends lap time notifications to Discord
func (a *App) notifyLapTime() {
	if a.state.current.lastLapTime <= 0 {
		return
	}

	var message string
	bestLapTime := a.gtClient.Telemetry.BestLaptime()

	// TODO: add config option to notify all laps or best lap only
	if bestLapTime > 0 && a.state.current.lastLapTime <= bestLapTime {
		message = "lap record. " + notifyDuration(a.state.current.lastLapTime)
	} else if a.state.current.lastLapTime > bestLapTime {
		message = "Down " + notifyDuration(a.state.current.lastLapTime-bestLapTime) + " seconds"
	} else {
		message = notifyDuration(a.state.current.lastLapTime)
	}

	// Send lap time message to Discord
	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Failed to send lap time message")
		} else {
			a.log.Debug().
				Str("component", "discord").
				Str("message", message).
				Int16("lap", a.state.current.currentLapNumber).
				Dur("lapTime", a.state.current.lastLapTime).
				Msg("Lap time message sent")
		}
	}
}

func (a *App) notifyLapNumber() {
	if a.state.current.currentLapNumber == 0 {
		return
	}

	raceLaps := int16(a.gtClient.Telemetry.RaceLaps())
	longRace := raceLaps > 10
	lapsRemaining := raceLaps - a.state.current.currentLapNumber + 1
	raceProgressPercent := int8(100 * float64(a.state.current.currentLapNumber) / float64(raceLaps))

	var currentQuarter int8
	switch {
	case raceProgressPercent >= 75:
		currentQuarter = 3
	case raceProgressPercent >= 50:
		currentQuarter = 2
	case raceProgressPercent >= 25:
		currentQuarter = 1
	default:
		currentQuarter = 0
	}

	message := ""
	switch {
	case lapsRemaining <= 0:
		message = "race complete"
	case a.state.current.currentLapNumber == raceLaps:
		message = "final lap"
	case lapsRemaining <= 3 && longRace:
		message = fmt.Sprintf("%d laps remaining", lapsRemaining)
	case currentQuarter > a.pitRadioState.lastRaceProgress && currentQuarter == 3 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.currentLapNumber, lapsRemaining)
	case currentQuarter > a.pitRadioState.lastRaceProgress && currentQuarter == 2:
		message = fmt.Sprintf("Lap %d, halfway there", a.state.current.currentLapNumber)
	case currentQuarter > a.pitRadioState.lastRaceProgress && currentQuarter == 1 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.currentLapNumber, lapsRemaining)
	}

	a.pitRadioState.lastRaceProgress = currentQuarter

	if message == "" {
		return
	}

	// Send lap number message to Discord
	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Failed to send lap number message")
		} else {
			a.log.Debug().
				Str("component", "discord").
				Str("message", message).
				Int16("lap", a.state.current.currentLapNumber).
				Msg("Lap number message sent")
		}
	}
}

// notifyDuration formats a time.Duration value for text and speech output
func notifyDuration(lapTime time.Duration) string {
	minutes := int(lapTime.Minutes())
	lapTime = lapTime - (time.Duration(minutes) * time.Minute)

	seconds := int(lapTime.Seconds())
	lapTime = lapTime - (time.Duration(seconds) * time.Second)

	milliseconds := int(lapTime.Milliseconds())

	minutesStr := fmt.Sprintf("%d", minutes)

	var secondsStr string
	if seconds == 0 {
		secondsStr = "0"
	} else {
		secondsFmt := "%02d"
		if minutesStr == "0" {
			secondsFmt = "%d"
		}

		secondsStr = fmt.Sprintf(secondsFmt, seconds)
	}

	millisecondsStr := fmt.Sprintf("%03d", milliseconds)

	fmt.Printf("%s:%s.%s\n", minutesStr, secondsStr, millisecondsStr)

	return pronounceTime(minutesStr, secondsStr, millisecondsStr, false)
}

// pronounceTime formats minutes, seconds and millisecond time components for text and speech output
func pronounceTime(minutes string, seconds string, milliseconds string, includeUnits bool) string {
	announce := []string{}

	if minutes != "0" {
		announce = append(announce, minutes)
		if includeUnits {
			announce = append(announce, "minutes")
		}
	}

	announce = append(announce, seconds)
	// if includeUnits {
	announce = append(announce, "point")
	// }

	for _, r := range milliseconds {
		rune := string(r)

		if rune == "0" {
			rune = "oh"
		}

		announce = append(announce, rune)
	}

	return strings.Join(announce, " ")
}

// updateFuelMonitoring tracks fuel consumption and lap distance
func (a *App) updateFuelMonitoring() {
	if a.pitRadioState == nil {
		return
	}

	currentLap := a.gtClient.Telemetry.CurrentLap()
	currentFuel := a.gtClient.Telemetry.FuelLevelPercent()

	// Initialize on first update
	if a.pitRadioState.lastNotifiedLapNumber == 0 {
		a.pitRadioState.lastFuelLevel = currentFuel
		return
	}

	// Update lap distance estimation
	a.updateLapDistance()

	// Check for new lap completion
	if currentLap > a.pitRadioState.lastNotifiedLapNumber && a.pitRadioState.lastNotifiedLapNumber > 0 {
		a.processNewLapFuel(currentFuel, currentLap)
	}

	a.pitRadioState.lastFuelLevel = currentFuel
}

// updateLapDistance tracks the distance covered in the current lap
func (a *App) updateLapDistance() {
	currentPos := struct{ X, Y, Z float32 }{
		X: a.gtClient.Telemetry.PositionalMapCoordinates().X,
		Y: a.gtClient.Telemetry.PositionalMapCoordinates().Y,
		Z: a.gtClient.Telemetry.PositionalMapCoordinates().Z,
	}

	if a.pitRadioState.isTrackingDistance && a.pitRadioState.lastPosition.X != 0 {
		// Calculate distance between current and last position
		dx := float64(currentPos.X - a.pitRadioState.lastPosition.X)
		dy := float64(currentPos.Y - a.pitRadioState.lastPosition.Y)
		dz := float64(currentPos.Z - a.pitRadioState.lastPosition.Z)
		distance := dx*dx + dy*dy + dz*dz // Using squared distance for performance

		// Only add reasonable distance increments (filter out teleports/glitches)
		if distance > 0 && distance < 250000 { // Max ~500m between updates (squared)
			a.pitRadioState.lapDistanceTracked += distance
		}
	}

	a.pitRadioState.lastPosition = currentPos
}

// processNewLapFuel handles fuel consumption calculation when a new lap is detected
func (a *App) processNewLapFuel(currentFuel float32, currentLap int16) {
	// Calculate fuel used in the completed lap
	fuelUsed := a.pitRadioState.lastFuelLevel - currentFuel

	// Only process if we have valid fuel usage data
	if fuelUsed > 0 && fuelUsed < 1.0 { // Sanity check
		a.addFuelUsageToHistory(fuelUsed)
		a.calculateAverageFuelUsage()

		a.log.Debug().
			Str("component", "fuel").
			Int16("lap", currentLap-1).
			Float32("fuel_used_percent", fuelUsed*100).
			Float32("current_fuel_percent", currentFuel*100).
			Float32("average_usage_percent", a.pitRadioState.averageFuelUsagePerLap*100).
			Msg("Lap fuel consumption")

		// Finalize lap distance if we were tracking it
		if a.pitRadioState.isTrackingDistance && a.pitRadioState.lapDistanceTracked > 0 {
			a.pitRadioState.estimatedLapDistance = a.pitRadioState.lapDistanceTracked
			a.log.Debug().
				Str("component", "fuel").
				Float64("estimated_distance_m", a.pitRadioState.estimatedLapDistance).
				Msg("Estimated lap distance")
		}
	}

	// Reset distance tracking for new lap
	a.pitRadioState.lapDistanceTracked = 0
	a.pitRadioState.isTrackingDistance = true
}

// addFuelUsageToHistory adds fuel usage data to the rolling history
func (a *App) addFuelUsageToHistory(fuelUsed float32) {
	if len(a.pitRadioState.fuelUsageHistory) >= a.pitRadioState.maxFuelHistorySize {
		// Remove oldest entry
		a.pitRadioState.fuelUsageHistory = a.pitRadioState.fuelUsageHistory[1:]
	}
	a.pitRadioState.fuelUsageHistory = append(a.pitRadioState.fuelUsageHistory, fuelUsed)
	a.pitRadioState.sampledLaps = len(a.pitRadioState.fuelUsageHistory)
}

// calculateAverageFuelUsage computes the average fuel usage per lap
func (a *App) calculateAverageFuelUsage() {
	if len(a.pitRadioState.fuelUsageHistory) == 0 {
		return
	}

	var total float32
	for _, usage := range a.pitRadioState.fuelUsageHistory {
		total += usage
	}
	a.pitRadioState.averageFuelUsagePerLap = total / float32(len(a.pitRadioState.fuelUsageHistory))
}

// checkFuelWarnings determines if a pit stop should be called and sends the notification
func (a *App) checkFuelWarnings() {
	if a.pitRadioState.averageFuelUsagePerLap <= 0 || a.pitRadioState.sampledLaps < 2 {
		return // Insufficient fuel data for prediction
	}

	currentFuel := a.gtClient.Telemetry.FuelLevelPercent()
	currentLap := a.gtClient.Telemetry.CurrentLap()
	raceLaps := a.gtClient.Telemetry.RaceLaps()

	// Don't notify if already notified for this lap
	if a.pitRadioState.lastNotifiedFuelWarning == currentLap {
		return
	}

	// Calculate fuel needed for next lap plus a safety margin
	safetyMargin := a.pitRadioState.averageFuelUsagePerLap * 0.1 // 10% safety margin
	fuelNeededForNextLap := a.pitRadioState.averageFuelUsagePerLap + safetyMargin

	// Calculate remaining laps in race
	remainingLaps := int16(raceLaps) - currentLap

	// Calculate fuel needed to finish the race
	fuelNeededToFinish := float32(remainingLaps) * a.pitRadioState.averageFuelUsagePerLap

	var message string
	shouldNotify := false

	// Check if we need to pit this lap
	if currentFuel < fuelNeededForNextLap {
		// message = fmt.Sprintf("Pit this lap! Fuel low: %.1f percent remaining, need %.1f percent for next lap",
		// 	currentFuel*100, fuelNeededForNextLap*100)
		message = "Box this lap. Fuel low"
		shouldNotify = true
	} else if currentFuel < fuelNeededToFinish && remainingLaps > 1 {
		// Check if we'll run out before race end (early warning)
		remainingLapsWithFuel := currentFuel / a.pitRadioState.averageFuelUsagePerLap
		if remainingLapsWithFuel < float32(remainingLaps) {
			message = fmt.Sprintf("Fuel strategy: %.1f laps remaining with current fuel, %d laps in race",
				remainingLapsWithFuel, remainingLaps)
			shouldNotify = true
		}
	}

	if shouldNotify {
		a.pitRadioState.lastNotifiedFuelWarning = currentLap
		a.sendFuelWarning(message)
	}
}

// sendFuelWarning sends a fuel-related warning message to the pit radio
func (a *App) sendFuelWarning(message string) {
	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Failed to send fuel warning message")
		} else {
			a.log.Info().
				Str("component", "discord").
				Str("message", message).
				Int16("lap", a.state.current.currentLapNumber).
				Float32("fuel_percent", a.gtClient.Telemetry.FuelLevelPercent()*100).
				Msg("Fuel warning message sent")
		}
	}
}
