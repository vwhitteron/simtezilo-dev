package local

import (
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

// fakeBackend records how many times it is closed so the swap lifecycle can be
// asserted. OpenSink is unused by these tests (they exercise the backend-swap
// bookkeeping directly, not playback).
type fakeBackend struct {
	name   string
	closed int
}

func (f *fakeBackend) Name() string                         { return f.name }
func (f *fakeBackend) ListDevices() ([]audio.Device, error) { return nil, nil }
func (f *fakeBackend) OpenSink(audio.SinkConfig) (audio.Sink, error) {
	return nil, errors.New("not used")
}
func (f *fakeBackend) Close() error { f.closed++; return nil }

// TestOutput_BackendSwap verifies a queued backend swap only takes effect when
// applied (as the task goroutine does before each message), and that the
// replaced backend is closed exactly once.
func TestOutput_BackendSwap(t *testing.T) {
	t.Parallel()

	orig := &fakeBackend{name: "orig"}

	out, err := New(Config{Backend: orig, Logger: zerolog.Nop()})
	require.NoError(t, err)

	assert.Equal(t, "orig", out.currentBackend().Name())

	next := &fakeBackend{name: "next"}
	out.SetBackend(next)

	// Not applied until applyPendingBackend runs.
	assert.Equal(t, "orig", out.currentBackend().Name())
	assert.Equal(t, 0, orig.closed)

	out.applyPendingBackend()

	assert.Equal(t, "next", out.currentBackend().Name())
	assert.Equal(t, 1, orig.closed, "previous backend closed exactly once on swap")

	// Applying with nothing pending is a no-op.
	out.applyPendingBackend()
	assert.Equal(t, "next", out.currentBackend().Name())
	assert.Equal(t, 1, orig.closed)
}

// TestOutput_SetBackendSupersedesPending verifies that queuing a second swap
// before the first is applied closes the superseded backend immediately.
func TestOutput_SetBackendSupersedesPending(t *testing.T) {
	t.Parallel()

	orig := &fakeBackend{name: "orig"}

	out, err := New(Config{Backend: orig, Logger: zerolog.Nop()})
	require.NoError(t, err)

	first := &fakeBackend{name: "first"}
	second := &fakeBackend{name: "second"}

	out.SetBackend(first)
	out.SetBackend(second)

	assert.Equal(t, 1, first.closed, "superseded pending backend closed immediately")
	assert.Equal(t, 0, second.closed)
	assert.Equal(t, "orig", out.currentBackend().Name())

	out.applyPendingBackend()
	assert.Equal(t, "second", out.currentBackend().Name())
	assert.Equal(t, 1, orig.closed)
}

// TestOutput_CloseReleasesActiveAndPending verifies Close releases both the
// active backend and any pending (not-yet-applied) one.
func TestOutput_CloseReleasesActiveAndPending(t *testing.T) {
	t.Parallel()

	orig := &fakeBackend{name: "orig"}

	out, err := New(Config{Backend: orig, Logger: zerolog.Nop()})
	require.NoError(t, err)

	pending := &fakeBackend{name: "pending"}
	out.SetBackend(pending)

	require.NoError(t, out.Close())

	assert.Equal(t, 1, orig.closed, "active backend closed on Close")
	assert.Equal(t, 1, pending.closed, "pending backend closed on Close")
}
