package synth

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
)

type Synthesizer struct {
	effects      *EffectsSampleBank
	log          zerolog.Logger
	mixer        *Mixer
	outputDevice *OutputDevice
	kinematics   *kinematics.KinematicsTracker
	sampleRate   int
	outFile      *os.File
}

type SynthOpts struct {
	Config     *config.Synthesizer // FIXME: TODO: use base config pointer?
	Logger     zerolog.Logger
	Kinematics *kinematics.KinematicsTracker
}

func NewSynth(opts SynthOpts) (*Synthesizer, error) {
	bufferSlotSize := opts.Config.SampleRateHz / 60
	bufferSlotCount := 20
	bufferSize := bufferSlotSize * bufferSlotCount * 2 // 40 frames of audio at 8khz

	mixer, err := NewMixer(MixerConfig{
		MasterGain:    &opts.Config.MasterGain,
		GainIncrement: &opts.Config.GainIncrement,
		BufferSize:    bufferSize,
		Logger:        opts.Logger.With().Str("component", "synth mixer").Logger(),
	})
	if err != nil {
		return nil, fmt.Errorf("create mixer: %w", err)
	}

	_ = mixer.AddChannel("transmission", &opts.Config.TransmissionGain)
	_ = mixer.AddChannel("chassis", &opts.Config.ChassisGain)
	_ = mixer.AddChannel("engine", &opts.Config.EngineGain)

	outputDevice, err := NewOutputDevice(SynthOutDeviceOpts{
		Logger: opts.Logger.With().Str("component", "synth output device").Logger(),
	})
	if err != nil {
		return nil, err
	}

	var outFile *os.File
	if opts.Config.OutputFile != "" {
		outFile, err = os.Create(opts.Config.OutputFile)
		if err != nil {
			return nil, fmt.Errorf("create output wav file: %w", err)
		}

		log.Info().Str("file", opts.Config.OutputFile).Msg("saving audio output")
	}

	effects := NewEffectsSampleBank(opts.Config.SampleRateHz)

	return &Synthesizer{
		effects:      effects,
		log:          opts.Logger.With().Str("component", "synth").Logger(),
		mixer:        mixer,
		outputDevice: outputDevice,
		kinematics:   opts.Kinematics,
		sampleRate:   opts.Config.SampleRateHz,
		outFile:      outFile,
	}, nil
}

func (s *Synthesizer) GetSampleRate() int {
	return s.sampleRate
}

func (s *Synthesizer) GetBufferLength() int {
	return s.mixer.GetBufferLength()
}

// Buffer accessor methods
func (s *Synthesizer) ReadBufferNew(length int) []float64 {
	s.mixer.MixToMaster(length)

	return s.mixer.ReadChannel("_master", length)
}

func (s *Synthesizer) WriteBuffer(channel string, sample []float64) {
	magnitude, err := s.mixer.GetChannelPowerRatio(channel)
	if err != nil {
		s.log.Error().Err(err).Str("channel", channel).Msg("failed to get channel power ratio")
		return
	}

	s.mixer.WriteChannel(channel, sample, magnitude, false)
}

func (s *Synthesizer) OverwriteBuffer(channel string, sample []float64) {
	magnitude, err := s.mixer.GetChannelPowerRatio(channel)
	if err != nil {
		s.log.Error().Err(err).Str("channel", channel).Msg("failed to get channel power ratio")
		return
	}

	s.mixer.WriteChannel(channel, sample, magnitude, true)
}

func (s *Synthesizer) ShiftBuffer(length int) {
	s.mixer.Shift(length)
}

func (s *Synthesizer) ClearBuffer() {
	s.mixer.Shift(s.mixer.GetBufferLength())
}

func (s *Synthesizer) GetChannelMagnitude(name string) (float64, error) {
	return s.mixer.GetChannelPowerRatio(name)
}

func (s *Synthesizer) FadeIn(period time.Duration) {
	s.mixer.FadeIn(period)
}

func (s *Synthesizer) ApplyMasterGain(value float64) float64 {
	outputGain, _ := s.mixer.GetChannelPowerRatio("_master")

	return value * outputGain
}

func (s *Synthesizer) Silence() {
	s.mixer.SetFader(config.MinimumGain)
	s.mixer.silenced = true
}

// Effect accessor methods
func (s *Synthesizer) GetEffectSample(name string) []float64 {
	return s.effects.GetSample(name)
}

func (s *Synthesizer) PlayEffect(name string, magnitude float64) {
	channelMagnitude, err := s.mixer.GetChannelPowerRatio(name)
	if err != nil {
		s.log.Error().Err(err).Str("channel", name).Msg("failed to get channel power ratio")
		return
	}

	magnitude *= channelMagnitude

	// TODO: handle invalid effect name
	sample := s.effects.GetSample(name)
	s.mixer.WriteChannel(name, sample, magnitude, false)
}

func (s *Synthesizer) Close() error {
	s.mixer.Close()

	return s.outFile.Close()
}
