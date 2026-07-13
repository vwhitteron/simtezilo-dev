package webui //nolint:testpackage // white-box: exercises unexported apply handlers and callback fields

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/vwhitteron/simtezilo-dev/app/audio"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
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

// TestEnrichBluetoothNames_IgnoresNonAudioDevice verifies that a connected
// non-audio peripheral (the fan/windsim) never labels the snd-aloop Bluetooth
// output. Previously the fan controller, being the first connected device,
// relabeled the Loopback bridge and appeared as the selected pit-radio device
// even when no headset was connected.
func TestEnrichBluetoothNames_IgnoresNonAudioDevice(t *testing.T) {
	t.Parallel()

	handler := newAudioTestHandler()
	handler.btDevices = func(context.Context) []platform.CmdBTDevice {
		return []platform.CmdBTDevice{
			{Address: "AA:BB:CC:DD:EE:FF", Name: "Fan Controller", Type: "fan", Connected: true},
		}
	}

	// The snd-aloop bridge carries no MAC in its name and is typed Bluetooth.
	devices := []audio.Device{
		{Name: "Loopback (hw:2,0)", DisplayName: "Bluetooth", Type: audio.DeviceBluetooth},
	}

	handler.enrichBluetoothNames(context.Background(), devices)

	assert.Equal(t, "Bluetooth", devices[0].DisplayName,
		"a connected fan controller must not label the Bluetooth output")
}

// TestEnrichBluetoothNames_UsesConnectedAudioDevice verifies that a connected
// audio device (a headset) still labels the snd-aloop Bluetooth bridge.
func TestEnrichBluetoothNames_UsesConnectedAudioDevice(t *testing.T) {
	t.Parallel()

	handler := newAudioTestHandler()
	handler.btDevices = func(context.Context) []platform.CmdBTDevice {
		return []platform.CmdBTDevice{
			{Address: "AA:BB:CC:DD:EE:FF", Name: "Fan Controller", Type: "fan", Connected: true},
			{Address: "11:22:33:44:55:66", Name: "My Headset", Type: "headset", Connected: true},
		}
	}

	devices := []audio.Device{
		{Name: "Loopback (hw:2,0)", DisplayName: "Bluetooth", Type: audio.DeviceBluetooth},
	}

	handler.enrichBluetoothNames(context.Background(), devices)

	assert.Equal(t, "My Headset", devices[0].DisplayName,
		"a connected headset should label the Bluetooth output")
}

// TestInjectPairedBluetoothDevices_OneEntryPerPairedDevice verifies that the
// single snd-aloop Bluetooth bridge entry is expanded into one entry per paired
// audio device, labelled by name, each carrying the bridge (Loopback) device ID,
// so every paired speaker is selectable even while disconnected.
func TestInjectPairedBluetoothDevices_OneEntryPerPairedDevice(t *testing.T) {
	t.Parallel()

	handler := newAudioTestHandler()
	handler.btDevices = func(context.Context) []platform.CmdBTDevice {
		return []platform.CmdBTDevice{
			{Address: "33:33:33:33:33:33", Name: "Fan Controller", Type: "fan", Paired: true},
			{Address: "11:11:11:11:11:11", Name: "Garage Speaker", Type: "speaker", Paired: true, Connected: false},
			{Address: "22:22:22:22:22:22", Name: "My Headset", Type: "headset", Paired: true, Connected: true},
		}
	}

	devices := []audio.Device{
		{ID: "3", Name: "USB Audio (hw:1,0)", DisplayName: "USB Audio", Type: audio.DeviceUSB},
		{ID: "7", Name: "Loopback (hw:2,0)", DisplayName: "Bluetooth", Type: audio.DeviceBluetooth},
	}

	got := handler.injectPairedBluetoothDevices(context.Background(), devices)

	// The USB device is untouched; the single Bluetooth bridge entry is replaced
	// by one entry per paired audio device (the fan is excluded).
	names := map[string]audio.Device{}
	for _, d := range got {
		names[d.DisplayName] = d
	}

	assert.Contains(t, names, "USB Audio")
	assert.Contains(t, names, "Garage Speaker")
	assert.Contains(t, names, "My Headset")
	assert.NotContains(t, names, "Bluetooth", "the generic bridge entry is replaced")
	assert.NotContains(t, names, "Fan Controller", "non-audio devices are never offered")

	// Each Bluetooth entry is keyed by name and opens the bridge (Loopback) ID.
	assert.Equal(t, "7", names["Garage Speaker"].ID)
	assert.Equal(t, "Garage Speaker", names["Garage Speaker"].Name)
	assert.Equal(t, audio.DeviceBluetooth, names["Garage Speaker"].Type)
	assert.Equal(t, "7", names["My Headset"].ID)
}

// TestInjectPairedBluetoothDevices_NoPairedDevicesNoOp verifies the device list is
// returned untouched when nothing is paired (e.g. a host with a native BT output).
func TestInjectPairedBluetoothDevices_NoPairedDevicesNoOp(t *testing.T) {
	t.Parallel()

	handler := newAudioTestHandler()
	handler.btDevices = func(context.Context) []platform.CmdBTDevice {
		return nil
	}

	devices := []audio.Device{
		{ID: "7", Name: "Loopback (hw:2,0)", DisplayName: "Bluetooth", Type: audio.DeviceBluetooth},
	}

	got := handler.injectPairedBluetoothDevices(context.Background(), devices)

	assert.Equal(t, devices, got)
}
