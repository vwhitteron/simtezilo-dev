package synth

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
)

// TODO: remove and default to sum
var algorithms = []string{
	"sum",
	"rss",
}

type Mixer struct {
	configGainIncrement *float64

	channels     map[string]MixerChannel
	algorithm    int
	log          zerolog.Logger
	faderGain    float64 // controls fade-in after pause or session reset
	fadeInActive bool
	silenced     bool

	mu sync.RWMutex
}

type MixerChannel struct {
	activeGain float64
	configGain *float64
}

// TODO: set gain and gainIncrement to defaults and add setters instead
func NewMixer(gain *float64, gainIncrement *float64, logger zerolog.Logger) (*Mixer, error) {
	if gain == nil || gainIncrement == nil {
		return nil, fmt.Errorf("gain and gainIncrement must be valid pointers")
	}

	m := &Mixer{
		configGainIncrement: gainIncrement,

		channels: map[string]MixerChannel{
			"master": {
				activeGain: *gain,
				configGain: gain,
			},
		},
		algorithm:    0,
		log:          logger,
		faderGain:    config.MinimumGain,
		fadeInActive: false,
		silenced:     true,
	}

	go m.watchForConfigChanges()

	return m, nil
}

func (m *Mixer) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	master := m.channels["master"]
	master.activeGain = -60
	m.channels["master"] = master
}

func (m *Mixer) AddChannel(name string, gain *float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; ok {
		return fmt.Errorf("channel %q already exists", name)
	}

	m.channels[name] = MixerChannel{
		activeGain: *gain,
		configGain: gain,
	}

	return nil
}

func (m *Mixer) GetChannelNames() []string {
	names := make([]string, len(m.channels))

	m.mu.RLock()
	defer m.mu.RUnlock()

	i := 0
	for name := range m.channels {
		names[i] = name
		i++
	}

	return names
}

func (m *Mixer) SetAlgorithm(algorithmName string) error {
	for i, name := range algorithms {
		if name == algorithmName {
			m.algorithm = i

			return nil
		}
	}

	return fmt.Errorf("algorithm %q not found", algorithmName)
}

// TODO: remove functionality
func (m *Mixer) GetAlgorithm() string {
	return algorithms[m.algorithm]
}

// TODO: remove functionality
func (m *Mixer) NextAlgorithm() string {
	m.algorithm = (m.algorithm + 1) % len(algorithms)

	return algorithms[m.algorithm]
}

// TODO: remove functionality
func (m *Mixer) PreviousAlgorithm() string {
	m.algorithm = (m.algorithm - 1 + len(algorithms)) % len(algorithms)

	return algorithms[m.algorithm]
}

func (m *Mixer) GetChannelGain(name string) (float64, error) {
	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	m.mu.RLock()
	defer m.mu.Unlock()

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

	_ = m.SetChannelGain("master", m.faderGain)
}

func (m *Mixer) FadeIn(period time.Duration) {
	m.mu.RLock()
	master := m.channels["master"]
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
				_ = m.SetChannelGain("master", *master.configGain)

				break
			}

			_ = m.SetChannelGain("master", m.faderGain)

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

func (m *Mixer) MixSample(sample1 float64, sample2 float64, peak *float64) float64 {
	switch m.algorithm {
	case 0:
		return mixSampleAGC(sample1, sample2, peak)
	case 1:
		return mixSamplesRSS(sample1, sample2, peak)
	default:
		return 0.0
	}
}

// TODO: is there a better way to integrate config changes?
func (m *Mixer) watchForConfigChanges() {
	m.log.Debug().Str("event", "start").Msg("config watch")

	for {
		time.Sleep(100 * time.Millisecond)

		if m.fadeInActive || m.silenced {
			continue
		}

		for name, channel := range m.channels {
			if channel.activeGain == *channel.configGain {
				continue
			}

			_ = m.SetChannelGain(name, *channel.configGain)

			if name == "master" {
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

// Mixes two samples using an Automatic Gain Control (AGC) algorithm.
// Returns the mixed sample and the peak value which is later used to scale a slice of samples.
func mixSampleAGC(sample1 float64, sample2 float64, peak *float64) float64 {
	sum := sample1 + sample2

	sumAbs := math.Abs(sum)

	if sumAbs > *peak {
		*peak = sumAbs
	}

	return sum
}
