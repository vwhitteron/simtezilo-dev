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

// Realtime scheduling for the haptic audio producer thread. Only the producer
// runs per block; every other goroutine is control or background work.
const (
	// hapticRealtimePriority is the SCHED_FIFO priority the producer requests.
	// Keep it low. The ALSA interrupt threads and the PortAudio device callback
	// must both outrank the producer, or synthesis work starves the soundcard
	// clock and turns a scheduling win into a dropout.
	hapticRealtimePriority = 10

	// hapticRealtimeCPU is the core the producer pins itself to. The pin is
	// skipped unless the kernel command line isolated that core with isolcpus,
	// which support/rpi-setup.sh provisions, so leaving this set costs nothing
	// on an untuned machine. CPU 0 is never a sensible target because most
	// interrupts land there.
	hapticRealtimeCPU = 3

	// realtimeReportTimeout bounds how long audio startup waits for the
	// producer thread to report its scheduling result. The producer applies the
	// policy as its first action, so this is generous.
	realtimeReportTimeout = 2 * time.Second
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
