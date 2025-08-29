package synth

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
)

type Mixer struct {
	configGainIncrement *float64

	channels     map[string]*MixerChannel
	bufferSize   int
	log          zerolog.Logger
	faderGain    float64 // controls fade-in after pause or session reset
	fadeInActive bool
	silenced     bool

	// Buffer monitoring
	lastHealthCheck     time.Time
	healthCheckInterval time.Duration

	mu sync.RWMutex
}

type MixerChannel struct {
	activeGain float64
	configGain *float64
	buffer     Buffer
}

type MixerConfig struct {
	MasterGain    *float64
	GainIncrement *float64
	BufferSize    int
	Logger        zerolog.Logger
}

// TODO: set gain and gainIncrement to defaults and add setters instead
func NewMixer(mixerConfig MixerConfig) (*Mixer, error) {
	if mixerConfig.MasterGain == nil || mixerConfig.GainIncrement == nil {
		return nil, fmt.Errorf("gain and gainIncrement must be valid pointers")
	}

	m := &Mixer{
		configGainIncrement: mixerConfig.GainIncrement,

		bufferSize:   mixerConfig.BufferSize,
		channels:     map[string]*MixerChannel{},
		log:          mixerConfig.Logger,
		faderGain:    config.MinimumGain,
		fadeInActive: false,
		silenced:     true,

		// Initialize buffer monitoring
		lastHealthCheck:     time.Now(),
		healthCheckInterval: 5 * time.Second,
	}

	err := m.AddChannel("_master", mixerConfig.MasterGain)
	if err != nil {
		return nil, fmt.Errorf("add master channel: %w", err)
	}

	go m.watchForConfigChanges()

	return m, nil
}

func (m *Mixer) Close() {
	_ = m.SetChannelGain("_master", config.MinimumGain)
}

func (m *Mixer) GetBufferLength() int {
	return m.bufferSize
}

func (m *Mixer) AddChannel(name string, gain *float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; ok {
		return fmt.Errorf("channel %q already exists", name)
	}

	m.channels[name] = &MixerChannel{
		activeGain: *gain,
		configGain: gain,
		// buffer:     NewLinearBuffer(m.bufferSize),
		// buffer: NewRingBuffer(m.bufferSize),
		buffer: NewAdaptiveBuffer(m.bufferSize),
	}

	return nil
}

func (m *MixerChannel) Read(length int) []float64 {
	return m.buffer.Read(length)
}

func (m *MixerChannel) Write(samples []float64, magnitude float64, overwrite bool) {
	scaleSamples(&samples, magnitude)

	m.buffer.Write(samples, overwrite)
}

func (m *Mixer) WriteChannel(name string, samples []float64, magnitude float64, overwrite bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; !ok {
		return fmt.Errorf("channel not found: %q", name)
	}

	m.channels[name].Write(samples, magnitude, overwrite)

	return nil
}

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

func (m *Mixer) GetChannelNames() []string {
	names := make([]string, len(m.channels))

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
func (m *Mixer) GetChannelGain(name string) (float64, error) {
	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return channel.activeGain, nil
}

func (m *Mixer) SetChannelGain(name string, gain float64) error {
	channel, ok := m.channels[name]
	if !ok {
		return fmt.Errorf("channel %q does not exist", name)
	}

	if channel.activeGain == gain {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	channel.activeGain = gain
	m.channels[name] = channel

	return nil
}

func (m *Mixer) GetChannelPowerRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToPowerRatio(channel.activeGain), nil
}

func (m *Mixer) GetChannelAmplitudeRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToAmplitudeRatio(channel.activeGain), nil
}

func (m *Mixer) SetFader(gain float64) {
	m.mu.Lock()
	m.faderGain = gain
	m.mu.Unlock()

	_ = m.SetChannelGain("_master", m.faderGain)
}

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
		fadeInIncrement := (*master.configGain - m.faderGain) / (float64(period.Milliseconds() / fadeInInterval.Milliseconds()))

		m.log.Debug().Float64("current", m.faderGain).Float64("target", *master.configGain).Str("state", "begin").Msg("fade in")

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

func (m *Mixer) MixToMaster(length int) {
	outSamples := make([]float64, length)

	// mix in the chassis and transmission channels with equal priority
	var peak float64 = 0
	for _, name := range []string{"chassis", "transmission"} {
		channel, ok := m.channels[name]
		if !ok {
			m.log.Error().Str("channel", name).Msg("channel not found in mixer")
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
		m.log.Error().Str("channel", "engine").Msg("channel not found in mixer")
	} else {
		outSamplesTmp := make([]float64, length)
		engineSamples := channel.Read(length)

		done := false
		count := 0
		for !done {
			p := 0.0
			for i, engineSample := range engineSamples {
				outSamplesTmp[i] = mixSampleSum(outSamples[i], engineSample, &p)
			}

			if p > 1.0 {
				if count == 0 {
					scaleSamplesPeak(&engineSamples, p*2)
					count++
				} else {
					done = true
					magnitude = 1.0 / p
				}

				continue
			}

			done = true
		}

		copy(outSamples, outSamplesTmp)
	}

	m.channels["_master"].Write(outSamples, magnitude, true)
}

func (m *Mixer) ClearBuffers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, channel := range m.channels {
		channel.buffer.Clear()
	}
}

// checkBufferHealth monitors buffer health and logs issues
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

		// Check if the buffer supports health monitoring
		if adaptiveBuffer, ok := channel.buffer.(*AdaptiveBuffer); ok {
			overflows, underruns, fillRatio := adaptiveBuffer.Health()

			if overflows > 0 || underruns > 0 || fillRatio > 0.9 || fillRatio < 0.1 {
				m.log.Warn().
					Str("channel", name).
					Int("overflows", overflows).
					Int("underruns", underruns).
					Float64("fillRatio", fillRatio).
					Msg("buffer health issue detected")
			}
		}
	}
}

// TODO: is there a better way to integrate config changes?
func (m *Mixer) watchForConfigChanges() {
	m.log.Debug().Str("event", "start").Msg("config watch")

	for {
		time.Sleep(200 * time.Millisecond)

		if m.fadeInActive || m.silenced {
			continue
		}

		for name, channel := range m.channels {
			if channel.activeGain == *channel.configGain {
				continue
			}

			_ = m.SetChannelGain(name, *channel.configGain)

			if name == "_master" {
				if m.faderGain != channel.activeGain {
					m.mu.Lock()
					m.faderGain = channel.activeGain
					m.mu.Unlock()
				}
			}

			m.log.Debug().Str("channel", name).Float64("gain", *channel.configGain).Str("event", "change").Msg("config watch")
		}
	}
}
