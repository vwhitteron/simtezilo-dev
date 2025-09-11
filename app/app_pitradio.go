package app

import (
	"fmt"
	"strings"
	"time"
)

const (
	positionDebounceTime = 3 * time.Second
	messagePause         = 3 * time.Second
	maxFuelHistorySize   = 5
	fuelPreWarnLaps      = float32(2)
	fuelSafetyMargin     = float32(0.2) // 20% safety margin
)

func (a *App) resetPitRadioState(isNewTrack bool) {
	if a.pitRadioState == nil {
		a.pitRadioState = &pitRadioState{}
	}

	a.pitRadioState.lastNotifiedLapNumber = a.gtClient.Telemetry.CurrentLap()
	a.pitRadioState.lastNotifiedLapTime = a.gtClient.Telemetry.LastLaptime()
	a.pitRadioState.lastNotifiedPosition = a.gtClient.Telemetry.GridPosition()
	a.pitRadioState.positionNotifyDebounce = time.Now().Add(24 * time.Hour)
	a.pitRadioState.lastPosition = a.gtClient.Telemetry.PositionalMapCoordinates()
	a.pitRadioState.lastLapNumber = min(a.gtClient.Telemetry.CurrentLap()-1, 0)
	a.pitRadioState.lapDistance = 0
	a.pitRadioState.distanceTraveled = 0

	if len(a.pitRadioState.fuelUsageHistory) == 0 {
		a.pitRadioState.fuelUsageHistory = make([]float32, 0, 5)
	}

	// Initialize fuel monitoring only when the track changes
	if isNewTrack {
		a.pitRadioState.lastFuelPercent = a.gtClient.Telemetry.FuelLevelPercent()
		a.pitRadioState.fuelUsageHistory = make([]float32, 0, maxFuelHistorySize)
		a.pitRadioState.isTrackingDistance = true
	}

	a.log.Info().
		Str("component", "app").
		Bool("new_track", isNewTrack).
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
		a.resetPitRadioState(false)
	}

	if a.pitRadioState == nil {
		a.resetPitRadioState(true)

		return
	}

	a.updateFuelMonitoring()

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

	position := a.gtClient.Telemetry.GridPosition()

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
	if raceLaps == 0 {
		// TODO: handle endurance races
		return
	}

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

	a.updateLapDistance()

	a.processNewLapFuel()
}

// updateLapDistance tracks the distance covered in the current lap
func (a *App) updateLapDistance() {
	currentPos := a.gtClient.Telemetry.PositionalMapCoordinates()

	if a.pitRadioState.isTrackingDistance && a.pitRadioState.lastPosition.X != 0 {
		// Calculate distance between current and last position
		dx := float64(currentPos.X - a.pitRadioState.lastPosition.X)
		dy := float64(currentPos.Y - a.pitRadioState.lastPosition.Y)
		dz := float64(currentPos.Z - a.pitRadioState.lastPosition.Z)
		distance := dx*dx + dy*dy + dz*dz // Using squared distance for performance

		// Only add reasonable distance increments (filter out teleports/glitches)
		if distance > 0 && distance < 250000 { // Max ~500m between updates (squared)
			a.pitRadioState.lapDistance += distance
		}
	}

	a.pitRadioState.lastPosition = currentPos
}

// processNewLapFuel handles fuel consumption calculation when a new lap is detected
func (a *App) processNewLapFuel() {
	currentLap := a.gtClient.Telemetry.CurrentLap()

	// wait for new lap events
	if currentLap <= a.pitRadioState.lastLapNumber {
		return
	}

	a.pitRadioState.lastLapNumber = currentLap

	// insufficient data until 1st lap is complete
	if currentLap <= 1 {
		return
	}

	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	// Calculate fuel used in the completed lap
	fuelUsed := (a.pitRadioState.lastFuelPercent - currentFuelPercent) / 100

	fmt.Printf("Lap %d, current fuel: %.2f%%, last fuel: %.2f%%, fuel used: %.2f%%\n", currentLap-1, currentFuelPercent, a.pitRadioState.lastFuelPercent, fuelUsed*100)

	// Only process if we have valid fuel usage data
	if fuelUsed > 0 && fuelUsed < 1.0 { // Sanity check
		a.addFuelUsageToHistory(fuelUsed)
		a.calculateAverageFuelUsage()

		a.log.Debug().
			Str("component", "fuel").
			Int16("lap", currentLap-1).
			Float32("fuel_used_percent", fuelUsed*100).
			Float32("current_fuel_percent", currentFuelPercent).
			Float32("average_usage_percent", a.pitRadioState.averageFuelUsagePerLap*100).
			Msg("Lap fuel consumption")

		// Finalize lap distance if we were tracking it
		if a.pitRadioState.isTrackingDistance && a.pitRadioState.lapDistance > 0 {
			a.pitRadioState.estimatedLapDistance = a.pitRadioState.lapDistance
			a.log.Debug().
				Str("component", "fuel").
				Float64("estimated_distance_m", a.pitRadioState.estimatedLapDistance).
				Msg("Estimated lap distance")
		}
	}

	a.pitRadioState.lastFuelPercent = currentFuelPercent

	// Reset distance tracking for new lap
	a.pitRadioState.lapDistance = 0
	a.pitRadioState.isTrackingDistance = true
}

