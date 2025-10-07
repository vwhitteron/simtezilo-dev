package synthesizer

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

// Mixer handles multiple audio channels, mixing them into a master output channel.
type Mixer struct {
	configGainIncrement *float64

	channels     map[string]*MixerChannel
	bufferLength time.Duration
	sampleRateHz int
	log          zerolog.Logger
	faderGain    float64
	fadeInActive bool
	silenced     bool

	// Buffer monitoring
	lastHealthCheck     time.Time
	healthCheckInterval time.Duration

	mu sync.RWMutex
}

// MixerChannel represents an individual audio channel within the mixer.
type MixerChannel struct {
	activeGain float64
	configGain *float64
	buffer     Buffer
}

// MixerConfig holds configuration options for the Mixer.
type MixerConfig struct {
	MasterGain    *float64       // Pointer to the configuration master gain value
	GainIncrement *float64       // Pointer to the configuration gain increment value
	BufferLength  time.Duration  // Duration of audio the buffer should hold
	SampleRateHz  int            // Sample rate in Hz
	Log           zerolog.Logger // Logger instance for logging
}

// NewMixer creates a new Mixer instance with the provided configuration.
func NewMixer(mixerConfig MixerConfig) (*Mixer, error) {
	if mixerConfig.MasterGain == nil || mixerConfig.GainIncrement == nil {
		return nil, errors.New("gain and gainIncrement must be valid pointers")
	}

	// TODO: set gain and gainIncrement to defaults and add setters instead.
	mixer := &Mixer{
		configGainIncrement: mixerConfig.GainIncrement,

		bufferLength: mixerConfig.BufferLength,
		sampleRateHz: mixerConfig.SampleRateHz,
		channels:     map[string]*MixerChannel{},
		log:          mixerConfig.Log,
		faderGain:    config.MinimumGain,
		fadeInActive: false,
		silenced:     true,

		// Initialize buffer monitoring
		lastHealthCheck:     time.Now(),
		healthCheckInterval: 5 * time.Second,
	}

	err := mixer.AddChannel("_master", mixerConfig.MasterGain)
	if err != nil {
		return nil, fmt.Errorf("add master channel: %w", err)
	}

	go mixer.watchForConfigChanges()

	return mixer, nil
}

// Close gracefully shuts down the mixer, silencing output.
func (m *Mixer) Close() {
	_ = m.SetChannelGain("_master", config.MinimumGain)
}

// GetBufferLength returns the configured buffer length duration.
func (m *Mixer) GetBufferCapacity() int {
	return int(m.bufferLength.Seconds() * float64(m.sampleRateHz))
}

// AddChannel adds a new channel to the mixer with the specified name and initial gain.
func (m *Mixer) AddChannel(name string, gain *float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; ok {
		return fmt.Errorf("channel %q already exists", name)
	}

	m.channels[name] = &MixerChannel{
		activeGain: *gain,
		configGain: gain,
		// buffer: NewLinearBuffer(m.bufferLength, m.sampleRateHz),
		// buffer: NewRingBuffer(m.bufferLength, m.sampleRateHz),
		buffer: NewAdaptiveBuffer(m.bufferLength, m.sampleRateHz),
	}

	return nil
}

// Read reads the specified number of samples from the channel's buffer.
// All samples read are removed from the buffer.
func (m *MixerChannel) Read(length int) []float64 {
	return m.buffer.Read(length)
}

// Write writes samples to the channel's buffer with the specified magnitude and offset.
func (m *MixerChannel) Write(samples []float64, magnitude float64, offset int, overwrite bool) {
	// TODO: scaling slice in-place cause the gear shift wavform to be reduced every time it is played
	// is this the correct thing to do?
	ScaleSamples(&samples, magnitude)

	m.buffer.Write(samples, offset, overwrite)
}

