package synthesizer

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

// Mixer handles multiple audio channels, mixing them into a master output channel.
type Mixer struct {
	config *config.Config

	channels     map[string]*MixerChannel
	bufferLength time.Duration
	sampleRateHz int
	log          zerolog.Logger
	faderGain    float64
	fadeInActive bool
	silenced     bool

	// Calibration mode state
	calibrator        *calibrator.Calibrator
	sineWavePhaseL    float64
	sineWavePhaseR    float64
	calibrationStereo bool

	// Buffer monitoring
	lastHealthCheck     time.Time
	healthCheckInterval time.Duration

	mu sync.RWMutex
}

// MixerChannel represents an individual audio channel within the mixer.
type MixerChannel struct {
	activeGain float64
	buffer     Buffer
}

// MixerConfig holds configuration options for the Mixer.
type MixerConfig struct {
	Config       *config.Config         // Full config reference for lock-free reads
	Calibrator   *calibrator.Calibrator // Calibration mode signal manager
	BufferLength time.Duration          // Duration of audio the buffer should hold
	SampleRateHz int                    // Sample rate in Hz
	Log          zerolog.Logger         // Logger instance for logging
}

// NewMixer creates a new Mixer instance with the provided configuration.
func NewMixer(mixerConfig MixerConfig) (*Mixer, error) {
	if mixerConfig.Config == nil {
		return nil, errors.New("config must be a valid pointer")
	}

	mixer := &Mixer{
		config: mixerConfig.Config,

		bufferLength:      mixerConfig.BufferLength,
		sampleRateHz:      mixerConfig.SampleRateHz,
		channels:          map[string]*MixerChannel{},
		log:               mixerConfig.Log,
		faderGain:         config.MinimumGain,
		fadeInActive:      false,
		silenced:          true,
		calibrator:        mixerConfig.Calibrator,
		sineWavePhaseL:    0,
		sineWavePhaseR:    0,
		calibrationStereo: false,

		// Initialize buffer monitoring
		lastHealthCheck:     time.Now(),
		healthCheckInterval: 5 * time.Second,
	}

	// Initialize master with lock-free config read
	masterGain := mixer.config.GetSynthMasterGain()

	err := mixer.AddChannel(ChannelMaster, masterGain)
	if err != nil {
		return nil, fmt.Errorf("add master channel: %w", err)
	}

	go mixer.watchForConfigChanges()

	return mixer, nil
}

// Close gracefully shuts down the mixer, silencing output.
func (m *Mixer) Close() {
	_ = m.SetChannelGain(ChannelMaster, config.MinimumGain)
}

// GetBufferCapacity returns the configured buffer length duration in samples.
func (m *Mixer) GetBufferCapacity() int {
	return int(m.bufferLength.Seconds() * float64(m.sampleRateHz))
}

// AddChannel adds a new channel to the mixer with the specified name and initial gain.
func (m *Mixer) AddChannel(name string, initialGain float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; ok {
		return fmt.Errorf("channel %q already exists", name)
	}

	m.channels[name] = &MixerChannel{
		activeGain: initialGain,
		buffer:     NewAdaptiveBuffer(m.bufferLength, m.sampleRateHz),
	}

	return nil
}

// Read reads the specified number of samples from the channel's buffer.
// All samples read are removed from the buffer.
func (m *MixerChannel) Read(length int) []float64 {
	return m.buffer.Read(length)
}

// Write writes samples to the channel's buffer with the specified magnitude and offset.
func (m *MixerChannel) Write(samples []float64, magnitude float64, offset int, overwrite bool) {
	ScaleSamples(&samples, magnitude)

	m.buffer.Write(samples, offset, overwrite)
}

// WriteChannel writes the provided sample data to the specified channel buffer at the given offset.
func (m *Mixer) WriteChannel(name string, samples []float64, magnitude float64, offset int, overwrite bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; !ok {
		return fmt.Errorf("channel not found: %q", name)
	}

	m.channels[name].Write(samples, magnitude, offset, overwrite)

	return nil
}

