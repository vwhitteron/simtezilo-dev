package app

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// FormatDeltaTime formats a time delta for slower laps in tenths, hundredths, or thousandths.
func FormatDeltaTime(delta time.Duration) string {
	absSeconds := math.Abs(delta.Seconds())

	var (
		value float64
		units string
	)

	switch {
	case absSeconds > 0.90:
		// Values > 0.90s might ceil to 10 tenths, so treat as seconds
		rounded := math.Round(absSeconds*10) / 10

		unit := "seconds"
		if rounded == 1.0 {
			unit = "second"
		}

		if rounded == math.Floor(rounded) {
			return fmt.Sprintf("%.0f %s", rounded, unit)
		}

		return fmt.Sprintf("%.1f %s", rounded, unit)
	case absSeconds > 0.090:
		// Values > 0.090s might ceil to 10 hundredths, so treat as tenths
		value = delta.Seconds() * 10
		units = "tenth"
	case absSeconds > 0.0090:
		// Values > 0.0090s might ceil to 10 thou, so treat as hundredths
		value = delta.Seconds() * 100
		units = "hundredth"
	default:
		value = float64(delta.Milliseconds())
		units = "thou"
	}

	return PluraliseDelta(RoundDelta(value), units)
}

// RoundDelta rounds the delta value up or down based on whether it's faster or slower.
func RoundDelta(value float64) float64 {
	if value < 0 {
		return math.Abs(math.Floor(value))
	}

	return math.Abs(math.Ceil(value))
}

func PluraliseDelta(value float64, scale string) (format string) {
	roundedValue := int(value)

	format = strconv.Itoa(roundedValue) + " " + scale

	// Thousandths do not get pluralised
	if scale == "thou" {
		return format
	}

	if roundedValue != 1 {
		format += "s"
	}

	return format
}

// FormatDuration formats a time.Duration value for text and speech output.
func FormatDuration(lapTime time.Duration) string {
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

	return PronounceTime(minutesStr, secondsStr, millisecondsStr, false)
}

// PronounceTime formats minutes, seconds and millisecond time components for text and speech output.
func PronounceTime(minutes string, seconds string, milliseconds string, includeUnits bool) string {
	announce := []string{}

	if minutes != "0" {
		announce = append(announce, minutes)
		if includeUnits {
			announce = append(announce, "minutes")
		}
	}

	announce = append(announce, seconds)
	if includeUnits {
		announce = append(announce, "point")
	}

	for _, r := range milliseconds {
		char := string(r)

		if char == "0" {
			char = "oh"
		}

		announce = append(announce, char)
	}

	return strings.Join(announce, " ")
}
