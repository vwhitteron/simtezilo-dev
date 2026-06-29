package synthesizer

import (
	"context"
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

// ChannelDiagnostic holds per-channel buffer health metrics.
type ChannelDiagnostic struct {
	Name   string
	Muted  bool
	Health BufferHealth
}

// MixerDiagnostics aggregates diagnostic information from all mixer channels.
type MixerDiagnostics struct {
	Timestamp    time.Time
	Channels     []ChannelDiagnostic
	FaderGain    float64
	Silenced     bool
	FadeInActive bool
}

// StereoMixer handles two audio channels, mixing them into a master output channel.
type StereoMixer struct {
	config *config.Config

	channels            map[string]*MixerChannel
	bufferLength        time.Duration
	sampleRateHz        int
	cushionMs           int
	numOutputChannels   int
	outputChannelNames  []string
	chassisChannelNames []string

	// Reusable scratch for the mix path, AUDIO-CALLBACK-THREAD ONLY (see
	// MixToMaster). Grown on demand to avoid per-frame heap allocations.
	mixOutSamples          channelBuffer
	mixChannelSamples      deviceBuffer
	mixPeaks               channelValues
	engineWorkScratch      deviceBuffer
	calibratorEqAmplitudes channelValues

	log          zerolog.Logger
	faderGain    float64
	fadeInActive bool
	silenced     bool

	// Calibration mode state
	calibrator     calibrator.Calibrator
	sineWavePhaseL float64
	sineWavePhaseR float64

	// Buffer monitoring
	lastHealthCheck     time.Time
	healthCheckInterval time.Duration

	// Lifecycle management
	ctx    context.Context //nolint:containedctx // Context for managing lifecycle
	cancel context.CancelFunc

	mu sync.RWMutex
}

// MixerChannel represents an individual audio channel within the mixer.
type MixerChannel struct {
	activeGain float64
	buffer     Buffer
}

// StereoMixerConfig holds configuration options for the Mixer.
type StereoMixerConfig struct {
	Config       *config.Config        // Full config reference for lock-free reads
	Calibrator   calibrator.Calibrator // Calibration mode signal manager
	BufferLength time.Duration         // Duration of audio the buffer should hold
	SampleRateHz int                   // Sample rate in Hz
	Log          zerolog.Logger        // Logger instance for logging
}

// NewStereoMixer creates a new Mixer instance with the provided configuration.
func NewStereoMixer(mixerConfig StereoMixerConfig) (*StereoMixer, error) {
	if mixerConfig.Config == nil {
		return nil, errors.New("config must be a valid pointer")
	}

	ctx, cancel := context.WithCancel(context.Background())

	numOutputChannels := DefaultOutputChannels
	if n := mixerConfig.Config.GetAudioHapticsChannels(); n > 0 {
		numOutputChannels = n
	}

	mixer := &StereoMixer{
		config: mixerConfig.Config,

		bufferLength:      mixerConfig.BufferLength,
		sampleRateHz:      mixerConfig.SampleRateHz,
		cushionMs:         mixerConfig.Config.GetAudioHapticsCushionMs(),
		numOutputChannels: numOutputChannels,

		outputChannelNames:  buildChannelNames(outputChannelPrefix, numOutputChannels),
		chassisChannelNames: buildChannelNames(chassisChannelPrefix, numOutputChannels),

		channels:       map[string]*MixerChannel{},
		log:            mixerConfig.Log,
		faderGain:      config.MinimumGain,
		fadeInActive:   false,
		silenced:       true,
		calibrator:     mixerConfig.Calibrator,
		sineWavePhaseL: 0,
		sineWavePhaseR: 0,

		// Initialize buffer monitoring
		lastHealthCheck:     time.Now(),
		healthCheckInterval: 5 * time.Second,

		// Lifecycle management
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize master with lock-free config read
	masterGain := mixer.config.GetSynthMasterGain()

	err := mixer.AddChannel(ChannelMaster, masterGain)
	if err != nil {
		return nil, fmt.Errorf("add master channel: %w", err)
	}

	// Initialize per-channel output channels
	for ch := range numOutputChannels {
		channelGain := mixer.config.GetSynthChannelGain(ch)

		err = mixer.AddChannel(OutputChannelName(ch), channelGain)
		if err != nil {
			return nil, fmt.Errorf("add output channel %d: %w", ch, err)
		}
	}

	go mixer.watchForConfigChanges()

	return mixer, nil
}

// Close gracefully shuts down the mixer, silencing output.
func (m *StereoMixer) Close() {
	_ = m.SetChannelGain(ChannelMaster, config.MinimumGain)

	// Cancel context to stop background goroutines
	if m.cancel != nil {
		m.cancel()
	}
}

// GetBufferCapacity returns the configured buffer length duration in samples.
func (m *StereoMixer) GetBufferCapacity() int {
	return int(m.bufferLength.Seconds() * float64(m.sampleRateHz))
}

// AddChannel adds a new channel to the mixer with the specified name and initial gain.
func (m *StereoMixer) AddChannel(name string, initialGain float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; ok {
		return fmt.Errorf("channel %q already exists", name)
	}

	m.channels[name] = &MixerChannel{
		activeGain: initialGain,
		buffer:     NewAdaptiveBufferCushion(m.bufferLength, m.sampleRateHz, m.cushionMs),
	}

	return nil
}

// OutputChannelName returns the precomputed channel name for output channel ch.
func (m *StereoMixer) OutputChannelName(ch int) string {
	return m.outputChannelNames[ch]
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
func (m *StereoMixer) WriteChannel(name string, samples []float64, magnitude float64, offset int, overwrite bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.channels[name]; !ok {
		return fmt.Errorf("channel not found: %q", name)
	}

	m.channels[name].Write(samples, magnitude, offset, overwrite)

	return nil
}

// ChannelDepth reports the unread buffered depth of a channel in samples. The
// engine channel uses it to refill its small cushion each tick (top up to a
// target depth) instead of accumulating latency.
func (m *StereoMixer) ChannelDepth(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0
	}

	return channel.buffer.Used()
}

// ReadChannel reads the specified number of samples from the channel's buffer.
func (m *StereoMixer) ReadChannel(name string, length int) []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return nil
	}

	// Check if channel is muted using lock-free config reads
	var muted bool

	switch {
	case name == ChannelMaster:
		muted = m.config.GetSynthMasterMute()
	case IsChassisChannel(name):
		muted = m.config.GetSynthChassisMute()
	case name == ChannelTransmission:
		muted = m.config.GetSynthTransmissionMute()
	case name == ChannelEngine:
		muted = m.config.GetSynthEngineMute()
	case name == ChannelCalibrator:
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
func (m *StereoMixer) InspectChannelBuffer(name string, length int, offset int) []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if channel, ok := m.channels[name]; ok {
		return channel.buffer.Inspect(length, offset)
	}

	return nil
}

// GetChannelBufferLength returns the current length of samples in the specified channel's buffer.
func (m *StereoMixer) GetChannelBufferLength(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if channel, ok := m.channels[name]; ok {
		return channel.buffer.Length()
	}

	return 0
}

// GetChannelNames returns a list of all channel names configured in the mixer.
func (m *StereoMixer) GetChannelNames() []string {
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
func (m *StereoMixer) GetChannelGain(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return channel.activeGain, nil
}

// SetChannelGain sets the gain of the specified channel.
func (m *StereoMixer) SetChannelGain(name string, gain float64) error {
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
func (m *StereoMixer) GetChannelPowerRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToPowerRatio(channel.activeGain), nil
}

// GetChannelAmplitudeRatio returns the current amplitude ratio of the specified channel.
func (m *StereoMixer) GetChannelAmplitudeRatio(name string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	channel, ok := m.channels[name]
	if !ok {
		return 0, fmt.Errorf("channel %q does not exist", name)
	}

	return GainToAmplitudeRatio(channel.activeGain), nil
}

// fadeInRunning reports whether a fade-in goroutine is active, under the lock.
func (m *StereoMixer) fadeInRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.fadeInActive
}

// setFadeInActive sets the fade-in-active flag under the lock.
func (m *StereoMixer) setFadeInActive(active bool) {
	m.mu.Lock()
	m.fadeInActive = active
	m.mu.Unlock()
}

// getFaderGain reads the fader gain under the lock.
func (m *StereoMixer) getFaderGain() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.faderGain
}

