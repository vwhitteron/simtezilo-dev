package calibrate

import (
	"math"
	"sync"

	"github.com/gopxl/beep"
)

// Calibration manages calibration tone generation state.
type Calibration struct {
	enabled   bool
	frequency float64
	volume    float64
	mu        sync.RWMutex
}

// New creates a new Calibration instance with default values.
func New() *Calibration {
	return &Calibration{
		enabled:   false,
		frequency: 5,
		volume:    -30,
	}
}

// GetEnabled returns whether calibration mode is enabled.
func (c *Calibration) GetEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.enabled
}

// SetEnabled sets whether calibration mode is enabled.
func (c *Calibration) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enabled = enabled
}

// GetFrequency returns the calibration frequency in Hz.
func (c *Calibration) GetFrequency() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.frequency
}

// SetFrequency sets the calibration frequency in Hz.
func (c *Calibration) SetFrequency(frequency float64) {
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

// GetVolume returns the calibration volume in dB.
func (c *Calibration) GetVolume() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.volume
}

// SetVolume sets the calibration volume in dB.
func (c *Calibration) SetVolume(volume float64) {
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

// SineWave represents a sine wave generator for calibration.
type SineWave struct {
	freq   float64
	phase  float64
	sr     beep.SampleRate
	volume float64
	cal    *Calibration
}

// NewSineWave creates a new sine wave generator.
func NewSineWave(sampleRate beep.SampleRate, cal *Calibration) *SineWave {
	return &SineWave{
		freq:   cal.GetFrequency(),
		phase:  0,
		sr:     sampleRate,
		volume: cal.GetVolume(),
		cal:    cal,
	}
}

// Stream generates the sine wave samples.
func (s *SineWave) Stream(samples [][2]float64) (n int, ok bool) {
	// Update parameters from calibration state
	s.freq = s.cal.GetFrequency()
	s.volume = s.cal.GetVolume()

	for index := range samples {
		// Calculate the sine wave value
		sample := volumeToGain(s.volume) * math.Sin(s.phase)

		// Output to both left and right channels (stereo)
		samples[index][0] = sample
		samples[index][1] = sample

		// Increment phase for next sample
		// phase increment = 2π * frequency / sample_rate
		s.phase += 2 * math.Pi * s.freq / float64(s.sr)

		// Keep phase in reasonable range to avoid floating point precision issues
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}
	}

	return len(samples), true
}

// Err returns any error (none for infinite sine wave).
func (s *SineWave) Err() error {
	return nil
}

// volumeToGain converts dB to linear gain.
func volumeToGain(volume float64) float64 {
	return math.Pow(10, (volume / 20))
}

// Mixer switches between haptic output and calibration tone.
type Mixer struct {
	cal            *Calibration
	hapticStreamer beep.Streamer
	sineWave       *SineWave
}

// NewMixer creates a new mixer that switches between haptic and calibration.
func NewMixer(cal *Calibration, hapticStreamer beep.Streamer, sineWave *SineWave) *Mixer {
	return &Mixer{
		cal:            cal,
		hapticStreamer: hapticStreamer,
		sineWave:       sineWave,
	}
}

// Stream implements the beep.Streamer interface.
func (m *Mixer) Stream(samples [][2]float64) (n int, ok bool) {
	if m.cal.GetEnabled() {
		return m.sineWave.Stream(samples)
	}

	return m.hapticStreamer.Stream(samples)
}

// Err implements the beep.Streamer interface.
func (m *Mixer) Err() error {
	if m.cal.GetEnabled() {
		return m.sineWave.Err()
	}

	return m.hapticStreamer.Err()
}
