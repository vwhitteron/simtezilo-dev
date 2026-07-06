package webui //nolint:testpackage // white-box: exercises the unexported clear helper

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
)

// newBluetoothTestHandler builds a minimal systemHandler backed by a default
// config, enough to exercise the stale-selection clearing on device removal.
func newBluetoothTestHandler() *systemHandler {
	return &systemHandler{
		log:    zerolog.Nop(),
		config: appconfig.NewFromJSON([]byte("{}"), zerolog.Nop()),
	}
}

// TestClearRemovedBluetoothDevice_MatchingMACClears verifies that forgetting the
// device whose MAC is embedded in the saved pit-radio bluealsa selection drops
// that selection, so the app stops trying to open a device that no longer exists.
func TestClearRemovedBluetoothDevice_MatchingMACClears(t *testing.T) {
	t.Parallel()

	handler := newBluetoothTestHandler()
	handler.config.SetAudioPitRadioDevice("bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp")
	handler.config.SetAudioPitRadioDeviceName("bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp")

	// Address casing differs from the stored form to confirm the match is
	// case-insensitive.
	handler.clearRemovedBluetoothDevice("90:7a:58:d9:14:b3")

	assert.Empty(t, handler.config.GetAudioPitRadioDevice())
	assert.Empty(t, handler.config.GetAudioPitRadioDeviceName())
}

// TestClearRemovedBluetoothDevice_OtherDeviceUntouched verifies that forgetting a
// different device leaves an unrelated saved selection in place.
func TestClearRemovedBluetoothDevice_OtherDeviceUntouched(t *testing.T) {
	t.Parallel()

	handler := newBluetoothTestHandler()
	handler.config.SetAudioPitRadioDeviceName("bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp")

	handler.clearRemovedBluetoothDevice("AA:BB:CC:DD:EE:FF")

	assert.Equal(t, "bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp",
		handler.config.GetAudioPitRadioDeviceName())
}

// TestClearRemovedBluetoothDevice_NoMACSelectionUntouched verifies that a
// MAC-less selection (e.g. the snd-aloop "Bluetooth" bridge, which survives the
// unpair) is left alone.
func TestClearRemovedBluetoothDevice_NoMACSelectionUntouched(t *testing.T) {
	t.Parallel()

	handler := newBluetoothTestHandler()
	handler.config.SetAudioPitRadioDeviceName("Loopback: PCM (hw:2,0)")

	handler.clearRemovedBluetoothDevice("90:7A:58:D9:14:B3")

	assert.Equal(t, "Loopback: PCM (hw:2,0)", handler.config.GetAudioPitRadioDeviceName())
}