// setFaderGain writes the fader gain under the lock.
func (m *StereoMixer) setFaderGain(gain float64) {
	m.mu.Lock()
	m.faderGain = gain
	m.mu.Unlock()
}

// SetFader sets the fader gain, which controls the overall output level.
func (m *StereoMixer) SetFader(gain float64) {
	m.setFaderGain(gain)

	_ = m.SetChannelGain(ChannelMaster, gain)
}

// FadeIn gradually increases the master gain from minimum to the configured level over the specified period.
func (m *StereoMixer) FadeIn(period time.Duration) {
	m.mu.RLock()
	masterGain := m.channels[ChannelMaster].activeGain
	m.mu.RUnlock()

	// Lock-free config read
	targetGain := m.config.GetSynthMasterGain()

	// FadeIn is the explicit "resume output" signal, so lift silence here
	// unconditionally. The async producer idles while silenced (SetIdleCheck);
	// if we only cleared this inside the goroutine below it would stay set
	// whenever the fader is already at target (e.g. a disable/enable race during
	// a session restart left the gain ramped up but silenced still true),
	// leaving the producer idle and haptics dead until the next Silence().
	m.mu.Lock()
	m.silenced = false
	m.mu.Unlock()

	if masterGain == targetGain || m.fadeInRunning() {
		return
	}

	go func() {
		m.setFadeInActive(true)

		fadeInInterval := 50 * time.Millisecond
		incrementTime := float64(period.Milliseconds() / fadeInInterval.Milliseconds())
		fadeInIncrement := (targetGain - m.getFaderGain()) / incrementTime

		m.log.Debug().
			Float64("current", m.getFaderGain()).
			Float64("target", targetGain).
			Str("state", "begin").
			Msg("fade in")

		for {
			gain := m.getFaderGain() + fadeInIncrement

			// fade in complete
			if gain >= targetGain {
				m.setFaderGain(targetGain)
				_ = m.SetChannelGain(ChannelMaster, targetGain)

				break
			}

			m.setFaderGain(gain)
			_ = m.SetChannelGain(ChannelMaster, gain)

			time.Sleep(fadeInInterval)
		}

		m.setFadeInActive(false)

		m.mu.RLock()
		currentGain := m.channels[ChannelMaster].activeGain
		m.mu.RUnlock()

		m.log.Debug().
			Float64("current", currentGain).
			Float64("target", targetGain).
			Str("state", "complete").
			Msg("fade in")
	}()
}

