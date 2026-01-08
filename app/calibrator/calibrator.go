package calibrator

import (
	"context"
	"sync"
	"time"
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
	enabled        bool
	frequency      float64
	volume         float64
	channel        OutputChannel
	mu             sync.RWMutex
	sweeping       bool
	sweepCancel    context.CancelFunc
	sweepFrequency float64 // Current frequency during sweep
	sweepMin       float64 // Minimum sweep frequency
	sweepMax       float64 // Maximum sweep frequency
	sweepDuration  float64 // Sweep duration in seconds
}

// New creates a new Calibrator instance with default values.
func New() *Calibrator {
	return &Calibrator{
		enabled:       false,
		frequency:     5,
		volume:        -30,
		channel:       OutputChannelBoth,
		sweepMin:      5,
		sweepMax:      160,
		sweepDuration: 10,
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

// GetSweepMin returns the minimum sweep frequency in Hz.
func (c *Calibrator) GetSweepMin() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.sweepMin
}

// SetSweepMin sets the minimum sweep frequency in Hz.
func (c *Calibrator) SetSweepMin(freq float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clamp between 5 and sweepMax-1
	if freq < 5 {
		freq = 5
	}

	if freq >= c.sweepMax {
		freq = c.sweepMax - 1
	}

	c.sweepMin = freq
}

// GetSweepMax returns the maximum sweep frequency in Hz.
func (c *Calibrator) GetSweepMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.sweepMax
}

// SetSweepMax sets the maximum sweep frequency in Hz.
func (c *Calibrator) SetSweepMax(freq float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clamp between sweepMin+1 and 160
	if freq > 160 {
		freq = 160
	}

	if freq <= c.sweepMin {
		freq = c.sweepMin + 1
	}

	c.sweepMax = freq
}

// GetSweepDuration returns the sweep duration in seconds.
func (c *Calibrator) GetSweepDuration() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.sweepDuration
}

// SetSweepDuration sets the sweep duration in seconds.
func (c *Calibrator) SetSweepDuration(duration float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clamp between 1 and 60 seconds
	if duration < 1 {
		duration = 1
	}

	if duration > 60 {
		duration = 60
	}

	c.sweepDuration = duration
}

// IsSweeping returns whether a frequency sweep is currently active.
func (c *Calibrator) IsSweeping() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.sweeping
}

// GetSweepFrequency returns the current frequency during a sweep.
// Returns the static frequency if not sweeping.
func (c *Calibrator) GetSweepFrequency() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.sweeping {
		return c.sweepFrequency
	}

	return c.frequency
}

// StartSweep starts a frequency sweep from sweepMin to sweepMax.
// The sweep takes approximately 10 seconds to complete.
// If calibration is not enabled, it will be enabled automatically.
func (c *Calibrator) StartSweep() {
	c.mu.Lock()

	// Stop any existing sweep
	if c.sweepCancel != nil {
		c.sweepCancel()
		c.sweepCancel = nil
	}

	// Enable calibration mode if not already enabled
	if !c.enabled {
		c.enabled = true
	}

	// Create cancellation context
	ctx, cancel := context.WithCancel(context.Background())
	c.sweepCancel = cancel
	c.sweeping = true
	c.sweepFrequency = c.sweepMin // Start at configured minimum frequency

	c.mu.Unlock()

	// Run sweep in goroutine
	go c.runSweep(ctx)
}

// StopSweep stops an active frequency sweep.
func (c *Calibrator) StopSweep() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sweepCancel != nil {
		c.sweepCancel()
		c.sweepCancel = nil
	}

	c.sweeping = false
	c.sweepFrequency = c.frequency
}

// runSweep executes the frequency sweep loop.
func (c *Calibrator) runSweep(ctx context.Context) {
	const stepSize = 1.0

	// Helper function to calculate step duration based on current range and sweep duration
	calcStepDuration := func(minFreq, maxFreq, sweepSecs float64) time.Duration {
		stepCount := (maxFreq - minFreq) / stepSize
		if stepCount <= 0 {
			stepCount = 1
		}

		sweepDuration := time.Duration(sweepSecs * float64(time.Second))

		return sweepDuration / time.Duration(stepCount)
	}

	// Get initial values
	c.mu.RLock()
	minFreq := c.sweepMin
	maxFreq := c.sweepMax
	sweepSecs := c.sweepDuration
	prevMin := minFreq
	prevMax := maxFreq
	prevSweepSecs := sweepSecs

	c.mu.RUnlock()

	stepDuration := calcStepDuration(minFreq, maxFreq, sweepSecs)

	ticker := time.NewTicker(stepDuration)
	defer ticker.Stop()

	currentFreq := minFreq

	for {
		select {
		case <-ctx.Done():
			// Sweep cancelled
			return
		case <-ticker.C:
			// Read current min/max/duration values (they may have changed)
			c.mu.RLock()
			minFreq = c.sweepMin
			maxFreq = c.sweepMax
			sweepSecs = c.sweepDuration
			c.mu.RUnlock()

			// If min/max/duration changed, recalculate step duration and reset ticker
			if minFreq != prevMin || maxFreq != prevMax || sweepSecs != prevSweepSecs {
				prevMin = minFreq
				prevMax = maxFreq
				prevSweepSecs = sweepSecs
				stepDuration = calcStepDuration(minFreq, maxFreq, sweepSecs)
				ticker.Reset(stepDuration)
			}

			// Clamp current frequency to new bounds if needed
			if currentFreq < minFreq {
				currentFreq = minFreq
			}

			if currentFreq > maxFreq {
				currentFreq = minFreq
			}

			c.mu.Lock()
			c.sweepFrequency = currentFreq
			c.mu.Unlock()

			currentFreq += stepSize
			if currentFreq > maxFreq {
				// Sweep complete - restart from minimum
				currentFreq = minFreq
			}
		}
	}
}
