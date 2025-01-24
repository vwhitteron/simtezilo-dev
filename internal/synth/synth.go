package synth

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/internal/config"
	"github.com/vwhitteron/simtezilo-dev/internal/physics"
)

type Synthesizer struct {
	buffer       *Buffer
	bumpStream   BumpStream
	effects      *EffectsSampleBank
	log          zerolog.Logger
	mixer        *Mixer
	outputDevice *OutputDevice
	physics      *physics.PhysicsTracker
	sampleRate   int
}

type SynthOpts struct {
	AssetDir string
	Config   config.Synthesizer
	Logger   zerolog.Logger
	Physics  *physics.PhysicsTracker
}

func NewSynth(opts SynthOpts) (*Synthesizer, error) {
	mixer := NewMixer(opts.Config.MasterGain, opts.Config.GainIncrement, opts.Logger.With().Str("component", "synth mixer").Logger())
	mixer.AddChannel("gearchange", float64(opts.Config.GearStreetVolume/100))
	mixer.AddChannel("chassis", float64(opts.Config.ChassisVolume/100))

	bufferSize := opts.Config.SampleRateHz / 60
	buffer := NewBuffer(bufferSize, 20, mixer, opts.Logger.With().Str("component", "synth buffer").Logger())

	outputDevice, err := NewOutputDevice(SynthOutDeviceOpts{
		Logger: opts.Logger.With().Str("component", "synth output device").Logger(),
	})
	if err != nil {
		return nil, err
	}

	effects := NewEffectsSampleBank(opts.Config.SampleRateHz)

	return &Synthesizer{
		buffer:       buffer,
		effects:      effects,
		log:          opts.Logger.With().Str("component", "synth").Logger(),
		mixer:        mixer,
		outputDevice: outputDevice,
		physics:      opts.Physics,
		sampleRate:   opts.Config.SampleRateHz,
	}, nil
}

func (s *Synthesizer) GetSampleRate() int {
	return s.sampleRate
}

// Buffer accessor methods
func (s *Synthesizer) ReadBuffer(length int) []float64 {
	return s.buffer.Read(length)
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
	s.mixer.MasterDecrease()

	return s.mixer.Master
}

func (s *Synthesizer) IncreaseMasterGain() float64 {
	s.mixer.MasterIncrease()

	return s.mixer.Master
}

func (s *Synthesizer) GetMasterGain() float64 {
	return s.mixer.Master
}

func (s *Synthesizer) SetChannelVolume(name string, volume int) error {
	err := s.mixer.SetChannelVolume(name, float64(volume)/100)

	return err
}

func (s *Synthesizer) IncreaseChannelVolume(name string) (int, error) {
	volume, err := s.mixer.IncreaseChannelVolume(name)

	return int(volume * 100), err
}

func (s *Synthesizer) DecreaseChannelVolume(name string) (int, error) {
	volume, err := s.mixer.DecreaseChannelVolume(name)

	return int(volume * 100), err
}

func (s *Synthesizer) GetChannelVolume(name string) (int, error) {
	volume, err := s.mixer.GetChannelVolume(name)

	return int(volume * 100), err
}

func (s *Synthesizer) FadeIn(period time.Duration) {
	s.mixer.FadeIn(period)
}

func (s *Synthesizer) MixOutput(value float64) float64 {
	return value * s.mixer.output
}

func (s *Synthesizer) Silence() {
	s.mixer.SetFader(-30) // FIXME: dont' use fixed value
}

// Effect accessor methods
func (s *Synthesizer) GetEffectSample(name string) []float64 {
	return s.effects.GetSample(name)
}

func (s *Synthesizer) PlayEffect(name string) {
	// FIXME: handle invalid effect name
	sample := s.effects.GetSample(name)
	s.buffer.Write(name, sample)
}