// ReadChannel reads the specified number of samples from the channel's buffer.
func (m *Mixer) ReadChannel(name string, length int) []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return nil
	}

	// Check if channel is muted using lock-free config reads
	var muted bool

	switch name {
	case ChannelMaster:
		muted = m.config.GetSynthMasterMute()
	case ChannelChassis:
		muted = m.config.GetSynthChassisMute()
	case ChannelTransmission:
		muted = m.config.GetSynthTransmissionMute()
	case ChannelEngine:
		muted = m.config.GetSynthEngineMute()
	case ChannelCalibrator:
		muted = false
	}

	if muted {
		// Return silence for muted channels
		return make([]float64, length)
	}

	// Check buffer health periodically
	m.checkBufferHealth()

	return channel.Read(length)
}

// InspectChannelBuffer returns a copy of the specified channel buffer for inspection.
func (m *Mixer) InspectChannelBuffer(name string, length int, offset int) []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if channel, ok := m.channels[name]; ok {
		return channel.buffer.Inspect(length, offset)
	}

	return nil
}

// GetChannelBufferLength returns the current length of samples in the specified channel's buffer.
func (m *Mixer) GetChannelBufferLength(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if channel, ok := m.channels[name]; ok {
		return channel.buffer.Length()
	}

	return 0
}

// GetChannelNames returns a list of all channel names configured in the mixer.
func (m *Mixer) GetChannelNames() []string {
	names := []string{}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for name := range m.channels {
		// skip internal channels
		if name[0:1] == "_" {
			continue
		}

		names = append(names, name)
	}

	return names
}

// GetChannelGain returns the current gain of the specified channel.
func (m *Mixer) GetChannelGain(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return channel.activeGain, nil
}

// SetChannelGain sets the gain of the specified channel.
func (m *Mixer) SetChannelGain(name string, gain float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	channel, ok := m.channels[name]
	if !ok {
		return fmt.Errorf("channel %q does not exist", name)
	}

	if channel.activeGain == gain {
		return nil
	}

	channel.activeGain = gain
	m.channels[name] = channel

	return nil
}

// GetChannelPowerRatio returns the current power ratio of the specified channel.
func (m *Mixer) GetChannelPowerRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToPowerRatio(channel.activeGain), nil
}

// GetChannelAmplitudeRatio returns the current amplitude ratio of the specified channel.
func (m *Mixer) GetChannelAmplitudeRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToAmplitudeRatio(channel.activeGain), nil
}

// SetFader sets the fader gain, which controls the overall output level.
func (m *Mixer) SetFader(gain float64) {
	m.mu.Lock()
	m.faderGain = gain
	m.mu.Unlock()

	_ = m.SetChannelGain(ChannelMaster, m.faderGain)
}

// FadeIn gradually increases the master gain from minimum to the configured level over the specified period.
func (m *Mixer) FadeIn(period time.Duration) {
	m.mu.RLock()
	master := m.channels[ChannelMaster]
	m.mu.RUnlock()

	// Lock-free config read
	targetGain := m.config.GetSynthMasterGain()

	if master.activeGain == targetGain || m.fadeInActive {
		return
	}

	go func() {
		m.silenced = false
		m.fadeInActive = true

		fadeInInterval := 50 * time.Millisecond
		incrementTime := float64(period.Milliseconds() / fadeInInterval.Milliseconds())
		fadeInIncrement := (targetGain - m.faderGain) / incrementTime

		m.log.Debug().
			Float64("current", m.faderGain).
			Float64("target", targetGain).
			Str("state", "begin").
			Msg("fade in")

		for {
			m.faderGain += fadeInIncrement

			// fade in complete
			if m.faderGain >= targetGain {
				m.mu.Lock()
				m.faderGain = targetGain
				m.mu.Unlock()
				_ = m.SetChannelGain(ChannelMaster, targetGain)

				break
			}

			_ = m.SetChannelGain(ChannelMaster, m.faderGain)

			time.Sleep(fadeInInterval)
		}

		m.fadeInActive = false

		m.mu.RLock()
		master := m.channels[ChannelMaster]
		m.mu.RUnlock()

		m.log.Debug().
			Float64("current", master.activeGain).
			Float64("target", targetGain).
			Str("state", "complete").
			Msg("fade in")
	}()
}