// channelBuffer holds one channel's audio samples.
type channelBuffer []float64

// grow resizes the channel buffer to the specified length.
func (b *channelBuffer) grow(length int) {
	if cap(*b) < length {
		*b = make(channelBuffer, length)
	}

	*b = (*b)[:length]
}

// zero overwrites all elements in the channel buffer with zeros.
func (b *channelBuffer) zero() {
	for i := range *b {
		(*b)[i] = 0
	}
}

// scalePeak reduces the amplitude of the channel buffer to no larger than the specified peak.
func (b *channelBuffer) scalePeak(peak float64) {
	scaleSamplesPeak((*[]float64)(b), peak)
}

// deviceBuffer holds one channelBuffer per output channel (channels x length).
type deviceBuffer []channelBuffer

// grow resizes the device buffer to the specified number of channels and length.
func (d *deviceBuffer) grow(channels, length int) {
	if cap(*d) < channels {
		*d = make(deviceBuffer, channels)
	}

	*d = (*d)[:channels]

	for ch := range channels {
		(*d)[ch].grow(length)
	}
}

// channelValues holds one scalar per output channel.
type channelValues []float64

// grow resizes the channel values slice to the specified number of channels.
func (v *channelValues) grow(channels int) {
	if cap(*v) < channels {
		*v = make(channelValues, channels)
	}

	*v = (*v)[:channels]
}

