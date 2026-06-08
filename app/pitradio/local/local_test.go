package local //nolint:testpackage // white-box: exercises the unexported currentBackend method

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

// fakeBackend records how many times it is closed so the backend lifecycle can
// be asserted. OpenSink is unused by these tests (they do not exercise
// playback).
type fakeBackend struct {
	name   string
	closed int
}

func (f *fakeBackend) Name() string                         { return f.name }
func (f *fakeBackend) ListDevices() ([]audio.Device, error) { return nil, nil }
func (f *fakeBackend) OpenSink(audio.SinkConfig) (audio.Sink, error) { //nolint:ireturn // satisfies audio.Backend interface
	return nil, errors.New("not used")
}

func (f *fakeBackend) Close() error {
	f.closed++

	return nil
}

// TestOutput_UsesConstructionBackend verifies the backend supplied at
// construction is the one used, and that it is not swapped over the Output's
// lifetime (backend changes are restart-required).
func TestOutput_UsesConstructionBackend(t *testing.T) {
	t.Parallel()

	orig := &fakeBackend{name: "orig"}

	out, err := New(Config{Backend: orig, Logger: zerolog.Nop()})
	require.NoError(t, err)

	assert.Equal(t, "orig", out.currentBackend().Name())
}

// TestOutput_CloseReleasesBackend verifies Close releases the active backend
// exactly once.
func TestOutput_CloseReleasesBackend(t *testing.T) {
	t.Parallel()

	orig := &fakeBackend{name: "orig"}

	out, err := New(Config{Backend: orig, Logger: zerolog.Nop()})
	require.NoError(t, err)

	require.NoError(t, out.Close())

	assert.Equal(t, 1, orig.closed, "active backend closed on Close")
}