// MixToMaster mixes all active channels into the master channel buffer using an alternative algorithm.
func (m *Mixer) MixToMaster(length int) {
	outSamples := make([]float64, length)

	m.mu.RLock()

	// Check if calibration mode is enabled
	if m.calibrator != nil && m.calibrator.IsEnabled() {
		frequency := m.calibrator.GetFrequency()
		channel := m.calibrator.GetChannel()

		// Generate sine wave samples
		for i := range outSamples {
			// For mono (both channels), use phase L only
			outSamples[i] = math.Sin(m.sineWavePhaseL)

			// Increment phase
			m.sineWavePhaseL += 2 * math.Pi * frequency / float64(m.sampleRateHz)

			// Keep phase in reasonable range
			if m.sineWavePhaseL > 2*math.Pi {
				m.sineWavePhaseL -= 2 * math.Pi
			}
		}

		masterChannel := m.channels[ChannelMaster]
		m.mu.RUnlock()

		// Update stereo flag after releasing read lock
		m.mu.Lock()
		m.calibrationStereo = (channel != calibrator.OutputChannelBoth)
		m.mu.Unlock()

		masterChannel.Write(outSamples, 1.0, 0, true)

		return
	}

	// Reset calibration stereo mode when not calibrating
	m.mu.RUnlock()
	m.mu.Lock()
	m.calibrationStereo = false
	m.mu.Unlock()
	m.mu.RLock()

	// Normal haptic mode - mix chassis, transmission, and engine
	// mix in the chassis and transmission channels with equal priority
	var peak float64

	for _, name := range []string{ChannelChassis, ChannelTransmission} {
		channel, ok := m.channels[name]
		if !ok {
			m.mu.RUnlock()
			m.log.Error().Str("channel", name).Msg("channel not found in mixer")
			m.mu.RLock()

			continue
		}

		// Lock-free config reads for mute state
		var muted bool
		if name == ChannelChassis {
			muted = m.config.GetSynthChassisMute()
		} else {
			muted = m.config.GetSynthTransmissionMute()
		}

		if muted {
			continue
		}

		samples := channel.Read(length)

		for i, sample := range samples {
			outSamples[i] = mixSampleSum(outSamples[i], sample, &peak)
		}
	}

	if peak > 1.0 {
		scaleSamplesPeak(&outSamples, peak)
	}

	// mix in the engine channel with lower priority
	m.mixEngineChannel(outSamples, length)

	masterChannel := m.channels[ChannelMaster]
	m.mu.RUnlock()

	magnitude := 1.0

	masterChannel.Write(outSamples, magnitude, 0, true)
}

// ClearBuffers clears all channel buffers in the mixer.
func (m *Mixer) ClearBuffers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, channel := range m.channels {
		channel.buffer.Clear()
	}
}

// ClearChannelBuffer clears a specific channel's buffer.
func (m *Mixer) ClearChannelBuffer(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if channel, ok := m.channels[name]; ok {
		channel.buffer.Clear()
	}
}

// ResetSineWavePhase resets the sine wave phase to zero for the calibrator.
func (m *Mixer) ResetSineWavePhase() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sineWavePhaseL = 0
	m.sineWavePhaseR = 0
}

// IsCalibrationStereo returns whether calibration mode is outputting stereo.
func (m *Mixer) IsCalibrationStereo() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.calibrationStereo
}

// GetCalibrationChannel returns the current calibration output channel setting.
func (m *Mixer) GetCalibrationChannel() calibrator.OutputChannel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.calibrator == nil {
		return calibrator.OutputChannelBoth
	}

	return m.calibrator.GetChannel()
}

// mixEngineChannel mixes the engine channel into the output samples with lower priority.
func (m *Mixer) mixEngineChannel(outSamples []float64, length int) {
	channel, ok := m.channels[ChannelEngine]
	if !ok {
		m.mu.RUnlock()
		m.log.Error().Str("channel", ChannelEngine).Msg("channel not found in mixer")
		m.mu.RLock()

		return
	}

	// Lock-free config read for mute state
	if m.config.GetSynthEngineMute() {
		return
	}

	engineSamples := channel.Read(length)
	if len(engineSamples) == 0 {
		return
	}

	m.processEngineSamples(outSamples, engineSamples)
}

