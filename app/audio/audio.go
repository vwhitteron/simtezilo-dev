// Package audio provides a backend-agnostic abstraction over audio output
// devices. Concrete backends (beep, portaudio) implement the Backend
// interface, decoupling the synthesizer and pit-radio consumers from any
// particular audio library or platform.
//
// All sample data crossing the abstraction boundary is interleaved float32
// (portaudio's native format): for an N-channel frame the layout is
// [c0, c1, ... cN-1, c0, c1, ...]. The beep backend adapts this to beep's
// [][2]float64 representation internally.
package audio

import "errors"

// Backend names recognised by New.
const (
	BackendBeep      = "beep"
	BackendPortAudio = "portaudio"
)

// ErrBackendUnavailable is returned when a backend is selected by name but the
// binary was not built with support for it (build tag missing).
var ErrBackendUnavailable = errors.New("audio backend not available in this build")

// Device describes an output device exposed by a backend.
//
// Name is the stable, backend-agnostic selection key: it is what
// ResolveOutputDevice matches on and what the config persists as deviceName, so
// it must stay consistent between enumeration and open. DisplayName is the
// friendly label shown in the UI and may be enriched (e.g. with a Bluetooth
// alias) without affecting selection.
type Device struct {
	ID                string // backend-specific identifier ("" selects the default)
	Name              string // stable selection key (persisted, matched on resolve)
	DisplayName       string // friendly label for presentation only; falls back to Name when empty
	Type              DeviceType
	Backend           string // backend that owns this device
	MaxChannels       int    // maximum output channels supported
	DefaultSampleRate int    // device's preferred sample rate in Hz
	IsDefault         bool   // true if this is the system default output
}

// DeviceType is a semantic, presentation-agnostic classification used to group
// and icon devices in the UI.
type DeviceType string

// Device type classifications.
const (
	DeviceBuiltin   DeviceType = "builtin"
	DeviceUSB       DeviceType = "usb"
	DeviceHDMI      DeviceType = "hdmi"
	DeviceBluetooth DeviceType = "bluetooth"
)

// SinkConfig describes a requested output stream. A zero DeviceID selects the
// backend default device.
type SinkConfig struct {
	DeviceID   string
	Channels   int
	SampleRate int
	LatencyMs  int
}

// SampleSource produces interleaved float32 audio frames on demand. ReadInterleaved
// must fill buf with frames*channels samples where frames == len(buf)/channels,
// returning the number of frames written. Returning ok=false signals end of
// stream; sources that stream indefinitely should always return true and fill
// silence when idle.
type SampleSource interface {
	ReadInterleaved(buf []float32, channels int) (frames int, ok bool)
}

// Sink is a started or startable output stream bound to a device.
type Sink interface {
	// Start begins pulling from src and writing to the device.
	Start(src SampleSource) error
	// Stop halts playback and releases the stream.
	Stop() error
	// Channels reports the channel count the sink was opened with.
	Channels() int
}

// Backend is a concrete audio implementation (beep, portaudio).
type Backend interface {
	// Name returns the backend identifier (one of the Backend* constants).
	Name() string
	// ListDevices enumerates available output devices.
	ListDevices() ([]Device, error)
	// OpenSink opens an output stream matching cfg. The returned Sink is not
	// started until Start is called.
	OpenSink(cfg SinkConfig) (Sink, error)
	// Close releases any backend-wide resources.
	Close() error
}
