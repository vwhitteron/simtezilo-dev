package app

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	telemetry_client "github.com/zetetos/gt-telemetry"
)

const (
	positionDebounceTime        = 3 * time.Second
	messagePause                = 3 * time.Second
	maxFuelHistorySize          = 5
	fuelPreWarnLaps             = float64(2)
	fuelSafetyMarginLaps        = float64(0.5)     // 0.5 lap safety margin for lap-based calculations
	fuelRangeSafetyMarginMeters = float64(2000)    // 1000 meters safety margin for distance-based calculations
	rangeInitialValue           = float64(1000000) // Initial high value to indicate uninitialized state

	// Continuous fuel range monitoring constants
	maxFuelRangeSamples     = 20              // Number of samples for rolling average
	fuelRangeSampleInterval = 2 * time.Second // How often to sample fuel range
	fuelRangeSmoothFactor   = float64(0.15)   // EMA smoothing factor (0.1 = slow, 0.3 = fast)
)

// pitRadioState tracks Discord/pit radio communication state
// Handled separately from the main race state to prevent interference due to differences
// in refresh rates
type pitRadioState struct {
	// Last values sent to prevent duplicate messages
	lastNotifiedLapNumber int16
	lastNotifiedLapTime   time.Duration
	lastRaceProgress      int8
	lastNotifiedPosition  int16

	// Current position tracking with debouncing
	currentPosition        int16
	debouncePositionNotify time.Time

	// Fuel monitoring state
	lastFuelPercent           float32
	sampledLaps               int
	lastNotifiedFuelWarning   int16
	fuelNotifyPrewarnComplete bool
	fuelUsageHistory          []float32

	// Distance-based fuel range tracking (works even during first lap)
	fuelSampleDistance  float64 // Total distance when fuel was last sampled
	fuelPercentAtSample float32 // Fuel percentage when distance was sampled
	fuelInitialPercent  float32 // Starting fuel percentage for range calculation

	// Continuous fuel range monitoring
	fuelUsageRatePerLap       float32
	fuelUsageRatePerKm        float64
	fuelRangeMeters           float64
	fuelRangeMetersSafe       float64
	fuelRangeMetersSmoothed   float64 // Moving average fuel range in meters
	fuelRangeLaps             float64
	fuelRangeLapsSafe         float64
	fuelRangeLapsSmoothed     float64   // Smoothed fuel range in laps
	fuelRangeLapsSmoothedSafe float64   // Smoothed fuel range in laps
	fuelRangeSamples          []float32 // Rolling buffer for fuel range calculations
	fuelSmoothingLastUpdate   time.Time // Last time we updated the smoothed range

	// Lap distance estimation for fuel calculations
	lastLapNumber       int16
	lastPosition        telemetry_client.Vector
	lapDistanceTracker  float64
	lapDistance         float64
	distanceTotalMeters float64
}

