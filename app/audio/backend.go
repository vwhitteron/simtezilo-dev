package audio

import (
	"fmt"
	"sort"

	"github.com/rs/zerolog"
)

// backendFactory constructs a Backend. Backends register a factory under their
// name via registerBackend in an init function; the tag-gated portaudio backend
// only registers when compiled in.
type backendFactory func(zerolog.Logger) (Backend, error)

var backendRegistry = map[string]backendFactory{} //nolint:gochecknoglobals // registry pattern; populated by init() in backend files

func registerBackend(name string, f backendFactory) {
	backendRegistry[name] = f
}

// New constructs the named backend. An empty name selects the beep backend.
// If the portaudio backend is requested but was not compiled into the binary,
// ErrBackendUnavailable is returned so callers can fall back.
func New(name string, log zerolog.Logger) (Backend, error) {
	if name == "" {
		name = BackendBeep
	}

	factory, ok := backendRegistry[name]
	if !ok {
		if name == BackendPortAudio {
			return nil, fmt.Errorf("%q: %w", name, ErrBackendUnavailable)
		}

		return nil, fmt.Errorf("unknown audio backend %q", name)
	}

	return factory(log)
}

// ResolveOutputDevice picks the native device ID to open for a saved selection,
// keying off the human-readable device Name — the only identifier common to all
// backends and stable across both a backend switch and portaudio's positional
// index reshuffling — with the native ID as a tiebreaker for duplicate names.
// It returns "" to mean "use the backend default device".
//
// Resolution order:
//   - name matches exactly one device      -> that device's ID
//   - name matches several devices          -> the one whose ID == savedID, else the first
//   - name matches none, savedID still valid -> savedID
//   - otherwise                              -> "" (default device)
//
// If the device list cannot be read it falls back to savedID (best effort).
func ResolveOutputDevice(b Backend, name, savedID string) string {
	if name == "" && savedID == "" {
		return ""
	}

	devices, err := b.ListDevices()
	if err != nil {
		return savedID
	}

	var named []Device

	if name != "" {
		for _, d := range devices {
			if d.Name == name {
				named = append(named, d)
			}
		}
	}

	switch len(named) {
	case 1:
		return named[0].ID
	case 0:
		// Name gone (or unset): honour the stored ID only if it still exists.
		for _, d := range devices {
			if savedID != "" && d.ID == savedID {
				return savedID
			}
		}

		return ""
	default:
		// Duplicate names: disambiguate with the stored native ID when possible.
		for _, d := range named {
			if d.ID == savedID {
				return savedID
			}
		}

		return named[0].ID
	}
}

// FindOutputDevice resolves the same selection as ResolveOutputDevice but
// returns the matching Device so callers can inspect its DefaultSampleRate.
// Returns the zero Device and false when no device matches or the device list
// cannot be read.
func FindOutputDevice(b Backend, name, savedID string) (Device, bool) {
	devices, err := b.ListDevices()
	if err != nil {
		return Device{}, false
	}

	var named []Device

	if name != "" {
		for _, d := range devices {
			if d.Name == name {
				named = append(named, d)
			}
		}
	}

	switch len(named) {
	case 1:
		return named[0], true
	case 0:
		for _, d := range devices {
			if savedID != "" && d.ID == savedID {
				return d, true
			}
		}

		return Device{}, false
	default:
		for _, d := range named {
			if d.ID == savedID {
				return d, true
			}
		}

		return named[0], true
	}
}

// AvailableBackends returns the sorted names of backends compiled into this
// binary.
func AvailableBackends() []string {
	names := make([]string, 0, len(backendRegistry))
	for name := range backendRegistry {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}
