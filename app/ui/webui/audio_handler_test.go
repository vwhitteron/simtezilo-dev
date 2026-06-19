package webui //nolint:testpackage // white-box: exercises unexported apply handlers and callback fields

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
)

// newAudioTestHandler builds a minimal configHandler backed by a default config,
// enough to exercise the audio config-apply handlers and their
// live-reconfiguration hooks.
func newAudioTestHandler() *configHandler {
	return &configHandler{
		log:    zerolog.Nop(),
		config: appconfig.NewFromJSON([]byte("{}"), zerolog.Nop()),
	}
}

// TestApplyHapticsOutputConfig_RestartTriggers verifies the live-restart hook
// fires for genuine output changes but NOT for a deviceName-only change (which is
// metadata saved alongside a device change that already triggers the restart —
// the double-restart guard).
func TestApplyHapticsOutputConfig_RestartTriggers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		patch       map[string]any
		wantRestart bool
	}{
		{"device change", map[string]any{"device": "newdev"}, true},
		{"channels change", map[string]any{"channels": float64(4)}, true},
		{"sampleRate change", map[string]any{"sampleRate": float64(48000)}, true},
		{"latency change", map[string]any{"latencyMs": float64(40)}, true},
		{"deviceName only", map[string]any{"deviceName": "Speakers"}, false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			handler := newAudioTestHandler()

			called := false
			handler.onHapticsOutputChanged = func() { called = true }

			errs := handler.applyHapticsOutputConfig(testCase.patch)

			assert.Empty(t, errs)
			assert.Equal(t, testCase.wantRestart, called)
		})
	}
}

// TestApplyHapticsOutputConfig_NoChangeNoRestart ensures re-saving the current
// values (e.g. an unrelated settings save) does not glitch playback by restarting.
func TestApplyHapticsOutputConfig_NoChangeNoRestart(t *testing.T) {
	t.Parallel()

	handler := newAudioTestHandler()

	called := false
	handler.onHapticsOutputChanged = func() { called = true }

	errs := handler.applyHapticsOutputConfig(map[string]any{
		"device":     handler.config.GetAudioHapticsDevice(),
		"channels":   float64(handler.config.GetAudioHapticsChannels()),
		"sampleRate": float64(handler.config.GetAudioHapticsSampleRate()),
		"latencyMs":  float64(handler.config.GetAudioHapticsLatencyMs()),
	})

	assert.Empty(t, errs)
	assert.False(t, called, "saving unchanged output values must not trigger a restart")
}

// TestApplyHardwareConfig_BackendChangeRequiresRestart verifies that changing the
// audio backend persists the new value and marks the config restart-required
// (backend changes are applied on restart, not live).
func TestApplyHardwareConfig_BackendChangeRequiresRestart(t *testing.T) {
	t.Parallel()

	handler := newAudioTestHandler()
	assert.False(t, handler.config.IsRestartRequired(), "fresh config should not require a restart")

	errs := handler.applyHardwareConfig(map[string]any{"audioBackend": "portaudio"})

	assert.Empty(t, errs)
	assert.Equal(t, "portaudio", handler.config.GetAudioBackend())
	assert.True(t, handler.config.IsRestartRequired(),
		"changing the audio backend should require a restart")
}

// TestApplyAudioConfig_PersistsDeviceName guards the device + deviceName wiring
// (the field whose absence caused the empty-deviceName bug) for both outputs.
func TestApplyAudioConfig_PersistsDeviceName(t *testing.T) {
	t.Parallel()

	handler := newAudioTestHandler()

	assert.Empty(t, handler.applyHapticsOutputConfig(map[string]any{
		"device": "5", "deviceName": "Speakers",
	}))
	assert.Equal(t, "5", handler.config.GetAudioHapticsDevice())
	assert.Equal(t, "Speakers", handler.config.GetAudioHapticsDeviceName())

	assert.Empty(t, handler.applyPitRadioAudioConfig(map[string]any{
		"device": "2", "deviceName": "Headphones",
	}))
	assert.Equal(t, "2", handler.config.GetAudioPitRadioDevice())
	assert.Equal(t, "Headphones", handler.config.GetAudioPitRadioDeviceName())
}