// WriteChannel writes the provided sample data to the specified channel buffer at the given offset.
func (m *Mixer) WriteChannel(name string, samples []float64, magnitude float64, offset int, overwrite bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; !ok {
		return fmt.Errorf("channel not found: %q", name)
	}

	m.channels[name].Write(samples, magnitude, offset, overwrite)

	return nil
}

// ReadChannel reads the specified number of samples from the channel's buffer.
func (m *Mixer) ReadChannel(name string, length int) []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.channels[name]; !ok {
		return nil
	}

	// Check buffer health periodically
	m.checkBufferHealth()

	return m.channels[name].Read(length)
}

// InspectChannelBuffer returns a copy of the specified channel buffer for inspection.
func (m *Mixer) InspectChannelBuffer(name string, length int, offset int) []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if channel, ok := m.channels[name]; ok {
		return channel.buffer.Inspect(length, offset)
	}

	return nil
}

// GetChannelNames returns a list of all channel names configured in the mixer.
func (m *Mixer) GetChannelNames() []string {
	names := []string{}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for name := range m.channels {
		// skip internal channels
		if name[0:1] == "_" {
			continue
		}

		names = append(names, name)
	}

	return names
}

// GetChannelGain returns the current gain of the specified channel.
func (m *Mixer) GetChannelGain(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return channel.activeGain, nil
}

// SetChannelGain sets the gain of the specified channel.
func (m *Mixer) SetChannelGain(name string, gain float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	channel, ok := m.channels[name]
	if !ok {
		return fmt.Errorf("channel %q does not exist", name)
	}

	if channel.activeGain == gain {
		return nil
	}

	channel.activeGain = gain
	m.channels[name] = channel

	return nil
}

// GetChannelPowerRatio returns the current power ratio of the specified channel.
func (m *Mixer) GetChannelPowerRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToPowerRatio(channel.activeGain), nil
}

// GetChannelAmplitudeRatio returns the current amplitude ratio of the specified channel.
func (m *Mixer) GetChannelAmplitudeRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToAmplitudeRatio(channel.activeGain), nil
}

// SetFader sets the fader gain, which controls the overall output level.
func (m *Mixer) SetFader(gain float64) {
	m.mu.Lock()
	m.faderGain = gain
	m.mu.Unlock()

	_ = m.SetChannelGain("_master", m.faderGain)
}

// FadeIn gradually increases the master gain from minimum to the configured level over the specified period.
func (m *Mixer) FadeIn(period time.Duration) {
	m.mu.RLock()
	master := m.channels["_master"]
	m.mu.RUnlock()

	if master.activeGain == *master.configGain || m.fadeInActive {
		return
	}

	go func() {
		m.silenced = false
		m.fadeInActive = true

		fadeInInterval := 50 * time.Millisecond
		incrementTime := float64(period.Milliseconds() / fadeInInterval.Milliseconds())
		fadeInIncrement := (*master.configGain - m.faderGain) / incrementTime

		m.log.Debug().
			Float64("current", m.faderGain).
			Float64("target", *master.configGain).
			Str("state", "begin").
			Msg("fade in")

		for {
			m.faderGain += fadeInIncrement

			// fade in complete
			if m.faderGain >= *master.configGain {
				m.mu.Lock()
				m.faderGain = *master.configGain
				m.mu.Unlock()
				_ = m.SetChannelGain("_master", *master.configGain)

				break
			}

			_ = m.SetChannelGain("_master", m.faderGain)

			time.Sleep(fadeInInterval)
		}

		m.fadeInActive = false

		m.log.Debug().
			Float64("current", master.activeGain).
			Float64("target", *master.configGain).
			Str("state", "complete").
			Msg("fade in")
	}()
}

