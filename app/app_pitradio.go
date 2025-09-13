package app

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	telemetry_client "github.com/zetetos/gt-telemetry"
)

type trackDataAction bool

const (
	// Notification
	fuelPreWarnNotifyLaps float64 = 2               // Number of laps before fuel exhaustion to send pre-warn notifications
	fuelUpdateNotifyLaps  int16   = 5               // Number of laps between fuel range updates
	positionDebounceTime          = 3 * time.Second // Suppress position change notifications for this duration
	messagePause                  = 3 * time.Second // Pause between pit radio messages

	// Fuel range
	samplingInitialState        int16   = -2              // Lap number to signify an initialised sampling state
	fuelRangeSamplesMax         int     = 20              // Number of samples for rolling average
	fuelRangeSafetyMarginMeters float64 = 2000            // 1000 meters safety margin for distance-based calculations
	fuelRangeInitialValue       float64 = 1000000         // Initial high value to indicate uninitialized state
	fuelRangeSmoothFactor       float64 = 0.15            // EMA smoothing factor (0.1 = slow, 0.3 = fast)
	fuelSafetyMarginLaps        float64 = 0.5             // 0.5 lap safety margin for lap-based calculations
	fuelRangeSampleInterval             = 2 * time.Second // How often to sample fuel range

	// Distance
	shortestSampleDistance float64 = 300       // Minimum distance in meters to be considered for range calculations
	shortestTrackDistance  float64 = 900 * 0.9 // Shortest known track length in meters (Northern Isle Speedway)

	// Track data handling
	retainTrackData trackDataAction = false // Retain existing track data when resetting the pit radio state
	resetTrackData  trackDataAction = true  // Clear track data when resetting the pit radio state
)

// pitRadioState tracks Discord/pit radio communication state
// Handled separately from the main race state to prevent interference due to differences
// in refresh rates
type pitRadioState struct {
	// Nofification tracking to prevent duplicate or noisy messages
	fuelNotifyPrewarnComplete  bool          // Whether the fuel pre-warn notification has been sent
	lastNotifiedLapFuelWarning int16         // Last lap number when a fuel warning was sent
	lastNotifiedLapNumber      int16         // Last notified lap number
	lastNotifiedLapTime        time.Duration // Last notified lap time
	lastNotifiedRaceProgress   int8          // Last notified race progress percentage
	lastNotifiedGridPosition   int16         // Last notified grid position of the vehicle
	debounceGirdPositionNotify time.Time     // Suppress grid position change notifications until this time

	// Grid position tracking
	currentGridPosition int16 // Current race position of the vehicle

	// Fuel range estimation
	lastFuelPercent           float32   // Fuel percentage from the last sample
	fuelInitialPercent        float32   // Fuel percentage from the first sample
	fuelPercentAtSample       float32   // Fuel percentage when distance was sampled
	fuelSampleDistance        float64   // Total distance when fuel was last sampled
	fuelUsageRatePerKm        float64   // Fuel usage rate in percent per kilometer
	fuelRangeMeters           float64   // Current fuel range in meters
	fuelRangeMetersSafe       float64   // Safe fuel range in meters with safety margin
	fuelRangeMetersSmoothed   float64   // Moving average fuel range in meters
	fuelRangeLaps             float64   // Fuel range in laps
	fuelRangeLapsSafe         float64   // Fuel range in laps with safety margin
	fuelRangeLapsSmoothed     float64   // Moving average fuel range in laps
	fuelRangeLapsSmoothedSafe float64   // Moving average fuel range in laps with safety margin
	fuelRangeSamples          []float32 // Rolling buffer for moving average fuel range calculations
	fuelSmoothingLastUpdate   time.Time // Last time the fuel range buffer was updated

	// Lap distance estimation for fuel calculations
	lapBeingSampled     int16                   // Lap number currently being sampled for determining lap distance
	lapStartMarker      float64                 // Total distance travelled at the start of the current lap
	lapDistance         float64                 // Track lap distance in meters
	distanceTotalMeters float64                 // Total distance travelled in the session in meters
	circuitPosition     telemetry_client.Vector // Last known circuit position for distance calculations
}

