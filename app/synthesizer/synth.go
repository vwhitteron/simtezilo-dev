package synthesizer

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/codec"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
)

// Mixer channel names.
const (
	ChannelMaster       = "_master"
	ChannelChassis      = "chassis"
	ChannelEngine       = "engine"
	ChannelTransmission = "transmission"
)

// Synthesizer is the main synthesizer structure that holds the mixer, effects, and output device.
type Synthesizer struct {
	effects      *EffectsSampleBank
	log          zerolog.Logger
	mixer        *Mixer
	outputDevice *OutputDevice
	kinematics   *kinematics.State
	sampleRate   int
	outFile      *os.File
}

// SynthOpts holds the options for creating a new Synthesizer.
type SynthOpts struct {
	Config     *config.Synthesizer
	BaseConfig *config.Config // Base config for lock-free reads in mixer
	Logger     zerolog.Logger
	Kinematics *kinematics.State
}

// New creates a new Synthesizer instance with the provided options.
func New(opts *SynthOpts) (*Synthesizer, error) {
	synthesizer := &Synthesizer{
		effects:    NewEffectsSampleBank(),
		kinematics: opts.Kinematics,
		sampleRate: opts.Config.InternalSampleRateHz,
		log:        opts.Logger.With().Str("package", "synth").Logger(),
	}

	var err error

	bufferLength := 2 * time.Second

	// Pass full config for lock-free reads
	synthesizer.mixer, err = NewMixer(MixerConfig{
		Config:       opts.BaseConfig,
		BufferLength: bufferLength,
		SampleRateHz: opts.Config.InternalSampleRateHz,
		Log:          opts.Logger.With().Str("package", "synth mixer").Logger(),
	})
	if err != nil {
		return nil, fmt.Errorf("create mixer: %w", err)
	}

	// Add channels with initial values from config
	_ = synthesizer.mixer.AddChannel(ChannelTransmission, opts.Config.TransmissionGain)
	_ = synthesizer.mixer.AddChannel(ChannelChassis, opts.Config.ChassisGain)
	_ = synthesizer.mixer.AddChannel(ChannelEngine, opts.Config.EngineGain)

	synthesizer.outputDevice, err = NewOutputDevice(SynthOutDeviceOpts{
		Log: opts.Logger.With().Str("package", "synth output device").Logger(),
	})
	if err != nil {
		return nil, err
	}

	if opts.Config.OutputFile != "" {
		synthesizer.outFile, err = os.Create(opts.Config.OutputFile)
		if err != nil {
			return nil, fmt.Errorf("create output wav file: %w", err)
		}

		log.Info().Str("file", opts.Config.OutputFile).Msg("saving audio output")
	}

	return synthesizer, nil
}

// Close gracefully shuts down the synthesizer, closing the mixer and output file if applicable.
func (s *Synthesizer) Close() (err error) {
	s.mixer.Close()

	if s.outFile != nil {
		err = s.outFile.Close()
	}

	return err
}

// GetSampleRate returns the internal sample rate of the synthesizer.
func (s *Synthesizer) GetSampleRate() int {
	return s.sampleRate
}

// GetBufferCapacity returns the buffer capacity of the mixer in samples.
func (s *Synthesizer) GetBufferCapacity() int {
	return s.mixer.GetBufferCapacity()
}

// InspectChannelBuffer returns a copy of the specified channel buffer for inspection.
func (s *Synthesizer) InspectChannelBuffer(name string, length int, offset int) []float64 {
	return s.mixer.InspectChannelBuffer(name, length, offset)
}

// ReadBuffer mixes all channels to the master and returns the mixed sample data.
func (s *Synthesizer) ReadBuffer(length int) []float64 {
	s.mixer.MixToMaster(length)

	return s.mixer.ReadChannel(ChannelMaster, length)
}

// WriteBuffer writes the provided sample data to the specified channel buffer at the given offset.
func (s *Synthesizer) WriteBuffer(channel string, sample []float64, offset int) {
	magnitude, err := s.mixer.GetChannelPowerRatio(channel)
	if err != nil {
		s.log.Error().Err(err).Str("channel", channel).Msg("failed to get channel power ratio")

		return
	}

	_ = s.mixer.WriteChannel(channel, sample, magnitude, offset, false)
}

// OverwriteBuffer overwrites the specified channel buffer with the provided sample data at the given offset.
func (s *Synthesizer) OverwriteBuffer(channel string, sample []float64, offset int) {
	magnitude, err := s.mixer.GetChannelPowerRatio(channel)
	if err != nil {
		s.log.Error().Err(err).Str("channel", channel).Msg("failed to get channel power ratio")

		return
	}

	_ = s.mixer.WriteChannel(channel, sample, magnitude, offset, true)
}

// GetChannelMagnitude returns the current magnitude (gain) of the specified channel.
func (s *Synthesizer) GetChannelMagnitude(name string) (float64, error) {
	return s.mixer.GetChannelPowerRatio(name)
}

// FadeIn gradually increases the master gain from minimum to the configured level over the specified period.
func (s *Synthesizer) FadeIn(period time.Duration) {
	s.mixer.FadeIn(period)
}

// ApplyMasterGain applies the current master gain to the provided value and returns the adjusted value.
func (s *Synthesizer) ApplyMasterGain(value float64) float64 {
	outputGain, _ := s.mixer.GetChannelPowerRatio(ChannelMaster)

	return value * outputGain
}

// Silence immediately silences all mixeroutput and clears buffers.
func (s *Synthesizer) Silence() {
	s.mixer.SetFader(config.MinimumGain)
	s.mixer.silenced = true

	s.mixer.ClearBuffers()
}

// EffectSampleBank returns the effects sample bank.
func (s *Synthesizer) EffectSampleBank() *EffectsSampleBank {
	return s.effects
}

// GetEffectSample returns the raw sample data for the specified effect name.
func (s *Synthesizer) GetEffectSample(name string, sampleRate int) codec.PCMFloat64 {
	effectSample := s.effects.GetSample(name, sampleRate)

	return effectSample
}

// PlayEffect plays an effect at a given magnitude on a given channel.
func (s *Synthesizer) PlayEffect(name string, magnitude float64, channel string) {
	channelMagnitude, err := s.mixer.GetChannelPowerRatio(channel)
	if err != nil {
		s.log.Error().Err(err).Str("channel", name).Msg("failed to get channel power ratio")

		return
	}

	magnitude *= channelMagnitude

	effectSample := s.effects.GetSample(name, s.sampleRate)

	_ = s.mixer.WriteChannel(channel, effectSample.Samples(), magnitude, 0, false)
}