// MixToMaster mixes all active channels into the master channel buffer using an alternative algorithm.
func (m *Mixer) MixToMaster(length int) {
	outSamples := make([]float64, length)

	// mix in the chassis and transmission channels with equal priority
	var peak float64

	m.mu.RLock()

	for _, name := range []string{"chassis", "transmission"} {
		channel, ok := m.channels[name]
		if !ok {
			m.mu.RUnlock()
			m.log.Error().Str("channel", name).Msg("channel not found in mixer")
			m.mu.RLock()

			continue
		}

		if *channel.configGain == config.MinimumGain {
			continue
		}

		samples := channel.Read(length)

		for i, sample := range samples {
			outSamples[i] = mixSampleSum(outSamples[i], sample, &peak)
		}
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
	}

	magnitude := 1.0
	// mix in the engine channel with lower priority
	channel, ok := m.channels["engine"]
	if !ok {
		m.mu.RUnlock()
		m.log.Error().Str("channel", "engine").Msg("channel not found in mixer")
		m.mu.RLock()
	} else if *channel.configGain > config.MinimumGain {
		engineSamples := channel.Read(length)

		if len(engineSamples) > 0 {
			outSamplesWork := make([]float64, length)

			// Perform direct mix and get the peak value
			for index, engineSample := range engineSamples {
				peak := 0.0

				engineScaled := engineSample

				engineMax := 1.0 - signal.Abs(outSamples[index])
				if engineSample > engineMax || engineSample < -engineMax {
					engineScaled = engineMax * engineSample
				}

				mixed := mixSampleSum(outSamples[index], engineScaled, &peak)

				outSamplesWork[index] = mixed

				if mixed > 1.0 || mixed < -1.0 {
					m.log.Warn().
						Float64("sample", outSamples[index]).
						Float64("engine", engineSample).
						Float64("engineScaled", engineScaled).
						Float64("mixed", mixed).
						Float64("peak", peak).
						Msg("clipping")
				}
			}

			copy(outSamples, outSamplesWork)
		}
	}

	masterChannel := m.channels["_master"]
	m.mu.RUnlock()

	masterChannel.Write(outSamples, magnitude, 0, true)
}

// ClearBuffers clears all channel buffers in the mixer.
func (m *Mixer) ClearBuffers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, channel := range m.channels {
		channel.buffer.Clear()
	}
}

// checkBufferHealth monitors buffer health and logs issues.
func (m *Mixer) checkBufferHealth() {
	now := time.Now()
	if now.Sub(m.lastHealthCheck) < m.healthCheckInterval {
		return
	}

	m.lastHealthCheck = now

	for name, channel := range m.channels {
		if name == "_master" {
			continue
		}

		if *channel.configGain <= config.MinimumGain {
			continue
		}

		// Check if the buffer supports health monitoring
		if adaptiveBuffer, ok := channel.buffer.(*AdaptiveBuffer); ok {
			overflows, underruns, fillRatio := adaptiveBuffer.Health()

			if overflows > 0 || underruns > 0 || fillRatio > 0.9 || fillRatio < 0.1 {
				m.log.Debug().
					Bool("healthy", false).
					Str("channel", name).
					Int("overflows", overflows).
					Int("underruns", underruns).
					Float64("fillRatio", fillRatio).
					Msg("buffer")
			}
		}
	}
}

// TODO: is there a better way to integrate config changes?
// watchForConfigChanges monitors configuration changes and applies them to the mixer channels.
func (m *Mixer) watchForConfigChanges() {
	m.log.Debug().Str("event", "start").Msg("config watch")

	for {
		time.Sleep(200 * time.Millisecond)

		if m.fadeInActive || m.silenced {
			continue
		}

		m.mu.RLock()

		for name, channel := range m.channels {
			if channel.activeGain == *channel.configGain {
				continue
			}

			m.mu.RUnlock()
			_ = m.SetChannelGain(name, *channel.configGain)

			if name == "_master" {
				if m.faderGain != channel.activeGain {
					m.mu.Lock()
					m.faderGain = channel.activeGain
					m.mu.Unlock()
				}
			}

			m.log.Debug().Str("channel", name).Float64("gain", *channel.configGain).Str("event", "change").Msg("config watch")
			m.mu.RLock()
		}

		m.mu.RUnlock()
	}
}