func (a *App) resetPitRadioState(isNewTrack bool) {
	if a.pitRadioState == nil {
		a.pitRadioState = &pitRadioState{}

		return
	}

	resetState := &pitRadioState{
		lastNotifiedLapNumber:     a.gtClient.Telemetry.CurrentLap(),
		lastNotifiedLapTime:       a.gtClient.Telemetry.LastLaptime(),
		lastNotifiedPosition:      a.gtClient.Telemetry.GridPosition(),
		debouncePositionNotify:    time.Now().Add(24 * time.Hour),
		lastPosition:              a.gtClient.Telemetry.PositionalMapCoordinates(),
		lastLapNumber:             min(a.gtClient.Telemetry.CurrentLap()-1, 0),
		fuelInitialPercent:        a.gtClient.Telemetry.FuelLevelPercent(),
		fuelRangeSamples:          make([]float32, 0, maxFuelRangeSamples),
		fuelSmoothingLastUpdate:   time.Now(),
		fuelUsageHistory:          make([]float32, 0, 5),
		fuelRangeMeters:           rangeInitialValue,
		fuelRangeMetersSafe:       rangeInitialValue,
		fuelRangeLaps:             rangeInitialValue,
		fuelRangeLapsSafe:         rangeInitialValue,
		fuelRangeLapsSmoothed:     rangeInitialValue,
		fuelRangeLapsSmoothedSafe: rangeInitialValue,
	}

	if len(a.pitRadioState.fuelUsageHistory) != 0 {
		resetState.fuelUsageHistory = a.pitRadioState.fuelUsageHistory
	}

	// Initialize continuous fuel range monitoring
	if len(a.pitRadioState.fuelRangeSamples) != 0 {
		resetState.fuelRangeSamples = a.pitRadioState.fuelRangeSamples
	}

	// Initialize fuel monitoring only when the track changes
	if isNewTrack {
		resetState.lastFuelPercent = a.gtClient.Telemetry.FuelLevelPercent()
		resetState.fuelUsageHistory = make([]float32, 0, maxFuelHistorySize)
		fmt.Println("RESET: Pit radio fuel state initialized for new track") // TODO: remove debug logging
	}

	a.log.Info().
		Str("component", "app").
		Bool("new_track", isNewTrack).
		Str("state", fmt.Sprintf("%+v", a.state)).
		Msg("state reset")

	a.pitRadioState = resetState
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

	a.updateFuelConsumption()

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
		a.pitRadioState.debouncePositionNotify = time.Now().Add(positionDebounceTime)

		a.log.Debug().
			Str("component", "discord").
			Int16("new_position", position).
			Int16("old_position", a.pitRadioState.lastNotifiedPosition).
			Msg("Position change")
	}

	// Debounce position changes until time delay reached
	if time.Now().Before(a.pitRadioState.debouncePositionNotify) {
		return false
	}

	// Reset debounce timer
	a.pitRadioState.debouncePositionNotify = time.Now().Add(24 * time.Hour)
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

// updateFuelConsumption tracks fuel consumption and lap distance
func (a *App) updateFuelConsumption() {
	if a.pitRadioState == nil {
		return
	}

	a.checkForRefuel()

	a.updateDistanceTravelled()

	a.updateFuelRange()

	a.updateFuelUsedPerLap()

	// TODO: remove debug logging
	if a.state.current.sequenceNumber%600 == 0 {
		fmt.Printf("DISTANCE: lap=%.0fm, total=%.0fm\n",
			a.pitRadioState.lapDistance,
			a.pitRadioState.distanceTotalMeters,
		)

		fmt.Printf("FUEL %%: current=%.2f%%, last=%.2f%%, initial=%.2f%%\n",
			a.gtClient.Telemetry.FuelLevelPercent(),
			a.pitRadioState.lastFuelPercent,
			a.pitRadioState.fuelInitialPercent,
		)

		fmt.Printf("FUEL l: rate=%.2f%%/lap, range=%.2f laps, ranges safe=%.2f, ema=%.2f laps, ema safe=%.2f laps, samples=%d laps\n",
			a.pitRadioState.fuelUsageRatePerLap,
			a.pitRadioState.fuelRangeLaps,
			a.pitRadioState.fuelRangeLapsSafe,
			a.pitRadioState.fuelRangeLapsSmoothed,
			a.getFuelRangeLapsSafe(),
			a.pitRadioState.sampledLaps,
		)

		fmt.Printf("FUEL m: rate=%.2f%%/km, range=%.2fkm, range safe=%.2fkm\n",
			a.pitRadioState.fuelUsageRatePerKm,
			a.pitRadioState.fuelRangeMeters/1000,
			a.pitRadioState.fuelRangeMetersSafe/1000,
		)
	}
}

// updateDistanceTravelled tracks the total and lap distances covered in the current session
func (a *App) updateDistanceTravelled() {
	currentPos := a.gtClient.Telemetry.PositionalMapCoordinates()

	if a.pitRadioState.lastPosition.X != 0 {
		// Calculate distance between current and last position
		dx := float64(currentPos.X - a.pitRadioState.lastPosition.X)
		dy := float64(currentPos.Y - a.pitRadioState.lastPosition.Y)
		dz := float64(currentPos.Z - a.pitRadioState.lastPosition.Z)
		distance := math.Sqrt(dx*dx + dy*dy + dz*dz)

		// Only add reasonable distance increments (filter out teleports/glitches)
		if distance > 0 && distance < 500 { // Max ~500m between updates
			a.pitRadioState.lapDistanceTracker += distance
			a.pitRadioState.distanceTotalMeters += distance
		}
	}

	a.pitRadioState.lastPosition = currentPos
}

