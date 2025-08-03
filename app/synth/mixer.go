package synth

import (
	"fmt"
	"math"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

var algorithms = []string{
	"sum",
	"rss",
}

type Mixer struct {
	Master        float64
	gainIncrement float64

	channels     map[string]float64
	algorithm    int
	logger       zerolog.Logger
	outputGain   float64
	faderGain    float64 // controls fade-in after pause or session reset
	fadeInActive bool
}

// TODO: set gain and gainIncrement to defaults and add setters instead
func NewMixer(gain float64, gainIncrement float64, logger zerolog.Logger) *Mixer {
	return &Mixer{
		Master:        gain,
		gainIncrement: gainIncrement,
		outputGain:    gain,

		channels:     map[string]float64{},
		algorithm:    0,
		logger:       logger,
		faderGain:    -30,
		fadeInActive: false,
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

func (m *Mixer) SetAlgorithm(algorithmName string) error {
	for i, name := range algorithms {
		if name == algorithmName {
			m.algorithm = i

			return nil
		}
	}

	return fmt.Errorf("algorithm %q not found", algorithmName)
}

func (m *Mixer) GetAlgorithm() string {
	return algorithms[m.algorithm]
}

func (m *Mixer) NextAlgorithm() string {
	m.algorithm = (m.algorithm + 1) % len(algorithms)

	return algorithms[m.algorithm]
}

func (m *Mixer) PreviousAlgorithm() string {
	m.algorithm = (m.algorithm - 1 + len(algorithms)) % len(algorithms)

	return algorithms[m.algorithm]
}

func (m *Mixer) IncreaseChannelVolume(name string) (float64, error) {
	if _, ok := m.channels[name]; !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	volume := m.channels[name]

	if volume < 1 {
		volume += 0.01
	}

	m.channels[name] = volume

	return volume, nil
}

func (m *Mixer) DecreaseChannelVolume(name string) (float64, error) {
	if _, ok := m.channels[name]; !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	volume := m.channels[name]

	if volume > 0 {
		volume -= 0.01
	}

	m.channels[name] = volume

	return volume, nil
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

func (m *Mixer) MasterDecrease() {
	m.Master -= m.gainIncrement

	// don't adjust the fader or streamer if currently silenced or fading in
	if m.fadeInActive {
		return
	}

	m.faderGain = m.Master
	m.outputGain = volumeToGain(m.Master)

	m.logger.Debug().Float64("master", m.Master).Float64("streamer", m.outputGain).Str("state", "decrease").Msg("master volume")
}

func (m *Mixer) MasterIncrease() {
	m.Master += m.gainIncrement

	m.logger.Debug().Float64("master", m.Master).Float64("streamer", m.outputGain).Str("state", "increase").Msg("master volume")

	if m.fadeInActive {
		return
	}

	m.faderGain = m.Master
	m.outputGain = volumeToGain(m.Master)

}

func (m *Mixer) SetFader(gain float64) {
	m.faderGain = gain
	m.outputGain = volumeToGain(m.faderGain)
}

func (m *Mixer) FadeIn(period time.Duration) {
	if m.faderGain == m.Master || m.fadeInActive {
		return
	}

	go func() {
		m.fadeInActive = true

		fadeInInterval := 50 * time.Millisecond
		fadeInIncrement := (m.Master - m.faderGain) / (float64(period.Milliseconds() / fadeInInterval.Milliseconds()))

		m.logger.Debug().Float64("current", m.faderGain).Float64("target", m.Master).Str("state", "begin").Msg("fade in")

		for {
			m.faderGain += fadeInIncrement

			if m.faderGain >= m.Master {
				m.faderGain = m.Master
				m.outputGain = volumeToGain(m.Master)

				break
			}

			m.outputGain = volumeToGain(m.faderGain)

			time.Sleep(fadeInInterval)
		}

		m.logger.Debug().Float64("current", m.faderGain).Float64("target", m.Master).Str("state", "complete").Msg("fade in")

		m.fadeInActive = false
	}()
}

func volumeToGain(volume float64) float64 {
	return math.Pow(10, (volume / 10))
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

// Mixes two samples using a Root Square Sum algorithm.
// If peak is less than 0, it applies a limiter to the output sample to prevent clipping.
// Otherwise, the RSS mixed sample and the peak value are returned.
func mixSamplesRSS(sample1 float64, sample2 float64, peak *float64) float64 {
	sampleOut := 0.0

	squareSample1 := signal.Polarity(sample1) * sample1 * sample1
	squareSample2 := signal.Polarity(sample2) * sample2 * sample2
	sum := math.Abs(squareSample1 + squareSample2)
	sampleOut = math.Sqrt(sum)

	if sampleOut > *peak {
		*peak = sampleOut
	}

	// Restore the signal to its original polarity since RSS always results in a
	// positive value
	sampleOut = sampleOut * signal.Polarity(sample1+sample2)

	return sampleOut
}

// Adjusts the gain on a slice of samples using the peak value.
func scaleSamplesPeak(samples *[]float64, peak float64) {
	if peak < 1.0 {
		return
	}

	scale := 1.0 / peak

	for i := range *samples {
		(*samples)[i] = (*samples)[i] * scale
	}
}
