package audio

import (
	"math"

	"github.com/rs/zerolog"
)

type MixerGain struct {
	Master   float64
	Streamer float64

	fader           float64
	gearChange      float64
	chassis         float64
	fadeInIncrement float64

	logger zerolog.Logger
}

func NewAudioMixer(gain float64, logger zerolog.Logger) MixerGain {
	return MixerGain{
		Master:   gain,
		Streamer: gain,

		fader:           -30,
		gearChange:      0,
		chassis:         0,
		fadeInIncrement: 0,

		logger: logger,
	}
}

func (m *MixerGain) MasterDecrease(change float64) {
	currentGain := m.Master

	m.Master -= change

	if m.fader == currentGain {
		m.fader = m.Master
		m.Streamer = volumeToGain(m.fader + m.chassis)
	}

}

func (m *MixerGain) MasterIncrease(change float64) {
	currentGain := m.Master

	m.Master += change

	if m.fader == currentGain {
		m.fader = m.Master
		m.Streamer = volumeToGain(m.fader + m.chassis)
	}
}

func (m *MixerGain) GetGearChangeGain() float64 {
	return m.fader - m.gearChange
}

func (m *MixerGain) SetGearChangeGain(gain float64) {
	m.logger.Debug().Float64("gain", gain).Msg("set gear change gain")

	m.gearChange = gain
}

func (m *MixerGain) SetFader(gain float64) {
	m.fader = gain
	m.Streamer = volumeToGain(m.fader)
}

func (m *MixerGain) SetFadeInTime(samples float64) {
	m.fadeInIncrement = (m.fader - m.Master) / samples
}

func (m *MixerGain) FadeInHaptics() {
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
