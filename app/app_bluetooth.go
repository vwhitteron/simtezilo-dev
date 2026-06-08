package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

const bluetoothCmdTimeout = 15 * time.Second

// runBluetooth invokes a Bluetooth platform command with the optional stdin
// payload, reusing the setupMode helper gateway the app already uses for other
// platform actions.
func (a *App) runBluetooth(action platform.Command, stdin []byte) (*platform.CmdResponse, error) {
	if a.setupMode == nil || !a.setupMode.IsAvailable() {
		return nil, platform.ErrHelperUnavailable
	}

	ctx, cancel := context.WithTimeout(a.ctx, bluetoothCmdTimeout)
	defer cancel()

	return a.setupMode.PlatformAction(ctx, action, stdin)
}

// bluetoothAvailable reports whether Bluetooth management is available, gating
// the LCD Bluetooth branch purely on the presence of the platform helper — the
// same gate used by the web UI. When no adapter is present the device list is
// simply empty.
func (a *App) bluetoothAvailable() bool {
	return a.setupMode != nil && a.setupMode.IsAvailable()
}

// refreshBTDevices reloads the cached paired-device list and clamps the
// selection index. Best effort: on error the cache is left unchanged.
func (a *App) refreshBTDevices() {
	resp, err := a.runBluetooth(platform.BTList, nil)
	if err != nil || resp == nil {
		return
	}

	a.btDevices = resp.BTDevices

	if a.btSelectedIndex >= len(a.btDevices) {
		a.btSelectedIndex = 0
	}
}

// selectedBTDevice returns the currently selected paired device, or nil if the
// list is empty.
func (a *App) selectedBTDevice() *platform.CmdBTDevice {
	if a.btSelectedIndex < 0 || a.btSelectedIndex >= len(a.btDevices) {
		return nil
	}

	return &a.btDevices[a.btSelectedIndex]
}

// handleBluetoothDeviceSetting cycles the selected paired device. increase moves
// to the next device, decrease to the previous; get returns the current device
// label with its connection state.
func (a *App) handleBluetoothDeviceSetting(action string) string {
	switch action {
	case "increase":
		a.refreshBTDevices()

		if len(a.btDevices) > 0 {
			a.btSelectedIndex = (a.btSelectedIndex + 1) % len(a.btDevices)
		}
	case "decrease":
		a.refreshBTDevices()

		if len(a.btDevices) > 0 {
			a.btSelectedIndex = (a.btSelectedIndex - 1 + len(a.btDevices)) % len(a.btDevices)
		}
	default:
		a.refreshBTDevices()
	}

	return a.bluetoothDeviceLabel()
}

// bluetoothDeviceLabel formats the selected device for display on the LCD.
func (a *App) bluetoothDeviceLabel() string {
	device := a.selectedBTDevice()
	if device == nil {
		return "(none)"
	}

	state := "off"
	if device.Connected {
		state = "on"
	}

	return device.Name + " [" + state + "]"
}

// handleBluetoothToggleSetting connects or disconnects the currently selected
// paired device. Both increase and decrease toggle, matching other action
// leaves (e.g. record toggle).
func (a *App) handleBluetoothToggleSetting(action string) string {
	if action != "increase" && action != "decrease" {
		return a.bluetoothDeviceLabel()
	}

	device := a.selectedBTDevice()
	if device == nil {
		return "(none)"
	}

	cmd := platform.BTConnect
	if device.Connected {
		cmd = platform.BTDisconnect
	}

	payload, _ := json.Marshal(map[string]string{"address": device.Address}) //nolint:errchkjson // simple encoding

	_, err := a.runBluetooth(cmd, payload)
	if err != nil {
		a.log.Error().Err(err).Str("address", device.Address).Msg("bluetooth toggle failed")

		return "error"
	}

	// Reflect the new state.
	a.refreshBTDevices()

	return a.bluetoothDeviceLabel()
}
