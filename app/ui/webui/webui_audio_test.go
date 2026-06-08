package webui //nolint:testpackage // white-box: exercises unexported apply handlers and callback fields

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
)

// newAudioTestWebUI builds a minimal WebUI backed by a default config, enough to
// exercise the audio config-apply handlers and their live-reconfiguration hooks.
func newAudioTestWebUI() *WebUI {
	return &WebUI{
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

			webUI := newAudioTestWebUI()

			called := false
			webUI.onHapticsOutputChanged = func() { called = true }

			errs := webUI.applyHapticsOutputConfig(testCase.patch)

			assert.Empty(t, errs)
			assert.Equal(t, testCase.wantRestart, called)
		})
	}
}

// TestApplyHapticsOutputConfig_NoChangeNoRestart ensures re-saving the current
// values (e.g. an unrelated settings save) does not glitch playback by restarting.
func TestApplyHapticsOutputConfig_NoChangeNoRestart(t *testing.T) {
	t.Parallel()

	webUI := newAudioTestWebUI()

	called := false
	webUI.onHapticsOutputChanged = func() { called = true }

	errs := webUI.applyHapticsOutputConfig(map[string]any{
		"device":     webUI.config.GetAudioHapticsDevice(),
		"channels":   float64(webUI.config.GetAudioHapticsChannels()),
		"sampleRate": float64(webUI.config.GetAudioHapticsSampleRate()),
		"latencyMs":  float64(webUI.config.GetAudioHapticsLatencyMs()),
	})

	assert.Empty(t, errs)
	assert.False(t, called, "saving unchanged output values must not trigger a restart")
}

// TestApplyHardwareConfig_BackendChangeRequiresRestart verifies that changing the
// audio backend persists the new value and marks the config restart-required
// (backend changes are applied on restart, not live).
func TestApplyHardwareConfig_BackendChangeRequiresRestart(t *testing.T) {
	t.Parallel()

	webUI := newAudioTestWebUI()
	assert.False(t, webUI.config.IsRestartRequired(), "fresh config should not require a restart")

	errs := webUI.applyHardwareConfig(map[string]any{"audioBackend": "portaudio"})

	assert.Empty(t, errs)
	assert.Equal(t, "portaudio", webUI.config.GetAudioBackend())
	assert.True(t, webUI.config.IsRestartRequired(),
		"changing the audio backend should require a restart")
}

// TestApplyAudioConfig_PersistsDeviceName guards the device + deviceName wiring
// (the field whose absence caused the empty-deviceName bug) for both outputs.
func TestApplyAudioConfig_PersistsDeviceName(t *testing.T) {
	t.Parallel()

	webUI := newAudioTestWebUI()

	assert.Empty(t, webUI.applyHapticsOutputConfig(map[string]any{
		"device": "5", "deviceName": "Speakers",
	}))
	assert.Equal(t, "5", webUI.config.GetAudioHapticsDevice())
	assert.Equal(t, "Speakers", webUI.config.GetAudioHapticsDeviceName())

	assert.Empty(t, webUI.applyPitRadioAudioConfig(map[string]any{
		"device": "2", "deviceName": "Headphones",
	}))
	assert.Equal(t, "2", webUI.config.GetAudioPitRadioDevice())
	assert.Equal(t, "Headphones", webUI.config.GetAudioPitRadioDeviceName())
}
