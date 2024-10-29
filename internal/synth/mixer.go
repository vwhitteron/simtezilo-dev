package synth

import (
	"fmt"
	"math"
	"time"

	"github.com/rs/zerolog"
)

type Mixer struct {
	Master float64

	channels map[string]float64

	fader        float64
	fadeInActive bool
	output       float64

	logger zerolog.Logger
}

func NewMixer(gain float64, logger zerolog.Logger) *Mixer {
	return &Mixer{
		Master: gain,
		output: gain,

		fader: -30,

		fadeInActive: false,

		channels: map[string]float64{},
		logger:   logger,
	}
}

func (m *Mixer) AddChannel(name string, volume float64) error {
	if _, ok := m.channels[name]; ok {
		return fmt.Errorf("channel %q already exists", name)
	}

	m.channels[name] = volume

	return nil
}

func (m *Mixer) GetChannelNames() []string {
	names := make([]string, len(m.channels))

	i := 0
	for name := range m.channels {
		names[i] = name
		i++
	}

	return names
}

func (m *Mixer) SetChannelVolume(name string, volume float64) error {
	if _, ok := m.channels[name]; !ok {
		return fmt.Errorf("channel %q does not exist", name)
	}

	m.channels[name] = volume

	return nil
}

func (m *Mixer) GetChannelVolume(name string) (float64, error) {
	if _, ok := m.channels[name]; !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return m.channels[name], nil
}

func (m *Mixer) MasterDecrease(increment float64) {
	m.Master -= increment

	// don't adjust the fader or streamer if currently silenced or fading in
	if m.fadeInActive {
		return
	}

	m.fader = m.Master
	m.output = volumeToGain(m.Master)

	m.logger.Debug().Float64("master", m.Master).Float64("streamer", m.output).Str("state", "decrease").Msg("master volume")
}

func (m *Mixer) MasterIncrease(increment float64) {
	m.Master += increment

	m.logger.Debug().Float64("master", m.Master).Float64("streamer", m.output).Str("state", "increase").Msg("master volume")

	if m.fadeInActive {
		return
	}

	m.fader = m.Master
	m.output = volumeToGain(m.Master)

}

func (m *Mixer) SetFader(gain float64) {
	m.logger.Debug().Float64("gain", gain).Msg("set fader volume")
	m.fader = gain
	m.output = volumeToGain(m.fader)
}

func (m *Mixer) FadeIn(period time.Duration) {
	if m.fader == m.Master || m.fadeInActive {
		return
	}

	go func() {
		m.fadeInActive = true

		fadeInInterval := 50 * time.Millisecond
		fadeInIncrement := (m.Master - m.fader) / (float64(period.Milliseconds() / fadeInInterval.Milliseconds()))

		m.logger.Debug().Float64("current", m.fader).Float64("target", m.Master).Str("state", "begin").Msg("fade in")

		for {
			m.fader += fadeInIncrement

			if m.fader >= m.Master {
				m.fader = m.Master
				m.output = volumeToGain(m.Master)

				break
			}

			m.output = volumeToGain(m.fader)

			time.Sleep(fadeInInterval)
		}

		m.logger.Debug().Float64("current", m.fader).Float64("target", m.Master).Str("state", "complete").Msg("fade in")

		m.fadeInActive = false
	}()
}

func volumeToGain(volume float64) float64 {
	return math.Pow(10, (volume / 10))
}