func (a *App) resetPitRadioState(keepTrackData trackDataAction) {
	resetState := &pitRadioState{
		lastNotifiedLapNumber:      a.gtClient.Telemetry.CurrentLap(),
		lastNotifiedLapTime:        a.gtClient.Telemetry.LastLaptime(),
		lastNotifiedGridPosition:   a.gtClient.Telemetry.GridPosition(),
		debounceGirdPositionNotify: time.Now().Add(24 * time.Hour),
		circuitPosition:            a.gtClient.Telemetry.PositionalMapCoordinates(),
		fuelInitialPercent:         a.gtClient.Telemetry.FuelLevelPercent(),
		fuelRangeSamples:           make([]float32, 0, fuelRangeSamplesMax),
		fuelSmoothingLastUpdate:    time.Now(),
		fuelRangeMeters:            fuelRangeInitialValue,
		fuelRangeMetersSafe:        fuelRangeInitialValue,
		fuelRangeLaps:              fuelRangeInitialValue,
		fuelRangeLapsSafe:          fuelRangeInitialValue,
		fuelRangeLapsSmoothed:      fuelRangeInitialValue,
		fuelRangeLapsSmoothedSafe:  fuelRangeInitialValue,
		lapBeingSampled:            samplingInitialState,
		lapStartMarker:             -1,
	}

	// Initialize fuel monitoring only when the track changes
	if keepTrackData {
		resetState.lastFuelPercent = a.gtClient.Telemetry.FuelLevelPercent()
		resetState.fuelRangeSamples = make([]float32, 0, fuelRangeSamplesMax)
	} else { // Keep existing fuel monitoring data when session resets but track is the same
		resetState.fuelRangeSamples = a.pitRadioState.fuelRangeSamples
		resetState.lapDistance = a.pitRadioState.lapDistance
	}

	a.log.Debug().
		Bool("track_data_retained", bool(keepTrackData)).
		Str("state", fmt.Sprintf("%+v", a.state)).
		Msg("state reset")

	a.pitRadioState = resetState
}

// resetEstimatedFuelRange resets the estimated fuel range
func (a *App) resetEstimatedFuelRange() {
	fuelLevelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	a.pitRadioState.fuelNotifyPrewarnComplete = false

	a.pitRadioState.fuelInitialPercent = fuelLevelPercent
	a.pitRadioState.lastFuelPercent = fuelLevelPercent

	a.pitRadioState.distanceTotalMeters = 0

	a.pitRadioState.fuelRangeMeters = fuelRangeInitialValue
	a.pitRadioState.fuelRangeMetersSafe = fuelRangeInitialValue
	a.pitRadioState.fuelRangeMetersSmoothed = fuelRangeInitialValue
	a.pitRadioState.fuelRangeLaps = fuelRangeInitialValue
	a.pitRadioState.fuelRangeLapsSafe = fuelRangeInitialValue
	a.pitRadioState.fuelRangeLapsSmoothed = fuelRangeInitialValue
	a.pitRadioState.fuelRangeLapsSmoothedSafe = fuelRangeInitialValue
}

func (a *App) sendPitRadioMessage() {
	if a.pitRadio == nil {
		return
	}

	if !a.sequenceHasAdvanced() {
		return
	}

	if !a.telemetryIsActive() {
		return
	}

	if a.timeOfDayHasReset() {
		a.resetPitRadioState(retainTrackData)
	}

	if a.pitRadioState == nil {
		a.resetPitRadioState(resetTrackData)

		return
	}

	a.updateFuelConsumption()

	a.checkFuelWarnings()

	if a.positionHasChanged() {
		a.notifyPosition()
	}

}

func (a *App) newLapHandler() {
	a.log.Debug().
		Str("component", "new lap handler").
		Msg("Start")

	for {
		select {
		case <-a.lapStartEvents:
			a.udpateLapDistance()

			a.notifyLapTime()

			time.Sleep(messagePause)

			a.notifyLapNumber()
		default:
			time.Sleep(250 * time.Millisecond)
		}
	}
}

