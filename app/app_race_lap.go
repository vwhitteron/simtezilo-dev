package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
)

// notifyLapTime sends lap time notifications over the pit radio.
func (a *App) notifyLapTime() {
	if a.pitRadio == nil || a.pitRadioState == nil {
		return
	}

	if a.pitRadioState.lastNotifiedLapTime == a.state.current.lastLapTime {
		return
	}

	a.pitRadioState.lastNotifiedLapTime = a.state.current.lastLapTime

	if a.state.current.lastLapTime <= 0 {
		return
	}

	var message string

	bestLapTime := a.gtClient.Telemetry.BestLaptime()

	// TODO: add config option to notify all laps or best lap only
	//nolint:gocritic // if-else chain is clearer here
	if bestLapTime > 0 && a.state.current.lastLapTime <= bestLapTime && a.state.current.lapNumber > 2 {
		message = fmt.Sprintf("%s. %s",
			formatDuration(a.state.current.lastLapTime),
			a.i18n.GetString(languagedb.RadioLapRecord),
		)
	} else if a.state.current.lapNumber > 2 && a.state.current.lastLapTime > bestLapTime {
		// TODO: either make this a translation or drop it
		message = fmt.Sprintf("Down %s seconds",
			formatDuration(a.state.current.lastLapTime-bestLapTime))
	} else {
		message = formatDuration(a.state.current.lastLapTime)
	}

	// Send lap time message to Discord
	err := a.pitRadio.Send(pitradio.Message{
		MessageType: pitradio.TextMessage,
		Text:        message,
		Lang:        a.i18n.LanguageCode(),
		Accent:      a.config.GetAppAccent(),
		NoCache:     true,
	})
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "pit radio").
			Str("message", message).
			Msg("Send lap time message")

		return
	}

	a.log.Debug().
		Str("message", message).
		Int16("lap", a.state.current.lapNumber).
		Dur("lapTime", a.state.current.lastLapTime).
		Msg("Send lap time message")
}

// notifyLapNumber sends lap number notifications over the pit radio.
func (a *App) notifyLapNumber() {
	if !a.shouldSendLapNotification() {
		return
	}

	lapInfo := a.getLapNotificationInfo()

	message := a.determineLapMessage(lapInfo)
	if message == "" {
		return
	}

	a.sendLapNotification(message, lapInfo.currentLap)
}

// shouldSendLapNotification checks if a lap notification should be sent.
func (a *App) shouldSendLapNotification() bool {
	if a.pitRadioState == nil {
		return false
	}

	// TODO: add config options for min race laps to notify
	if a.gtClient.Telemetry.RaceLaps() == 1 {
		return false
	}

	currentLap := a.state.current.lapNumber

	return a.pitRadioState.lastNotifiedLapNumber < currentLap &&
		currentLap > 0 &&
		currentLap <= a.gtClient.Telemetry.RaceLaps()
}

// lapNotificationInfo holds information needed for lap notifications.
type lapNotificationInfo struct {
	currentLap      int16
	raceLaps        int16
	lapsRemaining   int16
	longRace        bool
	alreadyNotified bool
}

// getLapNotificationInfo gathers lap information for notifications.
func (a *App) getLapNotificationInfo() lapNotificationInfo {
	currentLap := a.state.current.lapNumber
	raceLaps := a.gtClient.Telemetry.RaceLaps()
	lapsRemaining := raceLaps - currentLap + 1
	longRace := raceLaps > 8
	alreadyNotified := a.pitRadioState.lastNotifiedLapNumber == currentLap

	return lapNotificationInfo{
		currentLap:      currentLap,
		raceLaps:        raceLaps,
		lapsRemaining:   lapsRemaining,
		longRace:        longRace,
		alreadyNotified: alreadyNotified,
	}
}

// determineLapMessage determines what lap message to send based on race conditions.
func (a *App) determineLapMessage(info lapNotificationInfo) string {
	raceCompleted := info.lapsRemaining <= 0 && !info.alreadyNotified
	finalLap := info.currentLap == info.raceLaps && !info.alreadyNotified
	// TODO: add config option for final lap countdown range
	lastFewLaps := info.lapsRemaining <= 3 && info.longRace && !info.alreadyNotified

	switch {
	case raceCompleted:
		return a.i18n.GetString(languagedb.RadioRaceFinish)
	case finalLap:
		return a.i18n.GetString(languagedb.RadioFinalLap)
	case lastFewLaps:
		format := a.i18n.GetString(languagedb.RadioLapsRemainingFmt)

		return fmt.Sprintf(format, info.lapsRemaining)
	case info.currentLap <= 1:
		return ""
	default:
		return fmt.Sprintf("Lap %d", info.currentLap)
	}
}

// sendLapNotification sends the lap notification message via pit radio.
func (a *App) sendLapNotification(message string, currentLap int16) {
	a.pitRadioState.lastNotifiedLapNumber = currentLap

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
			Msg("Send lap number message")

		return
	}

	a.log.Debug().
		Str("message", message).
		Int16("lap", a.state.current.lapNumber).
		Msg("Send lap number message")
}

// notifyRaceProgress sends lap number notifications based on race progress over the pit radio.
func (a *App) notifyRaceProgress() {
	if a.pitRadioState == nil {
		return
	}

	progressInterval := 25 // race percent TODO: make a config option

	currentLap := a.state.current.lapNumber

	if currentLap == 0 {
		return
	}

	raceLaps := a.gtClient.Telemetry.RaceLaps()
	if raceLaps == 0 {
		// TODO: handle time based endurance races
		return
	}

	circuitLengthMeters := a.circuit.LengthMeters()
	if circuitLengthMeters <= 0 {
		return
	}

	// Calculate race progress based on distance in meters
	totalRaceDistanceMeters := float64(raceLaps) * circuitLengthMeters
	currentRaceDistanceMeters := float64(currentLap-1)*circuitLengthMeters + a.circuit.LapProgress()*circuitLengthMeters

	raceProgressPercent := int8(100 * currentRaceDistanceMeters / totalRaceDistanceMeters)

	// Calculate current progress interval based on progressInterval
	currentProgressInterval := (raceProgressPercent / int8(progressInterval)) * int8(progressInterval)

	// Skip notifications at 0%
	if raceProgressPercent <= 0 {
		return
	}

	NotifyInterval := currentProgressInterval > a.pitRadioState.lastNotifiedRaceProgressInterval &&
		raceProgressPercent < 100

	if !NotifyInterval {
		return
	}

	format := a.i18n.GetString(languagedb.RadioRaceProgressFmt)
	message := fmt.Sprintf(format, raceProgressPercent)

	a.pitRadioState.lastNotifiedRaceProgressInterval = currentProgressInterval

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
				Msg("Send lap number message")

			return
		}

		a.log.Debug().
			Str("message", message).
			Int16("lap", a.state.current.lapNumber).
			Msg("Send lap number message")
	}
}

// formatDuration formats a time.Duration value for text and speech output.
func formatDuration(lapTime time.Duration) string {
	minutes := int(lapTime.Minutes())
	lapTime -= time.Duration(minutes) * time.Minute

	seconds := int(lapTime.Seconds())
	lapTime -= time.Duration(seconds) * time.Second

	milliseconds := int(lapTime.Milliseconds())

	minutesStr := strconv.Itoa(minutes)

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

	return pronounceTime(minutesStr, secondsStr, millisecondsStr, false)
}

// pronounceTime formats minutes, seconds and millisecond time components for text and speech output.
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
		char := string(r)

		if char == "0" {
			char = "oh"
		}

		announce = append(announce, char)
	}

	return strings.Join(announce, " ")
}
