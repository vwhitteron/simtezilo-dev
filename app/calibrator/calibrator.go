package calibrator

import (
	"sync"
)

// OutputChannel represents which audio channels should receive the calibration tone.
type OutputChannel string

const (
	// OutputChannelBoth sends calibration tone to both left and right channels.
	OutputChannelBoth OutputChannel = "both"

	// OutputChannelLeft sends calibration tone to left channel only.
	OutputChannelLeft OutputChannel = "left"

	// OutputChannelRight sends calibration tone to right channel only.
	OutputChannelRight OutputChannel = "right"
)

// Calibrator manages tone generation state for calibration mode.
type Calibrator struct {
	enabled   bool
	frequency float64
	volume    float64
	channel   OutputChannel
	mu        sync.RWMutex
}

// New creates a new Calibrator instance with default values.
func New() *Calibrator {
	return &Calibrator{
		enabled:   false,
		frequency: 5,
		volume:    -30,
		channel:   OutputChannelBoth,
	}
}

// IsEnabled returns whether calibration mode is enabled.
func (c *Calibrator) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.enabled
}

// SetEnabled sets whether calibration mode is enabled.
func (c *Calibrator) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enabled = enabled
}

// GetFrequency returns the calibrator frequency in Hz.
func (c *Calibrator) GetFrequency() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.frequency
}

// SetFrequency sets the calibrator frequency in Hz.
func (c *Calibrator) SetFrequency(frequency float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clamp frequency between 5 and 160 Hz
	if frequency < 5 {
		frequency = 5
	}

	if frequency > 160 {
		frequency = 160
	}

	c.frequency = frequency
}

// GetVolume returns the calibrator volume in dB.
func (c *Calibrator) GetVolume() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.volume
}

// SetVolume sets the calibrator volume in dB.
func (c *Calibrator) SetVolume(volume float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clamp volume between -60 and 0 dB
	if volume > 0 {
		volume = 0
	}

	if volume < -60 {
		volume = -60
	}

	c.volume = volume
}

// GetChannel returns the calibrator output channel selection.
func (c *Calibrator) GetChannel() OutputChannel {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.channel
}

// SetChannel sets the calibrator output channel selection.
func (c *Calibrator) SetChannel(channel OutputChannel) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate channel value
	switch channel {
	case OutputChannelBoth, OutputChannelLeft, OutputChannelRight:
		c.channel = channel
	default:
		c.channel = OutputChannelBoth
	}
}