func (a *App) checkForRefuel() {
	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	// Vehicle has been refueled
	if currentFuelPercent > a.pitRadioState.lastFuelPercent {
		a.pitRadioState.fuelNotifyPrewarnComplete = false

		// Reset distance-based fuel tracking for refueled vehicle
		a.pitRadioState.fuelInitialPercent = currentFuelPercent
		a.pitRadioState.lastFuelPercent = currentFuelPercent
		a.pitRadioState.distanceTotalMeters = 0     // Reset distance counter after refueling
		a.pitRadioState.fuelRangeMetersSmoothed = 0 // Reset distance-based range
	}
}

// updateFuelUsedPerLap handles fuel consumption calculation when a new lap is detected
func (a *App) updateFuelUsedPerLap() {
	currentLap := a.gtClient.Telemetry.CurrentLap()

	lapDelta := currentLap - a.pitRadioState.lastLapNumber

	switch {
	case currentLap <= 1: // insufficient data until 1st lap is complete
		return
	case lapDelta == 0: // same lap, no action
		return
	case lapDelta > 1: // missed laps
		a.pitRadioState.lastLapNumber = currentLap

		return
	case lapDelta < 0: // session reset
		a.pitRadioState.lastLapNumber = currentLap

		return
	default: //new lap, continue processing
		a.pitRadioState.lastLapNumber = currentLap
		a.pitRadioState.lapDistance = a.pitRadioState.lapDistanceTracker
		a.pitRadioState.lapDistanceTracker = 0
	}

	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	lapFuelUsedPercent := max(a.pitRadioState.lastFuelPercent-currentFuelPercent, 0)

	a.updateFuelUsageHistory(lapFuelUsedPercent)
	a.updateFuelUsageAverage()

	a.log.Debug().
		Str("component", "fuel").
		Int16("lap", currentLap-lapDelta).
		Float32("fuel_used_percent", lapFuelUsedPercent).
		Float32("current_fuel_percent", currentFuelPercent).
		Float32("average_usage_percent", a.pitRadioState.fuelUsageRatePerLap).
		Msg("Lap fuel consumption")

	a.pitRadioState.lastFuelPercent = currentFuelPercent
}

// updateFuelUsageHistory adds fuel usage data to the rolling history
func (a *App) updateFuelUsageHistory(fuelUsed float32) {
	if fuelUsed <= 0 || fuelUsed > 100 {
		return
	}

	// Shift the history buffer left when full
	if len(a.pitRadioState.fuelUsageHistory) >= maxFuelHistorySize {
		a.pitRadioState.fuelUsageHistory = a.pitRadioState.fuelUsageHistory[1:]
	}

	a.pitRadioState.fuelUsageHistory = append(a.pitRadioState.fuelUsageHistory, fuelUsed)
	a.pitRadioState.sampledLaps = len(a.pitRadioState.fuelUsageHistory)
}

// updateFuelUsageAverage computes the average fuel usage per lap
func (a *App) updateFuelUsageAverage() {
	if len(a.pitRadioState.fuelUsageHistory) == 0 {
		a.pitRadioState.fuelUsageRatePerLap = 0

		return
	}

	var total, count float32
	for _, usage := range a.pitRadioState.fuelUsageHistory {
		if usage <= 0 {
			continue
		}

		total += usage
		count += 1
	}

	a.pitRadioState.fuelUsageRatePerLap = total / count
}

// updateFuelRange calculates the fuel range in both laps and distance, applying smoothing to both
func (a *App) updateFuelRange() {
	// Check if enough time has passed since last update
	now := time.Now()
	if now.Sub(a.pitRadioState.fuelSmoothingLastUpdate) < fuelRangeSampleInterval {
		return
	}

	a.updateFuelRangeMeters()
	a.updateFuelRangeLaps()

	a.pitRadioState.fuelSmoothingLastUpdate = now
}

