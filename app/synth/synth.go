package synth

import (
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
)

type Synthesizer struct {
	buffer       *Buffer
	effects      *EffectsSampleBank
	log          zerolog.Logger
	Mixer        *Mixer
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
	mixer, err := NewMixer(
		&opts.Config.MasterGain,
		&opts.Config.GainIncrement,
		opts.Logger.With().Str("component", "synth mixer").Logger(),
	)
	if err != nil {
		return nil, fmt.Errorf("create mixer: %w", err)
	}

	_ = mixer.AddChannel("transmission", &opts.Config.TransmissionGain)
	_ = mixer.AddChannel("chassis", &opts.Config.ChassisGain)
	_ = mixer.AddChannel("engine", &opts.Config.EngineGain)
	_ = mixer.SetAlgorithm(opts.Config.Algorithm)

	bufferSlotSize := opts.Config.SampleRateHz / 60
	bufferSlotCount := 20
	buffer := NewBuffer(bufferSlotSize, bufferSlotCount)

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
		buffer:       buffer,
		effects:      effects,
		log:          opts.Logger.With().Str("component", "synth").Logger(),
		Mixer:        mixer,
		outputDevice: outputDevice,
		kinematics:   opts.Kinematics,
		sampleRate:   opts.Config.SampleRateHz,
		outFile:      outFile,
	}, nil
}

func (s *Synthesizer) GetSampleRate() int {
	return s.sampleRate
}

// Buffer accessor methods
func (s *Synthesizer) ReadBuffer(length int) []float64 {
	sample := s.buffer.Read(length)

	if s.outFile != nil {
		_ = binary.Write(s.outFile, binary.LittleEndian, sample)
	}

	return sample
}

func (s *Synthesizer) WriteBuffer(channel string, sample []float64) {
	s.buffer.Write(channel, sample, 1.0, false)
}

func (s *Synthesizer) OverwriteBuffer(channel string, sample []float64) {
	s.buffer.Write(channel, sample, 1.0, true)
}

func (s *Synthesizer) ShiftBuffer(samples int) {
	s.buffer.Shift(samples)
}

func (s *Synthesizer) GetBufferLength() int {
	return s.buffer.Length()
}

func (s *Synthesizer) ClearBuffer() {
	s.buffer.Clear()
}

func (s *Synthesizer) GetChannelMagnitude(name string) (float64, error) {
	return s.Mixer.GetChannelPowerRatio(name)
}

func (s *Synthesizer) FadeIn(period time.Duration) {
	s.Mixer.FadeIn(period)
}

func (s *Synthesizer) ApplyMasterGain(value float64) float64 {
	outputGain, _ := s.Mixer.GetChannelPowerRatio("master")

	return value * outputGain
}

func (s *Synthesizer) Silence() {
	s.Mixer.SetFader(config.MinimumGain)
	s.Mixer.silenced = true
}

// Effect accessor methods
func (s *Synthesizer) GetEffectSample(name string) []float64 {
	return s.effects.GetSample(name)
}

func (s *Synthesizer) PlayEffect(name string) {
	// TODO: handle invalid effect name
	sample := s.effects.GetSample(name)
	s.buffer.Write(name, sample, 1.0, false)
}

func (s *Synthesizer) PlayEffectWithMagnitude(name string, magnitude float64) {
	// TODO: handle invalid effect name
	sample := s.effects.GetSample(name)
	s.buffer.Write(name, sample, magnitude, false)
}

func (s *Synthesizer) Close() error {
	s.Mixer.Close()

	return s.outFile.Close()
}