// addFuelUsageToHistory adds fuel usage data to the rolling history
func (a *App) addFuelUsageToHistory(fuelUsed float32) {
	if len(a.pitRadioState.fuelUsageHistory) >= maxFuelHistorySize {
		// Remove oldest entry
		a.pitRadioState.fuelUsageHistory = a.pitRadioState.fuelUsageHistory[1:]
	}
	a.pitRadioState.fuelUsageHistory = append(a.pitRadioState.fuelUsageHistory, fuelUsed)
	a.pitRadioState.sampledLaps = len(a.pitRadioState.fuelUsageHistory)

	fmt.Printf("Laps: %d, Fuel history: %+v\n", a.pitRadioState.sampledLaps, a.pitRadioState.fuelUsageHistory)
}

// calculateAverageFuelUsage computes the average fuel usage per lap
func (a *App) calculateAverageFuelUsage() {
	if len(a.pitRadioState.fuelUsageHistory) == 0 {
		a.pitRadioState.averageFuelUsagePerLap = 0

		return
	}

	var total float32
	var count float32
	for _, usage := range a.pitRadioState.fuelUsageHistory {
		if usage <= 0 {
			continue
		}

		total += usage
		count += 1
	}

	a.pitRadioState.averageFuelUsagePerLap = total / count
}

// checkFuelWarnings determines if a pit stop should be called and sends the notification
func (a *App) checkFuelWarnings() {
	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	// Insufficient fuel data for prediction
	if a.pitRadioState.averageFuelUsagePerLap <= 0 || a.pitRadioState.sampledLaps < 1 {
		if a.state.current.sequenceNumber%600 == 0 {
			fmt.Printf("Collecting fuel data. Current fuel: %.2f%%\n", currentFuelPercent)
		}

		return
	}

	currentLap := a.gtClient.Telemetry.CurrentLap()
	raceLaps := a.gtClient.Telemetry.RaceLaps()

	// Calculate fuel needed for next lap plus a safety margin
	fuelPercentNeededForNextLap := a.pitRadioState.averageFuelUsagePerLap * (1.0 + fuelSafetyMargin) * 100

	// Calculate remaining laps in race
	remainingLaps := int16(raceLaps) - currentLap

	// Calculate fuel needed to finish the race
	fuelPercentNeededToFinish := float32(remainingLaps) * a.pitRadioState.averageFuelUsagePerLap * (1.0 + fuelSafetyMargin) * 100

	if a.state.current.sequenceNumber%600 == 0 {
		fmt.Printf("Current fuel: %.2f%%, Need for next lap: %.2f%%, Need to finish: %.2f%%\n", currentFuelPercent, fuelPercentNeededForNextLap, fuelPercentNeededToFinish)
	}

	var message string
	shouldNotify := false

	// Check if we need to pit this lap
	if currentFuelPercent <= 1 {
		message = "Out of fuel!"
		shouldNotify = true
	} else if currentFuelPercent < fuelPercentNeededForNextLap*2 {
		message = "Box this lap. Fuel low"
		shouldNotify = true
	} else if currentFuelPercent < fuelPercentNeededForNextLap*(1.0+float32(fuelPreWarnLaps)) {
		message = fmt.Sprintf("Refuel in %d laps", int(currentFuelPercent/fuelPercentNeededForNextLap))
		shouldNotify = true
	} else if currentFuelPercent < fuelPercentNeededToFinish && remainingLaps > 1 {
		// Check if we'll run out before race end (early warning)
		fuelRangeLaps := currentFuelPercent / a.pitRadioState.averageFuelUsagePerLap
		if fuelRangeLaps < float32(remainingLaps) {
			message = fmt.Sprintf("Fuel strategy: %.1f laps remaining with current fuel, %d laps in race",
				fuelRangeLaps, remainingLaps)
			shouldNotify = true
		}
	}

	if shouldNotify && a.pitRadioState.lastNotifiedFuelWarning != currentLap {
		a.sendFuelWarning(message)
		a.pitRadioState.lastNotifiedFuelWarning = currentLap
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
