package bluetooth //nolint:testpackage // white-box: exercises the unexported device picker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

// TestPickBTAudioDevice_TargetHonoured verifies that a specific selection (by
// address) routes to that exact paired audio device, even while disconnected and
// even when another audio device is connected.
func TestPickBTAudioDevice_TargetHonoured(t *testing.T) {
	t.Parallel()

	devices := []platform.CmdBTDevice{
		{Address: "11:11:11:11:11:11", Name: "Connected Speaker", Type: "speaker", Paired: true, Connected: true},
		{Address: "22:22:22:22:22:22", Name: "Chosen Speaker", Type: "speaker", Paired: true, Connected: false},
	}

	got := pickBTAudioDevice(devices, "22:22:22:22:22:22")

	if assert.NotNil(t, got) {
		assert.Equal(t, "22:22:22:22:22:22", got.Address)
	}
}

// TestPickBTAudioDevice_TargetNonAudioRejected verifies that a target that
// resolves to a non-audio peripheral (the fan/windsim) is never selected.
func TestPickBTAudioDevice_TargetNonAudioRejected(t *testing.T) {
	t.Parallel()

	devices := []platform.CmdBTDevice{
		{Address: "33:33:33:33:33:33", Name: "Fan Controller", Type: "fan", Paired: true, Connected: true},
	}

	assert.Nil(t, pickBTAudioDevice(devices, "33:33:33:33:33:33"))
}

// TestPickBTAudioDevice_FallbackPrefersConnected verifies that with no specific
// target the picker prefers a connected audio device over a merely-paired one and
// ignores non-audio devices.
func TestPickBTAudioDevice_FallbackPrefersConnected(t *testing.T) {
	t.Parallel()

	devices := []platform.CmdBTDevice{
		{Address: "33:33:33:33:33:33", Name: "Fan Controller", Type: "fan", Paired: true, Connected: true},
		{Address: "44:44:44:44:44:44", Name: "Paired Speaker", Type: "speaker", Paired: true, Connected: false},
		{Address: "55:55:55:55:55:55", Name: "Live Headset", Type: "headset", Paired: true, Connected: true},
	}

	got := pickBTAudioDevice(devices, "")

	if assert.NotNil(t, got) {
		assert.Equal(t, "55:55:55:55:55:55", got.Address)
	}
}

// TestPickBTAudioDevice_FallbackToPaired verifies that with nothing connected the
// picker falls back to the first paired audio device.
func TestPickBTAudioDevice_FallbackToPaired(t *testing.T) {
	t.Parallel()

	devices := []platform.CmdBTDevice{
		{Address: "44:44:44:44:44:44", Name: "Paired Speaker", Type: "speaker", Paired: true, Connected: false},
	}

	got := pickBTAudioDevice(devices, "")

	if assert.NotNil(t, got) {
		assert.Equal(t, "44:44:44:44:44:44", got.Address)
	}
}
