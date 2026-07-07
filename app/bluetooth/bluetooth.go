// Package bluetooth manages the Bluetooth audio bridge and device selection for pit-radio output.
package bluetooth

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

const bluetoothCmdTimeout = 15 * time.Second

// bluetoothReconcileInterval is how often the reconciler re-checks that the audio
// bridge matches the desired state (pit-radio routed to the loopback + a Bluetooth
// audio device connected).
const bluetoothReconcileInterval = 30 * time.Second

// Gateway is the minimal interface required to issue Bluetooth platform commands.
// *setupmode.SetupMode satisfies this interface.
type Gateway interface {
	IsAvailable() bool
	PlatformAction(ctx context.Context, action platform.Command, stdin []byte) (*platform.CmdResponse, error)
}

// Manager owns all Bluetooth device state and audio bridge reconciliation.
type Manager struct {
	gateway Gateway
	log     zerolog.Logger
	wg      *sync.WaitGroup

	// ctx is the application lifecycle context, set once at construction (so it
	// is safe to read without synchronisation) and used by run, reconcile,
	// HandleToggleSetting, and OnPitRadioSinkActive.
	ctx context.Context

	devices       []platform.CmdBTDevice // cached paired-device list for the LCD selector
	selectedIndex int                    // index of the currently selected paired device

	// mu guards routedMAC and desiredMAC during bridge reconciliation.
	mu sync.Mutex
	// routedMAC is the address of the device the audio bridge is currently
	// routed to, "" when no bridge is up.
	routedMAC string
	// desiredMAC is the address of the paired Bluetooth speaker the user has
	// selected as the pit-radio output (resolved from the saved device name),
	// "" when the current selection is not a paired Bluetooth audio device. The
	// reconciler connects and routes to this specific device.
	desiredMAC string

	// loopbackActive is true while the pit-radio sink is open on the snd-aloop
	// Loopback device — the master the Bluetooth bridge slaves to. The bridge is
	// only run while this holds.
	loopbackActive atomic.Bool

	// reconcileOnce guards starting the Bluetooth audio bridge reconciler
	// exactly once for the Manager's lifetime.
	reconcileOnce sync.Once
}

// NewManager creates a Manager wired to the given gateway and logger. ctx is the
// application lifecycle context that bounds the reconciler goroutine and command
// timeouts. wg is the application's WaitGroup so shutdown waits for the reconciler
// goroutine.
func NewManager(gateway Gateway, log zerolog.Logger, wg *sync.WaitGroup, ctx context.Context) *Manager {
	return &Manager{
		gateway: gateway,
		log:     log,
		wg:      wg,
		ctx:     ctx,
	}
}

// run invokes a Bluetooth platform command with the optional stdin payload,
// reusing the gateway the app already uses for other platform actions.
func (m *Manager) run(action platform.Command, stdin []byte) (*platform.CmdResponse, error) {
	if m.gateway == nil || !m.gateway.IsAvailable() {
		return nil, platform.ErrHelperUnavailable
	}

	ctx, cancel := context.WithTimeout(m.ctx, bluetoothCmdTimeout)
	defer cancel()

	return m.gateway.PlatformAction(ctx, action, stdin)
}

// Available reports whether Bluetooth management is available, gating the LCD
// Bluetooth branch purely on the presence of the platform helper — the same gate
// used by the web UI. When no adapter is present the device list is simply empty.
func (m *Manager) Available() bool {
	return m.gateway != nil && m.gateway.IsAvailable()
}

