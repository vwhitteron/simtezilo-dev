package audio

import (
	"math"

	"github.com/rs/zerolog"
)

type Mixer struct {
	Master   float64
	Streamer float64

	fader           float64
	gearChange      float64
	chassis         float64
	fadeInIncrement float64

	logger zerolog.Logger
}

func NewAudioMixer(gain float64, logger zerolog.Logger) Mixer {
	return Mixer{
		Master:   gain,
		Streamer: gain,

		fader:           -30,
		gearChange:      0,
		chassis:         0,
		fadeInIncrement: 0,

		logger: logger,
	}
}

func (m *Mixer) MasterDecrease(change float64) {
	currentGain := m.Master

	m.Master -= change

	if m.fader == currentGain {
		m.fader = m.Master
		m.Streamer = volumeToGain(m.fader + m.chassis)
	}

}

func (m *Mixer) MasterIncrease(change float64) {
	currentGain := m.Master

	m.Master += change

	if m.fader == currentGain {
		m.fader = m.Master
		m.Streamer = volumeToGain(m.fader + m.chassis)
	}
}

func (m *Mixer) GetGearChangeGain() float64 {
	return m.fader - m.gearChange
}

func (m *Mixer) SetGearChangeGain(gain float64) {
	m.logger.Debug().Float64("gain", gain).Msg("set gear change gain")

	m.gearChange = gain
}

func (m *Mixer) SetFader(gain float64) {
	m.fader = gain
	m.Streamer = volumeToGain(m.fader)
}

func (m *Mixer) SetFadeInTime(samples float64) {
	m.fadeInIncrement = (m.fader - m.Master) / samples
}

func (m *Mixer) FadeInHaptics() {
	if m.fader == m.Master {
		return
	}

	if m.fader == -30 {
		m.logger.Debug().Str("state", "start").Msg("fade in")
	}

	newGain := m.fader

	if m.fadeInIncrement > 0 {
		newGain += m.fadeInIncrement
	} else {
		newGain -= m.fadeInIncrement
	}

	if newGain > m.Master {
		newGain = m.Master

		m.logger.Debug().Str("state", "complete").Msg("fade in")
	}

	m.fader = newGain

	m.Streamer = volumeToGain(m.fader + m.chassis)
}

func volumeToGain(volume float64) float64 {
	return math.Pow(10, (volume / 10))
}
