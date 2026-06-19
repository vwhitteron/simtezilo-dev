package predictivelap

import (
	"sync"
	"time"
)

// LapClock synthesizes the current lap's elapsed time from the telemetry packet
// sequence counter, used as a fallback when the real CurrentLaptime field is not
// present in the packet format. It accumulates only live frames: dropped packets
// (which represent real elapsed time) are counted, while paused/loading frames are
// excluded. It is safe for concurrent use — StartLap runs on the lap-event
// goroutine while Advance/Elapsed run on the tick loop.
type LapClock struct {
	mu            sync.Mutex
	frameInterval time.Duration
	started       bool
	elapsedFrames int64
}

// NewLapClock creates a LapClock whose synthesized time advances by frameInterval
// per live telemetry frame.
func NewLapClock(frameInterval time.Duration) *LapClock {
	return &LapClock{frameInterval: frameInterval}
}

// StartLap begins timing a new lap from zero.
func (c *LapClock) StartLap() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.started = true
	c.elapsedFrames = 0
}

// Advance accumulates frames telemetry frames of elapsed lap time. live is false
// while the game is paused or loading, in which case no time is accumulated.
func (c *LapClock) Advance(frames uint32, live bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started && live {
		c.elapsedFrames += int64(frames)
	}
}

// Elapsed returns the synthesized lap time and whether a lap is being timed.
func (c *LapClock) Elapsed() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return 0, false
	}

	return c.frameInterval * time.Duration(c.elapsedFrames), true
}

// Reset stops timing until the next StartLap.
func (c *LapClock) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.started = false
	c.elapsedFrames = 0
}
