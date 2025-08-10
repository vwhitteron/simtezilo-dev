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
	Config     config.Synthesizer
	Logger     zerolog.Logger
	Kinematics *kinematics.KinematicsTracker
}

func NewSynth(opts SynthOpts) (*Synthesizer, error) {
	mixer := NewMixer(
		opts.Config.MasterGain,
		opts.Config.GainIncrement,
		opts.Logger.With().Str("component", "synth mixer").Logger(),
	)
	_ = mixer.AddChannel("gearchange", float64(opts.Config.GearShiftVolume)/100.0)
	_ = mixer.AddChannel("chassis", float64(opts.Config.ChassisVolume)/100.0)
	_ = mixer.SetAlgorithm(opts.Config.Algorithm)

	bufferSlotSize := opts.Config.SampleRateHz / 60
	bufferSlotCount := 20
	buffer := NewBuffer(bufferSlotSize, bufferSlotCount, mixer, opts.Logger.With().Str("component", "synth buffer").Logger())

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

		log.Info().Str("file", opts.Config.OutputFile).Msg("Writing audio output")
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
	s.buffer.Write(channel, sample)
}

func (s *Synthesizer) ShiftBuffer(samples int) {
	s.buffer.ShiftBuffer(samples)
}

func (s *Synthesizer) GetBufferLength() int {
	return s.buffer.GetLength()
}

func (s *Synthesizer) ClearBuffer() {
	s.buffer.ClearBuffer()
}

// Mixer accessor methods
func (s *Synthesizer) DecreaseMasterGain() float64 {
	s.Mixer.MasterDecrease()

	return s.Mixer.Master
}

func (s *Synthesizer) IncreaseMasterGain() float64 {
	s.Mixer.MasterIncrease()

	return s.Mixer.Master
}

func (s *Synthesizer) GetMasterGain() float64 {
	return s.Mixer.Master
}

func (s *Synthesizer) SetChannelVolume(name string, volume int) error {
	err := s.Mixer.SetChannelVolume(name, float64(volume)/100)

	return err
}

func (s *Synthesizer) IncreaseChannelVolume(name string) (int, error) {
	volume, err := s.Mixer.IncreaseChannelVolume(name)

	return int(volume * 100), err
}

func (s *Synthesizer) DecreaseChannelVolume(name string) (int, error) {
	volume, err := s.Mixer.DecreaseChannelVolume(name)

	return int(volume * 100), err
}

func (s *Synthesizer) GetChannelVolume(name string) (int, error) {
	volume, err := s.Mixer.GetChannelVolume(name)

	return int(volume * 100), err
}

func (s *Synthesizer) FadeIn(period time.Duration) {
	s.Mixer.FadeIn(period)
}

func (s *Synthesizer) MixOutput(value float64) float64 {
	return value * s.Mixer.outputGain
}

func (s *Synthesizer) Silence() {
	s.Mixer.SetFader(-30) // TODO: dont' use fixed value
}

// Effect accessor methods
func (s *Synthesizer) GetEffectSample(name string) []float64 {
	return s.effects.GetSample(name)
}

func (s *Synthesizer) PlayEffect(name string) {
	// TODO: handle invalid effect name
	sample := s.effects.GetSample(name)
	s.buffer.Write(name, sample)
}

func (s *Synthesizer) PlayEffectWithVolume(name string, percent int) {
	// TODO: handle invalid effect name
	sample := s.effects.GetSample(name)
	s.buffer.WriteWithVolumePercent(name, percent, sample)
}

func (s *Synthesizer) Close() error {
	return s.outFile.Close()
}
