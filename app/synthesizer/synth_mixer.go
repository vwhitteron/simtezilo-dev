package synthesizer

import (
	"time"
)

// Mixer defines the methods required for mixer operations.
type Mixer interface { //nolint:interfacebloat // Large interface for core mixer functionality
	// Core mixing operations
	MixToMaster(length int)
	ReadChannel(name string, length int) []float64

	// OutputChannelName returns the precomputed channel name for output channel ch.
	OutputChannelName(ch int) string

	// Channel management
	AddChannel(name string, gain float64) error
	GetChannelGain(name string) (float64, error)
	SetChannelGain(name string, gain float64) error
	GetChannelPowerRatio(name string) (float64, error)
	WriteChannel(name string, samples []float64, magnitude float64, offset int, accumulate bool) error
	ChannelDepth(name string) int
	ClearChannelBuffer(name string)
	ClearBuffers()
	InspectChannelBuffer(name string, length int, offset int) []float64
	GetBufferCapacity() int

	// Fader control
	FadeIn(period time.Duration)
	SetFader(gain float64)

	// Calibration
	ResetSineWavePhase()

	// Lifecycle
	Close()

	// Config access for mute state
	GetChannelMute(channel int) bool
	GetMasterMute() bool
	SetSilenced(silenced bool)
	IsSilenced() bool

	// Diagnostics returns buffer health diagnostics for all channels
	Diagnostics() MixerDiagnostics
}
