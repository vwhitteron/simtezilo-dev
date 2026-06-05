package audio

import (
	"fmt"
	"sort"

	"github.com/rs/zerolog"
)

// backendFactory constructs a Backend. Backends register a factory under their
// name via registerBackend in an init function; tag-gated backends (malgo,
// portaudio) only register when compiled in.
type backendFactory func(zerolog.Logger) (Backend, error)

var backendRegistry = map[string]backendFactory{}

func registerBackend(name string, f backendFactory) {
	backendRegistry[name] = f
}

// New constructs the named backend. An empty name selects the beep backend.
// If a known backend name (malgo, portaudio) is requested but was not compiled
// into the binary, ErrBackendUnavailable is returned so callers can fall back.
func New(name string, log zerolog.Logger) (Backend, error) {
	if name == "" {
		name = BackendBeep
	}

	factory, ok := backendRegistry[name]
	if !ok {
		if name == BackendMalgo || name == BackendPortAudio {
			return nil, fmt.Errorf("%q: %w", name, ErrBackendUnavailable)
		}

		return nil, fmt.Errorf("unknown audio backend %q", name)
	}

	return factory(log)
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