// positionHasChanged checks if the grid position has changed since the last update
func (a *App) positionHasChanged() bool {
	if a.pitRadioState == nil {
		return false
	}

	if a.state.current.lapNumber <= 0 {
		return false
	}

	position := a.gtClient.Telemetry.GridPosition()

	if position <= 0 {
		return false
	}

	if position == a.pitRadioState.lastNotifiedGridPosition {
		return false
	} else if a.pitRadioState.currentGridPosition != position {
		a.pitRadioState.currentGridPosition = position
		a.pitRadioState.debounceGirdPositionNotify = time.Now().Add(positionDebounceTime)

		a.log.Debug().
			Str("component", "pit radio").
			Int16("new_position", position).
			Int16("old_position", a.pitRadioState.lastNotifiedGridPosition).
			Msg("Position change")
	}

	// Debounce position changes until time delay reached
	if time.Now().Before(a.pitRadioState.debounceGirdPositionNotify) {
		return false
	}

	// Reset debounce timer
	a.pitRadioState.debounceGirdPositionNotify = time.Now().Add(24 * time.Hour)
	a.pitRadioState.lastNotifiedGridPosition = a.pitRadioState.currentGridPosition

	return true
}

// notifyPosition sends position change notifications over the pit radio
func (a *App) notifyPosition() {
	message := fmt.Sprintf("P%d", a.pitRadioState.currentGridPosition)

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "pit radio").
				Str("message", message).
				Msg("Failed to send position change message")
		} else {
			a.log.Debug().
				Str("component", "pit radio").
				Str("message", message).
				Int16("lap", a.state.current.lapNumber).
				Msg("Position change message sent")
		}
	}
}

// notifyLapTime sends lap time notifications over the pit radio
func (a *App) notifyLapTime() {
	if a.pitRadioState == nil {
		return
	}

	a.pitRadioState.lastNotifiedLapTime = a.state.current.lastLapTime

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
				Str("component", "pit radio").
				Str("message", message).
				Msg("Failed to send lap time message")
		} else {
			a.log.Debug().
				Str("component", "pit radio").
				Str("message", message).
				Int16("lap", a.state.current.lapNumber).
				Dur("lapTime", a.state.current.lastLapTime).
				Msg("Lap time message sent")
		}
	}
}