// processEngineSamples processes and mixes engine samples into the output.
func (m *Mixer) processEngineSamples(outSamples, engineSamples []float64) {
	outSamplesWork := make([]float64, len(outSamples))

	// Perform direct mix and get the peak value
	for index, engineSample := range engineSamples {
		peak := 0.0

		engineScaled := engineSample

		engineMax := 1.0 - signal.Abs(outSamples[index])
		if engineSample > engineMax || engineSample < -engineMax {
			engineScaled = engineMax * engineSample
		}

		mixed := mixSampleSum(outSamples[index], engineScaled, &peak)

		outSamplesWork[index] = mixed

		if mixed > 1.0 || mixed < -1.0 {
			m.log.Warn().
				Float64("sample", outSamples[index]).
				Float64("engine", engineSample).
				Float64("engineScaled", engineScaled).
				Float64("mixed", mixed).
				Float64("peak", peak).
				Msg("clipping")
		}
	}

	copy(outSamples, outSamplesWork)
}

// checkBufferHealth monitors buffer health and logs issues.
func (m *Mixer) checkBufferHealth() {
	now := time.Now()
	if now.Sub(m.lastHealthCheck) < m.healthCheckInterval {
		return
	}

	m.lastHealthCheck = now

	for name, channel := range m.channels {
		if name == ChannelMaster {
			continue
		}

		// Check if channel is muted using lock-free config reads
		var muted bool

		switch name {
		case ChannelChassis:
			muted = m.config.GetSynthChassisMute()
		case ChannelTransmission:
			muted = m.config.GetSynthTransmissionMute()
		case ChannelEngine:
			muted = m.config.GetSynthEngineMute()
		}

		if muted {
			continue
		}

		// Check if the buffer supports health monitoring
		if adaptiveBuffer, ok := channel.buffer.(*AdaptiveBuffer); ok {
			overflows, underruns, fillRatio := adaptiveBuffer.Health()

			if overflows > 0 || underruns > 0 || fillRatio > 0.9 || fillRatio < 0.1 {
				m.log.Debug().
					Bool("healthy", false).
					Str("channel", name).
					Int("overflows", overflows).
					Int("underruns", underruns).
					Float64("fillRatio", fillRatio).
					Msg("buffer")
			}
		}
	}
}

// watchForConfigChanges monitors configuration changes and applies them to the mixer channels.
func (m *Mixer) watchForConfigChanges() {
	m.log.Debug().Str("event", "start").Msg("config watch")

	// Track previous mute states to detect changes
	previousMuteStates := make(map[string]bool)

	for {
		time.Sleep(200 * time.Millisecond)

		if m.fadeInActive || m.silenced {
			continue
		}

		m.mu.RLock()

		for name, channel := range m.channels {
			// Lock-free config reads
			var (
				configGain float64
				configMute bool
			)

			switch name {
			case ChannelMaster:
				// Master gain updates are skipped during calibration mode
				if m.calibrator != nil && m.calibrator.IsEnabled() {
					continue
				}

				configGain = m.config.GetSynthMasterGain()
				configMute = m.config.GetSynthMasterMute()
			case ChannelChassis:
				configGain = m.config.GetSynthChassisGain()
				configMute = m.config.GetSynthChassisMute()
			case ChannelTransmission:
				configGain = m.config.GetSynthTransmissionGain()
				configMute = m.config.GetSynthTransmissionMute()
			case ChannelEngine:
				configGain = m.config.GetSynthEngineGain()
				configMute = m.config.GetSynthEngineMute()
			default:
				continue
			}

			// Check if mute state changed
			prevMute, existed := previousMuteStates[name]
			if !existed || prevMute != configMute {
				previousMuteStates[name] = configMute

				// Clear buffer immediately when channel is muted for instant response
				if configMute {
					m.mu.RUnlock()
					m.mu.Lock()
					channel.buffer.Clear()
					m.mu.Unlock()
					m.log.Debug().Str("channel", name).Bool("muted", configMute).Str("event", "mute").Msg("config watch")
					m.mu.RLock()
				}
			}

			// Check if gain changed
			if channel.activeGain == configGain {
				continue
			}

			m.mu.RUnlock()
			_ = m.SetChannelGain(name, configGain)

			if name == ChannelMaster {
				if m.faderGain != channel.activeGain {
					m.mu.Lock()
					m.faderGain = channel.activeGain
					m.mu.Unlock()
				}
			}

			m.log.Debug().Str("channel", name).Float64("gain", configGain).Str("event", "change").Msg("config watch")
			m.mu.RLock()
		}

		m.mu.RUnlock()
	}
}