// updateFuelRangeMeters calculates fuel range in distance even without complete lap data
func (a *App) updateFuelRangeMeters() {
	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	// Only calculate range after a meaningful distance has been traveled
	if a.pitRadioState.distanceTotalMeters < 100 {
		return
	}

	fuelConsumed := a.pitRadioState.fuelInitialPercent - currentFuelPercent

	// Prevent division by zero or negative consumption
	if fuelConsumed <= 0 {
		return
	}

	a.pitRadioState.fuelUsageRatePerKm = float64(fuelConsumed) / a.pitRadioState.distanceTotalMeters * 1000

	fuelUsagePerMeter := float64(fuelConsumed) / a.pitRadioState.distanceTotalMeters

	a.pitRadioState.fuelRangeMeters = float64(currentFuelPercent) / fuelUsagePerMeter
	a.pitRadioState.fuelRangeMetersSafe = a.pitRadioState.fuelRangeMeters - fuelRangeSafetyMarginMeters

	a.updateFuelRangeMetersEMA()
}

// updateFuelRangeMetersEMA applies exponential moving average smoothing to distance-based fuel range
func (a *App) updateFuelRangeMetersEMA() {
	if a.pitRadioState.fuelRangeMeters <= 0 {
		return
	}

	// Initialize smoothed value if not set
	if a.pitRadioState.fuelRangeMetersSmoothed == rangeInitialValue {
		a.pitRadioState.fuelRangeMetersSmoothed = a.pitRadioState.fuelRangeMeters

		return
	}

	alpha := float64(fuelRangeSmoothFactor)
	a.pitRadioState.fuelRangeMetersSmoothed = alpha*a.pitRadioState.fuelRangeMeters + (1-alpha)*a.pitRadioState.fuelRangeMetersSmoothed
}

// updateFuelRangeLaps calculates how many complete laps can be completed with current fuel
func (a *App) updateFuelRangeLaps() {
	if a.pitRadioState.lapDistance <= 0 {
		a.pitRadioState.fuelRangeLaps = rangeInitialValue
		a.pitRadioState.fuelRangeLapsSafe = rangeInitialValue

		return
	}

	a.pitRadioState.fuelRangeLaps = a.pitRadioState.fuelRangeMeters / a.pitRadioState.lapDistance
	a.pitRadioState.fuelRangeLapsSafe = a.pitRadioState.fuelRangeLaps - fuelSafetyMarginLaps

	a.updateFuelRangeLapsEMA()
}

// updateFuelRangeLapsEMA applies exponential moving average smoothing to lap-based fuel range
func (a *App) updateFuelRangeLapsEMA() {
	// Insufficient data to create EMA value
	if a.pitRadioState.fuelRangeLaps <= 0 {
		return
	}

	// Add to rolling sample buffer
	if len(a.pitRadioState.fuelRangeSamples) >= maxFuelRangeSamples {
		// Remove oldest sample
		a.pitRadioState.fuelRangeSamples = a.pitRadioState.fuelRangeSamples[1:]
	}

	a.pitRadioState.fuelRangeSamples = append(a.pitRadioState.fuelRangeSamples, float32(a.pitRadioState.fuelRangeLaps))

	// Initialize with first valid sample
	if a.pitRadioState.fuelRangeLapsSmoothed == rangeInitialValue {
		a.pitRadioState.fuelRangeLapsSmoothed = a.pitRadioState.fuelRangeLaps
		a.pitRadioState.fuelRangeLapsSmoothedSafe = a.pitRadioState.fuelRangeLaps - fuelSafetyMarginLaps

		return
	}

	// Apply EMA smoothing: smoothed = α × current + (1-α) × smoothed
	a.pitRadioState.fuelRangeLapsSmoothed = fuelRangeSmoothFactor*a.pitRadioState.fuelRangeLaps + (1-fuelRangeSmoothFactor)*a.pitRadioState.fuelRangeLapsSmoothed
	a.pitRadioState.fuelRangeLapsSmoothedSafe = a.pitRadioState.fuelRangeLapsSmoothed - fuelSafetyMarginLaps
}

// getFuelRangeLapsSafe returns the fuel range with safety margin applied
// If the smoothed value is not available, it falls back to the non-smoothed value
func (a *App) getFuelRangeLapsSafe() float64 {
	if a.pitRadioState.fuelRangeLapsSmoothedSafe < rangeInitialValue {
		return a.pitRadioState.fuelRangeLapsSmoothedSafe
	}

	// Final fallback to non-smoothed calculation
	return a.pitRadioState.fuelRangeLapsSafe
}

