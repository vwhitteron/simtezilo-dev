package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

const bluetoothCmdTimeout = 15 * time.Second

// bluetoothReconcileInterval is how often the reconciler re-checks that the audio
// bridge matches the desired state (pit-radio routed to the loopback + a Bluetooth
// audio device connected).
const bluetoothReconcileInterval = 30 * time.Second

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

// isAudioBTDevice reports whether a Bluetooth device is an audio sink/source.
// The helper classifies devices by a semantic type; only audio devices are
// eligible for the pit-radio audio bridge (the fan/windsim is type "fan").
func isAudioBTDevice(device platform.CmdBTDevice) bool {
	switch device.Type {
	case "speaker", "headphones", "headset", "audio":
		return true
	default:
		return false
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

	// The connection state changed; reconcile the audio bridge to match.
	go a.reconcileBluetoothAudio()

	// Reflect the new state.
	a.refreshBTDevices()

	return a.bluetoothDeviceLabel()
}

// onPitRadioSinkActive is the local pit-radio Output's sink callback. The
// Bluetooth bridge can only run while the pit-radio sink (its loopback master) is
// open, so this tracks whether that sink is on the Loopback device and reconciles
// the bridge whenever it changes.
func (a *App) onPitRadioSinkActive(deviceName string, active bool) {
	a.pitRadioLoopbackActive.Store(active && strings.Contains(strings.ToLower(deviceName), "loopback"))

	go a.reconcileBluetoothAudio()
}

// startBluetoothAudioReconciler runs a background loop that periodically
// reconciles the Bluetooth audio path, catching state changes that happen outside
// an app action — e.g. the speaker being powered off and back on, or BlueZ
// dropping a trusted device. While the Bluetooth device is selected as the
// pit-radio output it (re)connects a dropped speaker and keeps the bridge up.
func (a *App) startBluetoothAudioReconciler() {
	a.wg.Go(func() {
		ticker := time.NewTicker(bluetoothReconcileInterval)
		defer ticker.Stop()

		a.reconcileBluetoothAudio()

		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.reconcileBluetoothAudio()
			}
		}
	})
}

// reconcileBluetoothAudio brings the Bluetooth audio path into the desired state:
// while the Bluetooth device is selected as the pit-radio output (its loopback
// sink — the bridge's master — is open) it ensures a paired speaker is connected
// and the snd-aloop→bluealsa bridge is up; otherwise it tears the bridge down.
// The connect is gated on selection so auto-reconnect only happens while the user
// has the Bluetooth device configured, and a deliberate disconnect of a
// non-selected device is left alone. Idempotent and best-effort.
func (a *App) reconcileBluetoothAudio() {
	a.btMu.Lock()
	defer a.btMu.Unlock()

	want := ""

	if a.pitRadioLoopbackActive.Load() {
		want = a.ensureBluetoothAudioConnected()
	}

	// Tear down a stale bridge (sink closed, device changed, or disconnected).
	if a.routedBTMAC != "" && a.routedBTMAC != want {
		a.routeBluetoothAudio(a.routedBTMAC, false)
		a.routedBTMAC = ""
	}

	if want != "" {
		a.routeBluetoothAudio(want, true)
		a.routedBTMAC = want
	}
}

// ensureBluetoothAudioConnected returns the address of a connected Bluetooth
// audio device, connecting a paired one if none is currently connected. BlueZ
// trusts devices at pair time but does not proactively reconnect a trusted
// speaker (the speaker has to initiate), so this drives the (re)connect itself.
// Called only while the Bluetooth device is the selected pit-radio output, so a
// dropped speaker is reconnected on the periodic tick. Best-effort: connect
// failures (device off/out of range) are logged and the next tick retries.
func (a *App) ensureBluetoothAudioConnected() string {
	resp, err := a.runBluetooth(platform.BTList, nil)
	if err != nil || resp == nil {
		return ""
	}

	for _, device := range resp.BTDevices {
		if device.Connected && isAudioBTDevice(device) {
			return device.Address
		}
	}

	// None connected: (re)connect the first paired audio device. A successful
	// Connect is synchronous, so the returned address is now connected. Skip
	// non-audio devices (e.g. the fan/windsim) so we never route them as a sink.
	for _, device := range resp.BTDevices {
		if !device.Paired || !isAudioBTDevice(device) {
			continue
		}

		payload, _ := json.Marshal(map[string]string{"address": device.Address}) //nolint:errchkjson // simple encoding

		_, err := a.runBluetooth(platform.BTConnect, payload)
		if err != nil {
			a.log.Debug().Err(err).Str("address", device.Address).Msg("bluetooth auto-reconnect failed")

			continue
		}

		a.log.Info().Str("address", device.Address).Str("name", device.Name).Msg("bluetooth auto-reconnected")

		return device.Address
	}

	return ""
}

// routeBluetoothAudio brings the snd-aloop→bluealsa audio bridge up or down for a
// device. Best-effort: failures are logged, never propagated.
func (a *App) routeBluetoothAudio(address string, enable bool) {
	payload, _ := json.Marshal(map[string]any{ //nolint:errchkjson // simple encoding
		"address": address,
		"enable":  enable,
	})

	_, err := a.runBluetooth(platform.BTAudioRoute, payload)
	if err != nil {
		a.log.Warn().Err(err).Str("address", address).Bool("enable", enable).Msg("bluetooth audio route failed")
	}
}