// zero overwrites all elements in the channel values slice with zeros.
func (v *channelValues) zero() {
	for i := range *v {
		(*v)[i] = 0
	}
}

// MixToMaster mixes all active channels into the master channel buffer using an alternative algorithm.
//
// MixToMaster must never run concurrently with itself: it reuses the
// callback-thread-only scratch fields on StereoMixer (mixOutSamples,
// mixChannelSamples, mixPeaks, and—via the helpers it calls—engineWorkScratch
// and calibratorEqAmplitudes). This holds today because it runs only from the
// single audio callback.
func (m *StereoMixer) MixToMaster(length int) {
	// Reusable callback-thread-only scratch (grown on demand). See struct doc.
	m.mixOutSamples.grow(length)
	outSamples := m.mixOutSamples

	m.mu.RLock()

	if m.mixCalibratorOutput(outSamples) {
		return
	}

	// Normal haptic mode - mix per-channel chassis with transmission and engine.
	// Separate output buffers per channel support per-channel EQ.
	m.mixChannelSamples.grow(m.numOutputChannels, length)
	m.mixPeaks.grow(m.numOutputChannels)

	channelSamples := m.mixChannelSamples
	peaks := m.mixPeaks

	// Scratch is reused across calls; clear the accumulators before mixing.
	for ch := range m.numOutputChannels {
		channelSamples[ch].zero()
	}

	peaks.zero()

	// Mix per-channel chassis with appropriate EQ
	chassisMuted := m.config.GetSynthChassisMute()
	if !chassisMuted {
		for ch := range m.numOutputChannels {
			if chassisCh, ok := m.channels[m.chassisChannelNames[ch]]; ok {
				samples := chassisCh.Read(length)
				for i, sample := range samples {
					channelSamples[ch][i] = mixSampleSum(channelSamples[ch][i], sample, &peaks[ch])
				}
			}
		}
	}

	// Mix transmission into all outputs (shared channel)
	if transmissionChannel, ok := m.channels[ChannelTransmission]; ok {
		transmissionMuted := m.config.GetSynthTransmissionMute()
		if !transmissionMuted {
			samples := transmissionChannel.Read(length)
			for i, sample := range samples {
				for ch := range m.numOutputChannels {
					channelSamples[ch][i] = mixSampleSum(channelSamples[ch][i], sample, &peaks[ch])
				}
			}
		}
	}

	// Scale peaks for each channel
	for ch := range m.numOutputChannels {
		if peaks[ch] > 1.0 {
			channelSamples[ch].scalePeak(peaks[ch])
		}
	}

	// Mix engine channel into all outputs with lower priority
	m.mixEngineChannelMulti(channelSamples, length)

	m.mu.RUnlock()

	magnitude := 1.0

	// Write the per-channel outputs. The master channel carries only the live
	// master gain (applied downstream by the Streamer); it no longer holds a
	// sample buffer, so nothing is mixed into it here.
	for channel := range m.numOutputChannels {
		m.channels[m.outputChannelNames[channel]].Write(channelSamples[channel], magnitude, 0, true)
	}
}

// ClearBuffers clears all channel buffers in the mixer.
func (m *StereoMixer) ClearBuffers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, channel := range m.channels {
		channel.buffer.Clear()
	}
}

