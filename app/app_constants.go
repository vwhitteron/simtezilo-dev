package app

import "time"

const frameRate = 60 // 60 Hz

const (
	pitRadioFrameRate     = 1   // 1 Hz
	FanControlFrameRate   = 3   // 3 Hz
	displayFrameRate      = 15  // 15 Hz
	engineHapticFrameRate = 30  // 30 Hz
	hapticFrameRate       = 120 // 120 Hz
	telemetryFrameRate    = 60  // 60 Hz
)

// Virtual display geometry, matching the real ST7789 panels so rendered screens
// look identical when mirrored to the web UI hardware view.
const (
	virtualDisplayWidth  uint16  = 240
	virtualDisplayHeight uint16  = 240
	virtualDisplayDPI    float64 = 265
)

// TODO: should these be user configurable?
const (
	tyreConditionStablisationTime = 5 * time.Second
	tyreInterNotifyGap            = 30 * time.Second
)