// checkFuelWarnings determines if a pit stop should be called and sends the notification
func (a *App) checkFuelWarnings() {
	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	// Insufficient fuel data for prediction
	if a.pitRadioState.fuelUsageRatePerLap <= 0 || a.pitRadioState.sampledLaps < 1 {
		return
	}

	currentLap := a.gtClient.Telemetry.CurrentLap()
	raceLaps := a.gtClient.Telemetry.RaceLaps()

	// Calculate fuel range in laps (how many complete laps we can do with current fuel)
	fuelRangeLapsSafe := a.getFuelRangeLapsSafe() // Use smoothed range for notifications

	// Calculate remaining laps in race
	remainingLaps := int16(raceLaps) - currentLap

	effectiveRange := rangeInitialValue // Avoid refuel notifications until range is known

	// Calculate current lap progress for more precise "this lap" warnings
	lapProgress := float64(0)
	if a.pitRadioState.lapDistance > 0 {
		lapProgress = a.pitRadioState.lapDistance / a.pitRadioState.lapDistance
		if lapProgress > 1.0 {
			lapProgress = 1.0
		}

		// Calculate remaining distance in current lap (0.0 to 1.0)
		remainingCurrentLapFraction := 1.0 - lapProgress

		// Effective range considering we need to complete this lap
		effectiveRange = fuelRangeLapsSafe - remainingCurrentLapFraction
	}

	var message string
	shouldNotify := false

	// Critical fuel warnings based on lap range
	if currentFuelPercent <= 1 {
		message = "Out of fuel!"
		shouldNotify = true
	} else if effectiveRange <= 0 {
		// Can't even finish this lap safely
		message = "Fuel insufficient, map 5 box box box"
		shouldNotify = true
	} else if effectiveRange < 1 {
		// Can finish this lap but need to pit immediately
		message = "Box this lap for fuel"
		shouldNotify = true
	} else if fuelRangeLapsSafe <= fuelPreWarnLaps {
		// Early warning when range drops below threshold plus pre-warn buffer
		lapsUntilRefuel := max(int(fuelRangeLapsSafe-1), 1)

		message = fmt.Sprintf("Refuel in %d laps", lapsUntilRefuel)
		shouldNotify = !a.pitRadioState.fuelNotifyPrewarnComplete

		a.pitRadioState.fuelNotifyPrewarnComplete = true
	} else if float64(remainingLaps) > fuelRangeLapsSafe && remainingLaps > 1 {
		// Check if we'll run out before race end (strategic warning)
		message = fmt.Sprintf("Fuel strategy: range %.1f laps with %d laps remaining",
			fuelRangeLapsSafe, remainingLaps)
		shouldNotify = true
	}

	if shouldNotify {
		a.sendFuelWarning(message, currentLap)
	}
}

// sendFuelWarning sends a fuel-related warning message to the pit radio
func (a *App) sendFuelWarning(message string, lap int16) {
	if message == "" {
		a.log.Error().
			Str("component", "discord").
			Err(errors.New("empty message")).
			Int16("lap", lap).
			Msg("Send fuel warning message")

		return
	}

	if a.pitRadioState.lastNotifiedFuelWarning == lap {
		return
	}

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "discord").
				Str("message", message).
				Msg("Send fuel warning message")
		} else {
			a.log.Info().
				Str("component", "discord").
				Str("message", message).
				Int16("lap", a.state.current.currentLapNumber).
				Float32("fuel_percent", a.gtClient.Telemetry.FuelLevelPercent()).
				Float64("fuel_range_laps_current", a.pitRadioState.fuelRangeLaps).
				Float64("fuel_range_laps_smoothed", a.pitRadioState.fuelRangeLapsSmoothed).
				Float64("fuel_range_distance_smoothed", a.pitRadioState.fuelRangeMetersSmoothed).
				Float64("fuel_range_with_safety_laps", a.getFuelRangeLapsSafe()).
				Msg("Send fuel warning message")
		}
	}

	a.pitRadioState.lastNotifiedFuelWarning = lap
}