// ClearChannelBuffer clears a specific channel's buffer.
func (m *StereoMixer) ClearChannelBuffer(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if channel, ok := m.channels[name]; ok {
		channel.buffer.Clear()
	}
}

// ResetSineWavePhase resets the sine wave phase to zero for the calibrator.
func (m *StereoMixer) ResetSineWavePhase() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sineWavePhaseL = 0
	m.sineWavePhaseR = 0
}

// checkBufferHealth monitors buffer health and logs issues at appropriate levels.
func (m *StereoMixer) checkBufferHealth() {
	now := time.Now()
	if now.Sub(m.lastHealthCheck) < m.healthCheckInterval {
		return
	}

	m.lastHealthCheck = now

	for name, channel := range m.channels {
		if name == ChannelMaster {
			continue
		}

		m.logChannelHealth(name, channel)
	}
}

// logChannelHealth inspects a single channel buffer and logs any health issues.
func (m *StereoMixer) logChannelHealth(name string, channel *MixerChannel) {
	adaptiveBuffer, ok := channel.buffer.(*AdaptiveBuffer)
	if !ok {
		return
	}

	detail := adaptiveBuffer.HealthDetailed()

	if detail.Overflows > 0 || detail.Underruns > 0 {
		m.logBufferIssue(name, detail)

		return
	}

	if detail.FillRatio > 0.9 || detail.FillRatio < 0.1 {
		m.logFillRatioWarning(name, detail)
	}
}

// logBufferIssue logs a warning when overflows or underruns are detected.
func (m *StereoMixer) logBufferIssue(name string, detail BufferHealth) {
	logEntry := m.log.Debug().
		Str("channel", name).
		Int("overflows", detail.Overflows).
		Int("underruns", detail.Underruns).
		Float64("fillRatio", detail.FillRatio)

	if !detail.LastOverflow.IsZero() {
		logEntry.Dur("lastOverflowAgo", time.Since(detail.LastOverflow))
	}

	if !detail.LastUnderrun.IsZero() {
		logEntry.Dur("lastUnderrunAgo", time.Since(detail.LastUnderrun))
	}

	logEntry.Msg("buffer health issue detected")
}

// logFillRatioWarning logs an info message when fill ratio is outside normal range.
func (m *StereoMixer) logFillRatioWarning(name string, detail BufferHealth) {
	m.log.Debug().
		Str("channel", name).
		Float64("fillRatio", detail.FillRatio).
		Int("used", detail.Used).
		Int("capacity", detail.Capacity).
		Msg("buffer fill ratio outside normal range")
}

// Diagnostics returns a snapshot of mixer channel buffer health for all channels.
func (m *StereoMixer) Diagnostics() MixerDiagnostics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var gain float64
	if mc, ok := m.channels[ChannelMaster]; ok {
		gain = mc.activeGain
	}

	diag := MixerDiagnostics{
		Timestamp:    time.Now(),
		FaderGain:    gain,
		Silenced:     m.silenced,
		FadeInActive: m.fadeInActive,
		Channels:     make([]ChannelDiagnostic, 0, len(m.channels)),
	}

	for name, channel := range m.channels {
		var isMuted bool

		switch {
		case name == ChannelMaster:
			isMuted = m.config.GetSynthMasterMute()
		case IsChassisChannel(name):
			isMuted = m.config.GetSynthChassisMute()
		case name == ChannelTransmission:
			isMuted = m.config.GetSynthTransmissionMute()
		case name == ChannelEngine:
			isMuted = m.config.GetSynthEngineMute()
		case IsOutputChannel(name):
			chIndex := ParseOutputChannelIndex(name)
			if chIndex >= 0 {
				isMuted = m.config.GetSynthChannelMute(chIndex)
			}
		}

		var health BufferHealth

		if adaptiveBuffer, ok := channel.buffer.(*AdaptiveBuffer); ok {
			m.mu.RUnlock()

			health = adaptiveBuffer.HealthDetailed()

			m.mu.RLock()
		}

		diag.Channels = append(diag.Channels, ChannelDiagnostic{
			Name:   name,
			Muted:  isMuted,
			Health: health,
		})
	}

	return diag
}