// notifyLapNumber sends lap number notifications over the pit radio
func (a *App) notifyLapNumber() {
	if a.pitRadioState == nil {
		return
	}

	a.pitRadioState.lastNotifiedLapNumber = a.state.current.lapNumber

	if a.state.current.lapNumber == 0 {
		return
	}

	raceLaps := int16(a.gtClient.Telemetry.RaceLaps())
	if raceLaps == 0 {
		// TODO: handle endurance races
		return
	}

	longRace := raceLaps > 10
	lapsRemaining := raceLaps - a.state.current.lapNumber + 1
	raceProgressPercent := int8(100 * float64(a.state.current.lapNumber) / float64(raceLaps))

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
	case a.state.current.lapNumber == raceLaps:
		message = "final lap"
	case lapsRemaining <= 3 && longRace:
		message = fmt.Sprintf("%d laps remaining", lapsRemaining)
	case currentQuarter > a.pitRadioState.lastNotifiedRaceProgress && currentQuarter == 3 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.lapNumber, lapsRemaining)
	case currentQuarter > a.pitRadioState.lastNotifiedRaceProgress && currentQuarter == 2:
		message = fmt.Sprintf("Lap %d, halfway there", a.state.current.lapNumber)
	case currentQuarter > a.pitRadioState.lastNotifiedRaceProgress && currentQuarter == 1 && longRace:
		message = fmt.Sprintf("Lap %d, %d laps remaining", a.state.current.lapNumber, lapsRemaining)
	}

	a.pitRadioState.lastNotifiedRaceProgress = currentQuarter

	if message == "" {
		return
	}

	if a.pitRadio != nil {
		err := a.pitRadio.Send(message)
		if err != nil {
			a.log.Error().
				Err(err).
				Str("component", "pit radio").
				Str("message", message).
				Msg("Failed to send lap number message")
		} else {
			a.log.Debug().
				Str("component", "pit radio").
				Str("message", message).
				Int16("lap", a.state.current.lapNumber).
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

// checkForRefuel resets fuel range calculations when the vehicle has been refueled
func (a *App) checkForRefuel() {
	if a.gtClient.Telemetry.FuelLevelPercent() <= a.pitRadioState.lastFuelPercent {
		return
	}

	a.resetEstimatedFuelRange()
}

// updateFuelConsumption calculates lap distance, vehicle fuel consumption and range
func (a *App) updateFuelConsumption() {
	if a.pitRadioState == nil {
		return
	}

	a.checkForRefuel()

	a.updateDistanceTravelled()

	a.updateFuelRange()

	// TODO: remove debug logging
	if a.state.current.sequenceNumber%600 == 0 {
		fmt.Printf("DISTANCE: lap=%.0fm, total=%.0fm, marker=%.0fm, sampling=%d\n",
			a.pitRadioState.lapDistance,
			a.pitRadioState.distanceTotalMeters,
			a.pitRadioState.lapStartMarker,
			a.pitRadioState.lapBeingSampled,
		)

		fmt.Printf("FUEL %%: current=%.2f%%, last=%.2f%%, initial=%.2f%%\n",
			a.gtClient.Telemetry.FuelLevelPercent(),
			a.pitRadioState.lastFuelPercent,
			a.pitRadioState.fuelInitialPercent,
		)

		fuelUsagePerLap := float32(0)
		if a.pitRadioState.lapDistance > 0 {
			fuelUsagePerLap = float32(a.pitRadioState.fuelUsageRatePerKm * a.pitRadioState.lapDistance / 1000)
		}

		fmt.Printf("FUEL l: rate=%.2f%%/lap, range=%.2f laps, ranges safe=%.2f, ema=%.2f laps, ema safe=%.2f laps\n",
			fuelUsagePerLap,
			a.pitRadioState.fuelRangeLaps,
			a.pitRadioState.fuelRangeLapsSafe,
			a.pitRadioState.fuelRangeLapsSmoothed,
			a.getFuelRangeLapsSafe(),
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

	if a.pitRadioState.circuitPosition.X != 0 {
		// Calculate distance between current and last position
		dx := float64(currentPos.X - a.pitRadioState.circuitPosition.X)
		dy := float64(currentPos.Y - a.pitRadioState.circuitPosition.Y)
		dz := float64(currentPos.Z - a.pitRadioState.circuitPosition.Z)
		distance := math.Sqrt(dx*dx + dy*dy + dz*dz)

		// Only add reasonable distance increments (filter out teleports/glitches)
		if distance > 0 && distance < 500 { // Max ~500m between updates
			a.pitRadioState.distanceTotalMeters += distance
		}
	}

	a.pitRadioState.circuitPosition = currentPos
}

// updateLapDistance updates the lap distance when a fully sampled lap has been completed
func (a *App) udpateLapDistance() {
	if a.pitRadioState == nil {
		return
	}

	currentLap := a.gtClient.Telemetry.CurrentLap()
	lastLap := currentLap - 1

	lapDistance := a.pitRadioState.distanceTotalMeters - a.pitRadioState.lapStartMarker

	a.pitRadioState.lastFuelPercent = a.gtClient.Telemetry.FuelLevelPercent()

	invalidSampleLap := false

	isInitState := a.pitRadioState.lapBeingSampled == samplingInitialState

	if isInitState {
		invalidSampleLap = true
	}

	if !isInitState && a.pitRadioState.lapBeingSampled != lastLap {
		invalidSampleLap = true

		a.log.Debug().
			Str("component", "fuel").
			Int16("last_lap", lastLap).
			Int16("sampled_lap", a.pitRadioState.lapBeingSampled).
			Msg("Sampled lap out of sync")
	}

	if !isInitState && a.pitRadioState.lapStartMarker < 0 {
		invalidSampleLap = true

		a.log.Debug().
			Str("component", "fuel").
			Int16("lap", lastLap).
			Float64("total_distance", a.pitRadioState.distanceTotalMeters).
			Float64("lap_start_marker", a.pitRadioState.lapStartMarker).
			Msg("Lap starting marker not set")
	}

	if !isInitState && lapDistance < shortestTrackDistance {
		invalidSampleLap = true

		a.log.Debug().
			Str("component", "fuel").
			Int16("lap", lastLap).
			Float64("lap_distance", lapDistance).
			Msg("Lap distance too short")
	}

	a.pitRadioState.lapStartMarker = a.pitRadioState.distanceTotalMeters

	a.pitRadioState.lapBeingSampled = currentLap

	if invalidSampleLap {
		return
	}

	// Update lap distance TODO: track this in a slice to calculate percentile lap distance
	a.pitRadioState.lapDistance = lapDistance

	a.log.Info().
		Str("component", "fuel").
		Int16("lap", lastLap).
		Float64("lap_distance", lapDistance).
		Msg("Lap distance updated")
}

// updateFuelRange estimates the fuel range in both laps and distance and also applies smoothing to both
func (a *App) updateFuelRange() {
	now := time.Now()
	if now.Sub(a.pitRadioState.fuelSmoothingLastUpdate) < fuelRangeSampleInterval {
		return
	}

	a.updateFuelRangeDistance()
	a.updateFuelRangeLaps()

	a.pitRadioState.fuelSmoothingLastUpdate = now
}

// updateFuelRangeDistance calculates fuel range in distance even without complete lap data
func (a *App) updateFuelRangeDistance() {
	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	if a.pitRadioState.distanceTotalMeters < shortestSampleDistance {
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

	a.updateFuelRangeDistanceEMA()
}

// updateFuelRangeDistanceEMA applies exponential moving average smoothing to distance-based fuel range
func (a *App) updateFuelRangeDistanceEMA() {
	if a.pitRadioState.fuelRangeMeters <= 0 {
		return
	}

	// Initialize smoothed value if not set
	if a.pitRadioState.fuelRangeMetersSmoothed == fuelRangeInitialValue {
		a.pitRadioState.fuelRangeMetersSmoothed = a.pitRadioState.fuelRangeMeters

		return
	}

	alpha := float64(fuelRangeSmoothFactor)
	a.pitRadioState.fuelRangeMetersSmoothed = alpha*a.pitRadioState.fuelRangeMeters + (1-alpha)*a.pitRadioState.fuelRangeMetersSmoothed
}

// updateFuelRangeLaps calculates how many complete laps can be completed with current fuel
func (a *App) updateFuelRangeLaps() {
	if a.pitRadioState.lapDistance <= 0 {
		a.pitRadioState.fuelRangeLaps = fuelRangeInitialValue
		a.pitRadioState.fuelRangeLapsSafe = fuelRangeInitialValue

		return
	}

	a.pitRadioState.fuelRangeLaps = a.pitRadioState.fuelRangeMeters / a.pitRadioState.lapDistance
	a.pitRadioState.fuelRangeLapsSafe = a.pitRadioState.fuelRangeLaps - fuelSafetyMarginLaps

	a.updateFuelRangeLapsEMA()
}

// updateFuelRangeLapsEMA applies exponential moving average smoothing to lap-based fuel range
func (a *App) updateFuelRangeLapsEMA() {
	// Insufficient data to create EMA value
	if a.pitRadioState.fuelRangeLaps == fuelRangeInitialValue {
		return
	}

	// Add to rolling sample buffer
	if len(a.pitRadioState.fuelRangeSamples) >= fuelRangeSamplesMax {
		// Remove oldest sample
		a.pitRadioState.fuelRangeSamples = a.pitRadioState.fuelRangeSamples[1:]
	}

	a.pitRadioState.fuelRangeSamples = append(a.pitRadioState.fuelRangeSamples, float32(a.pitRadioState.fuelRangeLaps))

	// Initialize with first valid sample
	if a.pitRadioState.fuelRangeLapsSmoothed == fuelRangeInitialValue {
		a.pitRadioState.fuelRangeLapsSmoothed = a.pitRadioState.fuelRangeLaps
		a.pitRadioState.fuelRangeLapsSmoothedSafe = a.pitRadioState.fuelRangeLaps - fuelSafetyMarginLaps

		return
	}

	// Calculate moving average from sample buffer
	var sum float64
	for _, sample := range a.pitRadioState.fuelRangeSamples {
		sum += float64(sample)
	}
	movingAverage := sum / float64(len(a.pitRadioState.fuelRangeSamples))

	// Apply EMA smoothing to the moving average: smoothed = α × movingAverage + (1-α) × smoothed
	a.pitRadioState.fuelRangeLapsSmoothed = fuelRangeSmoothFactor*movingAverage + (1-fuelRangeSmoothFactor)*a.pitRadioState.fuelRangeLapsSmoothed
	a.pitRadioState.fuelRangeLapsSmoothedSafe = a.pitRadioState.fuelRangeLapsSmoothed - fuelSafetyMarginLaps
}

// getFuelRangeLapsSafe returns the fuel range with safety margin applied
// If the smoothed value is not available, it falls back to the non-smoothed value
func (a *App) getFuelRangeLapsSafe() float64 {
	if a.pitRadioState.fuelRangeLapsSmoothedSafe < fuelRangeInitialValue {
		return a.pitRadioState.fuelRangeLapsSmoothedSafe
	}

	// Final fallback to non-smoothed calculation
	return a.pitRadioState.fuelRangeLapsSafe
}

// checkFuelWarnings determines if a pit stop should be called and sends the notification
func (a *App) checkFuelWarnings() {
	currentFuelPercent := a.gtClient.Telemetry.FuelLevelPercent()

	// Insufficient fuel data for prediction
	if a.pitRadioState.lapDistance <= 0 {
		return
	}

	currentLap := a.gtClient.Telemetry.CurrentLap()
	raceLaps := a.gtClient.Telemetry.RaceLaps()

	// Calculate fuel range in laps (how many complete laps we can do with current fuel)
	fuelRangeLapsSafe := a.getFuelRangeLapsSafe() // Use smoothed range for notifications

	// Calculate remaining laps in race
	remainingLaps := int16(raceLaps) - currentLap

	effectiveRange := fuelRangeInitialValue // Avoid refuel notifications until range is known

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
		message = "Out of fuel, limp to box"
		shouldNotify = true
	} else if effectiveRange <= 0 {
		// Can't even finish this lap safely
		message = "Fuel critical, map 5 box box box"
		shouldNotify = true
	} else if effectiveRange < 1 {
		// Can finish this lap but need to pit immediately
		message = "Box this lap for fuel"
		shouldNotify = true
	} else if fuelRangeLapsSafe <= fuelPreWarnNotifyLaps {
		// Early warning when range drops below threshold plus pre-warn buffer
		lapsUntilRefuel := max(int(fuelRangeLapsSafe-1), 1)

		message = fmt.Sprintf("Refuel in %d laps", lapsUntilRefuel)
		shouldNotify = !a.pitRadioState.fuelNotifyPrewarnComplete

		a.pitRadioState.fuelNotifyPrewarnComplete = true
	} else if float64(remainingLaps) > fuelRangeLapsSafe && remainingLaps%fuelUpdateNotifyLaps == 0 {
		// Check if we'll run out before race end (strategic warning)
		message = fmt.Sprintf("Fuel range %.1f laps with %d laps remaining",
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

	if a.pitRadioState.lastNotifiedLapFuelWarning == lap {
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
				Int16("lap", a.state.current.lapNumber).
				Float32("fuel_percent", a.gtClient.Telemetry.FuelLevelPercent()).
				Float64("fuel_range_laps_current", a.pitRadioState.fuelRangeLaps).
				Float64("fuel_range_laps_smoothed", a.pitRadioState.fuelRangeLapsSmoothed).
				Float64("fuel_range_distance_smoothed", a.pitRadioState.fuelRangeMetersSmoothed).
				Float64("fuel_range_with_safety_laps", a.getFuelRangeLapsSafe()).
				Msg("Send fuel warning message")
		}
	}

	a.pitRadioState.lastNotifiedLapFuelWarning = lap
}