// refreshDevices reloads the cached paired-device list and clamps the selection
// index. Best effort: on error the cache is left unchanged.
func (m *Manager) refreshDevices() {
	resp, err := m.run(platform.BTList, nil)
	if err != nil || resp == nil {
		return
	}

	m.devices = resp.BTDevices

	if m.selectedIndex >= len(m.devices) {
		m.selectedIndex = 0
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

// selectedDevice returns the currently selected paired device, or nil if the
// list is empty.
func (m *Manager) selectedDevice() *platform.CmdBTDevice {
	if m.selectedIndex < 0 || m.selectedIndex >= len(m.devices) {
		return nil
	}

	return &m.devices[m.selectedIndex]
}

// HandleDeviceSetting cycles the selected paired device. increase moves to the
// next device, decrease to the previous; get returns the current device label
// with its connection state.
func (m *Manager) HandleDeviceSetting(action string) string {
	switch action {
	case "increase":
		m.refreshDevices()

		if len(m.devices) > 0 {
			m.selectedIndex = (m.selectedIndex + 1) % len(m.devices)
		}
	case "decrease":
		m.refreshDevices()

		if len(m.devices) > 0 {
			m.selectedIndex = (m.selectedIndex - 1 + len(m.devices)) % len(m.devices)
		}
	default:
		m.refreshDevices()
	}

	return m.deviceLabel()
}

// deviceLabel formats the selected device for display on the LCD.
func (m *Manager) deviceLabel() string {
	device := m.selectedDevice()
	if device == nil {
		return "(none)"
	}

	state := "off"
	if device.Connected {
		state = "on"
	}

	return device.Name + " [" + state + "]"
}

// HandleToggleSetting connects or disconnects the currently selected paired
// device. Both increase and decrease toggle, matching other action leaves
// (e.g. record toggle).
func (m *Manager) HandleToggleSetting(action string) string {
	if action != "increase" && action != "decrease" {
		return m.deviceLabel()
	}

	device := m.selectedDevice()
	if device == nil {
		return "(none)"
	}

	cmd := platform.BTConnect
	if device.Connected {
		cmd = platform.BTDisconnect
	}

	payload, _ := json.Marshal(map[string]string{"address": device.Address}) //nolint:errchkjson // simple encoding

	_, err := m.run(cmd, payload)
	if err != nil {
		m.log.Error().Err(err).Str("address", device.Address).Msg("bluetooth toggle failed")

		return "error"
	}

	// The connection state changed; reconcile the audio bridge to match.
	go m.reconcile()

	// Reflect the new state.
	m.refreshDevices()

	return m.deviceLabel()
}

// OnPitRadioSinkActive is the local pit-radio Output's sink callback. The
// Bluetooth bridge can only run while the pit-radio sink (its loopback master) is
// open, so this records which paired speaker (if any) the current selection names
// and reconciles the bridge whenever it changes. deviceName is the saved selection
// name; for a Bluetooth output it is the paired device's friendly name (all
// Bluetooth selections open the same loopback master), which resolves here to the
// device's address so the reconciler can connect and route that specific one.
func (m *Manager) OnPitRadioSinkActive(deviceName string, active bool) {
	addr := ""
	if active {
		addr = m.addressForName(deviceName)
	}

	m.mu.Lock()
	m.desiredMAC = addr
	m.mu.Unlock()

	m.loopbackActive.Store(addr != "")

	go m.reconcile()
}

// addressForName returns the address of the paired audio device whose friendly
// name matches the selection, or "" when the name is not a paired Bluetooth audio
// device (e.g. a local sound card, or a forgotten speaker).
func (m *Manager) addressForName(name string) string {
	if name == "" {
		return ""
	}

	resp, err := m.run(platform.BTList, nil)
	if err != nil || resp == nil {
		return ""
	}

	for i := range resp.BTDevices {
		device := resp.BTDevices[i]
		if device.Paired && isAudioBTDevice(device) && device.Name == name {
			return device.Address
		}
	}

	return ""
}

// StartReconciler runs a background loop that periodically reconciles the
// Bluetooth audio path, catching state changes that happen outside an app
// action — e.g. the speaker being powered off and back on, or BlueZ dropping a
// trusted device. While the Bluetooth device is selected as the pit-radio output
// it (re)connects a dropped speaker and keeps the bridge up. Idempotent: multiple
// calls are safe, only the first starts the loop.
func (m *Manager) StartReconciler() {
	m.reconcileOnce.Do(func() {
		m.wg.Go(func() {
			ticker := time.NewTicker(bluetoothReconcileInterval)
			defer ticker.Stop()

			m.reconcile()

			for {
				select {
				case <-m.ctx.Done():
					return
				case <-ticker.C:
					m.reconcile()
				}
			}
		})
	})
}

// reconcile brings the Bluetooth audio path into the desired state: while the
// Bluetooth device is selected as the pit-radio output (its loopback sink — the
// bridge's master — is open) it ensures a paired speaker is connected and the
// snd-aloop→bluealsa bridge is up; otherwise it tears the bridge down. The
// connect is gated on selection so auto-reconnect only happens while the user has
// the Bluetooth device configured, and a deliberate disconnect of a non-selected
// device is left alone. Idempotent and best-effort.
func (m *Manager) reconcile() {
	m.mu.Lock()
	defer m.mu.Unlock()

	want := ""

	if m.loopbackActive.Load() {
		want = m.ensureConnected(m.desiredMAC)
	}

	// Tear down a stale bridge (sink closed, device changed, or disconnected).
	if m.routedMAC != "" && m.routedMAC != want {
		m.routeAudio(m.routedMAC, false)
		m.routedMAC = ""
	}

	if want != "" {
		m.routeAudio(want, true)
		m.routedMAC = want
	}
}

// ensureConnected returns the address of the connected Bluetooth speaker for the
// pit-radio output, connecting it if necessary. target is the address of the
// specifically-selected speaker; when set, only that device is considered so
// selecting a particular speaker routes to it. When target is empty it falls back
// to any connected (else the first paired) audio device. BlueZ trusts devices at
// pair time but does not proactively reconnect a trusted speaker (the speaker has
// to initiate), so this drives the (re)connect itself. Called only while a
// Bluetooth device is the selected pit-radio output, so a dropped speaker is
// reconnected on the periodic tick. Best-effort: connect failures (device
// off/out of range) are logged and the next tick retries.
func (m *Manager) ensureConnected(target string) string {
	resp, err := m.run(platform.BTList, nil)
	if err != nil || resp == nil {
		return ""
	}

	device := pickBTAudioDevice(resp.BTDevices, target)
	if device == nil {
		return ""
	}

	if device.Connected {
		return device.Address
	}

	// Not connected: (re)connect it. A successful Connect is synchronous, so the
	// returned address is now connected.
	payload, _ := json.Marshal(map[string]string{"address": device.Address}) //nolint:errchkjson // simple encoding

	_, err = m.run(platform.BTConnect, payload)
	if err != nil {
		m.log.Debug().Err(err).Str("address", device.Address).Msg("bluetooth auto-reconnect failed")

		return ""
	}

	m.log.Info().Str("address", device.Address).Str("name", device.Name).Msg("bluetooth auto-reconnected")

	return device.Address
}

// pickBTAudioDevice selects the paired audio device to route pit-radio to. When
// target (an address) is set it returns that specific paired audio device, so a
// deliberate speaker choice is honoured even while it is disconnected. Otherwise
// it prefers a connected audio device, then the first paired one. Non-audio
// devices (e.g. the fan/windsim) are never eligible. Returns nil when no suitable
// device exists.
func pickBTAudioDevice(devices []platform.CmdBTDevice, target string) *platform.CmdBTDevice {
	if target != "" {
		return firstBTDevice(devices, func(d platform.CmdBTDevice) bool {
			return d.Address == target && d.Paired && isAudioBTDevice(d)
		})
	}

	if d := firstBTDevice(devices, func(d platform.CmdBTDevice) bool {
		return d.Connected && isAudioBTDevice(d)
	}); d != nil {
		return d
	}

	return firstBTDevice(devices, func(d platform.CmdBTDevice) bool {
		return d.Paired && isAudioBTDevice(d)
	})
}

// firstBTDevice returns a pointer to the first device satisfying match, or nil.
func firstBTDevice(devices []platform.CmdBTDevice, match func(platform.CmdBTDevice) bool) *platform.CmdBTDevice {
	for i := range devices {
		if match(devices[i]) {
			return &devices[i]
		}
	}

	return nil
}

// routeAudio brings the snd-aloop→bluealsa audio bridge up or down for a device.
// Best-effort: failures are logged, never propagated.
func (m *Manager) routeAudio(address string, enable bool) {
	payload, _ := json.Marshal(map[string]any{ //nolint:errchkjson // simple encoding
		"address": address,
		"enable":  enable,
	})

	_, err := m.run(platform.BTAudioRoute, payload)
	if err != nil {
		m.log.Warn().Err(err).Str("address", address).Bool("enable", enable).Msg("bluetooth audio route failed")
	}
}