// watchForConfigChanges monitors configuration changes and applies them to the mixer channels.
func (m *StereoMixer) watchForConfigChanges() {
	m.log.Debug().Str("event", "start").Msg("config watch")

	// Track previous mute states to detect changes
	previousMuteStates := make(map[string]bool)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			m.log.Debug().Str("event", "stop").Msg("config watch")

			return
		case <-ticker.C:
		}

		m.mu.RLock()
		skip := m.fadeInActive || m.silenced
		m.mu.RUnlock()

		if skip {
			continue
		}

		m.mu.RLock()

		for name, channel := range m.channels {
			// Lock-free config reads
			var (
				configGain float64
				configMute bool
			)

			switch {
			case name == ChannelMaster:
				configGain = m.config.GetSynthMasterGain()
				configMute = m.config.GetSynthMasterMute()
			case IsChassisChannel(name):
				configGain = m.config.GetSynthChassisGain()
				configMute = m.config.GetSynthChassisMute()
			case name == ChannelTransmission:
				configGain = m.config.GetSynthTransmissionGain()
				configMute = m.config.GetSynthTransmissionMute()
			case name == ChannelEngine:
				configGain = m.config.GetSynthEngineGain()
				configMute = m.config.GetSynthEngineMute()
			case IsOutputChannel(name):
				chIndex := ParseOutputChannelIndex(name)
				if chIndex >= 0 {
					configGain = m.config.GetSynthChannelGain(chIndex)
					configMute = m.config.GetSynthChannelMute(chIndex)
				} else {
					continue
				}
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
				if m.getFaderGain() != configGain {
					m.setFaderGain(configGain)
				}
			}

			m.log.Debug().Str("channel", name).Float64("gain", configGain).Str("event", "change").Msg("config watch")
			m.mu.RLock()
		}

		m.mu.RUnlock()
	}
}

// GetChannelMute returns the mute state for the specified channel index.
func (m *StereoMixer) GetChannelMute(channel int) bool {
	return m.config.GetSynthChannelMute(channel)
}

// GetMasterMute returns the master mute state.
func (m *StereoMixer) GetMasterMute() bool {
	return m.config.GetSynthMasterMute()
}

// SetSilenced sets the silenced state of the mixer.
func (m *StereoMixer) SetSilenced(silenced bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.silenced = silenced
}

// IsSilenced reports whether the mixer is currently silenced (telemetry inactive).
func (m *StereoMixer) IsSilenced() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.silenced
}

// mixCalibratorOutput handles output generation when the calibrator is active or stopping.
// It must be called with m.mu.RLock held; it releases the lock before returning.
// Returns true if calibration output was generated (caller must return immediately).
func (m *StereoMixer) mixCalibratorOutput(outSamples []float64) bool { //nolint:gocognit,cyclop // calibration algorithm; complexity is inherent, not incidental
	if m.calibrator == nil || (!m.calibrator.IsEnabled() && !m.calibrator.IsStopping()) {
		return false
	}

	frequency := m.calibrator.GetSweepFrequency()
	isStopping := m.calibrator.IsStopping()
	length := len(outSamples)

	// Compute per-channel EQ amplitude multipliers. Reusable callback-thread-only
	// scratch (fully overwritten below, so no clear needed).
	m.calibratorEqAmplitudes.grow(m.numOutputChannels)
	eqAmplitudes := m.calibratorEqAmplitudes

	for channelIndex := range m.numOutputChannels {
		eqAmplitudes[channelIndex] = 1.0

		if m.config.GetSynthChannelEqEnabled(channelIndex) {
			curve, minFreq, resolution := m.config.GetSynthChannelEqCurve(channelIndex)
			if len(curve) > 0 {
				index := int((frequency - minFreq) / resolution)
				if index >= 0 && index < len(curve) {
					eqAmplitudes[channelIndex] = curve[index]
				}
			}
		}
	}

	// Reuse the shared per-channel scratch (fully overwritten per offset below,
	// so no clear needed). Safe because the calibrator path and the normal mix
	// path never run within the same MixToMaster call.
	m.mixChannelSamples.grow(m.numOutputChannels, length)
	channelSamples := m.mixChannelSamples

	var prevPhase float64

	for offset := range outSamples {
		prevPhase = m.sineWavePhaseL

		baseSample := math.Sin(m.sineWavePhaseL)
		outSamples[offset] = baseSample

		for ch := range m.numOutputChannels {
			channelSamples[ch][offset] = baseSample * eqAmplitudes[ch]
		}

		m.sineWavePhaseL += 2 * math.Pi * frequency / float64(m.sampleRateHz)
		if m.sineWavePhaseL > 2*math.Pi {
			m.sineWavePhaseL -= 2 * math.Pi
		}

		// Detect zero crossing when stopping: end on zero for a clean stop.
		if isStopping && math.Sin(prevPhase) <= 0 && math.Sin(m.sineWavePhaseL) >= 0 {
			outSamples[offset] = 0
			for ch := range m.numOutputChannels {
				channelSamples[ch][offset] = 0
			}

			m.mu.RUnlock()
			m.calibrator.ConfirmStopped()

			for j := offset + 1; j < length; j++ {
				outSamples[j] = 0
				for ch := range m.numOutputChannels {
					channelSamples[ch][j] = 0
				}
			}

			for ch := range m.numOutputChannels {
				m.channels[m.outputChannelNames[ch]].Write(channelSamples[ch], 1.0, 0, true)
			}

			return true
		}
	}

	m.mu.RUnlock()

	for ch := range m.numOutputChannels {
		m.channels[OutputChannelName(ch)].Write(channelSamples[ch], 1.0, 0, true)
	}

	return true
}

// mixEngineChannelMulti mixes the engine channel into all output samples with lower priority.
func (m *StereoMixer) mixEngineChannelMulti(outSamples deviceBuffer, length int) {
	channel, ok := m.channels[ChannelEngine]
	if !ok {
		m.log.Error().Str("channel", ChannelEngine).Msg("channel not found in mixer")

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

	// Process engine for all output channels
	m.processEngineSamplesMulti(outSamples, engineSamples)
}

// processEngineSamplesMulti processes and mixes engine samples into all output channels.
func (m *StereoMixer) processEngineSamplesMulti(outSamples deviceBuffer, engineSamples []float64) {
	// Reusable callback-thread-only work buffers, grown on demand. outSamples is
	// the caller's per-channel scratch (numOutputChannels x length), so the
	// dimensions match. Clear before use: on a short engine read (underrun
	// truncation) the loop below fills only the leading indices, and the
	// copy-back overwrites all of outSamples — the cleared tail preserves the
	// original behaviour of zeroing those trailing samples.
	m.engineWorkScratch.grow(len(outSamples), len(outSamples[0]))
	outSamplesWork := m.engineWorkScratch

	for channel := range outSamplesWork {
		outSamplesWork[channel].zero()
	}

	for index, engineSample := range engineSamples {
		for channel := range outSamples {
			peak := 0.0
			engineScaled := engineSample
			engineMax := 1.0 - signal.Abs(outSamples[channel][index])

			if engineSample > engineMax || engineSample < -engineMax {
				engineScaled = engineMax * engineSample
			}

			outSamplesWork[channel][index] = mixSampleSum(outSamples[channel][index], engineScaled, &peak)
		}
	}

	for channel := range outSamples {
		copy(outSamples[channel], outSamplesWork[channel])
	}
}
