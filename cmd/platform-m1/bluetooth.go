package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

const (
	btService          = "org.bluez"
	btAdapterIface     = "org.bluez.Adapter1"
	btDeviceIface      = "org.bluez.Device1"
	btAgentManagerPath = "/org/bluez"
	btAgentMgrIface    = "org.bluez.AgentManager1"
	btAgentIface       = "org.bluez.Agent1"
	btAgentPath        = dbus.ObjectPath("/simtezilo/btagent")
	dbusPropsIface     = "org.freedesktop.DBus.Properties"
	dbusObjMgrIface    = "org.freedesktop.DBus.ObjectManager"

	// nusServiceUUID is the Nordic UART Service advertised by the wind
	// simulator fan firmware. Devices advertising it are classified as "fan".
	nusServiceUUID = "6e400001-b5a3-f393-e0a9-e50e24dcca9e"

	// btScanDuration is how long bt-scan keeps discovery running before
	// enumerating results.
	btScanDuration = 8 * time.Second
	// btAgentCapability requests no input/output, so simple devices pair
	// without prompting for a PIN or passkey confirmation.
	btAgentCapability = "NoInputNoOutput"

	// btPairWait bounds how long pairDevice waits for an asynchronous or
	// in-progress pairing (one BlueZ signals via org.bluez.Error.InProgress) to
	// finish before giving up. It must stay comfortably inside the caller's
	// pair timeout.
	btPairWait = 20 * time.Second
	// btPairPollInterval is the gap between Paired-property checks while waiting
	// for a pairing to complete.
	btPairPollInterval = 500 * time.Millisecond
)

// errNoAddress is returned when a device-targeted command receives no address.
var errNoAddress = errors.New("no address provided")

// errNoAdapter is returned when no Bluetooth adapter is present.
var errNoAdapter = errors.New("no Bluetooth adapter present")

// btRequest is the stdin payload for device-targeted Bluetooth commands.
type btRequest struct {
	Address string `json:"address"`
}

// btAgent is a minimal BlueZ pairing agent. With NoInputNoOutput capability the
// only callbacks BlueZ may invoke are authorization checks, which we accept so
// headless pairing "just works".
type btAgent struct{}

func (btAgent) Release() *dbus.Error                                       { return nil }
func (btAgent) Cancel() *dbus.Error                                        { return nil }
func (btAgent) AuthorizeService(dbus.ObjectPath, string) *dbus.Error       { return nil }
func (btAgent) RequestAuthorization(dbus.ObjectPath) *dbus.Error           { return nil }
func (btAgent) RequestConfirmation(dbus.ObjectPath, uint32) *dbus.Error    { return nil }
func (btAgent) RequestPinCode(dbus.ObjectPath) (string, *dbus.Error)       { return "0000", nil }
func (btAgent) RequestPasskey(dbus.ObjectPath) (uint32, *dbus.Error)       { return 0, nil }
func (btAgent) DisplayPinCode(dbus.ObjectPath, string) *dbus.Error         { return nil }
func (btAgent) DisplayPasskey(dbus.ObjectPath, uint32, uint16) *dbus.Error { return nil }

// btClient bundles a system bus connection with the resolved adapter path.
type btClient struct {
	conn    *dbus.Conn
	adapter dbus.ObjectPath
	log     zerolog.Logger
}

// btReadRequest reads the {address} JSON payload from stdin.
func btReadRequest() (btRequest, error) {
	var req btRequest

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return req, fmt.Errorf("read stdin: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return req, errNoAddress
	}

	err = json.Unmarshal(data, &req)
	if err != nil {
		return req, fmt.Errorf("invalid JSON: %w", err)
	}

	if strings.TrimSpace(req.Address) == "" {
		return req, errNoAddress
	}

	return req, nil
}

// btOpen connects to the system bus and locates the first Bluetooth adapter.
// A nil error with an empty adapter path means no adapter is present.
func (p *manager) btOpen() (*btClient, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect system bus: %w", err)
	}

	objects, err := btManagedObjects(conn)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	var adapter dbus.ObjectPath

	for path, ifaces := range objects {
		_, ok := ifaces[btAdapterIface]
		if ok {
			adapter = path

			break
		}
	}

	return &btClient{conn: conn, adapter: adapter, log: p.log}, nil
}

func (c *btClient) close() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

// btManagedObjects returns the full BlueZ object tree.
func btManagedObjects(conn *dbus.Conn) (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	obj := conn.Object(btService, "/")

	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

	err := obj.Call(dbusObjMgrIface+".GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return nil, fmt.Errorf("get managed objects: %w", err)
	}

	return objects, nil
}

// adapterInfo reads the adapter properties into a platform.CmdBTAdapter.
func (c *btClient) adapterInfo() *platform.CmdBTAdapter {
	if c.adapter == "" {
		return &platform.CmdBTAdapter{Present: false}
	}

	props, err := c.allProps(c.adapter, btAdapterIface)
	if err != nil {
		return &platform.CmdBTAdapter{Present: false}
	}

	info := &platform.CmdBTAdapter{Present: true}
	info.Powered, _ = props["Powered"].Value().(bool)
	info.Discovering, _ = props["Discovering"].Value().(bool)
	info.Address, _ = props["Address"].Value().(string)

	return info
}

// allProps returns every property of an interface on an object.
func (c *btClient) allProps(path dbus.ObjectPath, iface string) (map[string]dbus.Variant, error) {
	obj := c.conn.Object(btService, path)

	var props map[string]dbus.Variant

	err := obj.Call(dbusPropsIface+".GetAll", 0, iface).Store(&props)
	if err != nil {
		return nil, fmt.Errorf("get properties for %s: %w", iface, err)
	}

	return props, nil
}

// setProp sets a single property on an interface.
func (c *btClient) setProp(path dbus.ObjectPath, iface, name string, value any) error {
	obj := c.conn.Object(btService, path)

	err := obj.Call(dbusPropsIface+".Set", 0, iface, name, dbus.MakeVariant(value)).Err
	if err != nil {
		return fmt.Errorf("set %s.%s: %w", iface, name, err)
	}

	return nil
}

// parseDevice converts a Device1 property map into a platform.CmdBTDevice.
func parseDevice(props map[string]dbus.Variant) platform.CmdBTDevice {
	device := platform.CmdBTDevice{}
	device.Address, _ = props["Address"].Value().(string)
	device.Name, _ = props["Alias"].Value().(string)

	if device.Name == "" {
		device.Name, _ = props["Name"].Value().(string)
	}

	if device.Name == "" {
		device.Name = device.Address
	}

	bluezIcon, _ := props["Icon"].Value().(string)
	serviceUUIDs, _ := props["UUIDs"].Value().([]string)
	cod, _ := props["Class"].Value().(uint32)
	device.Type = classifyDevice(bluezIcon, cod, serviceUUIDs)
	device.Paired, _ = props["Paired"].Value().(bool)
	device.Trusted, _ = props["Trusted"].Value().(bool)
	device.Connected, _ = props["Connected"].Value().(bool)
	device.RSSI, _ = props["RSSI"].Value().(int16)

	return device
}

// serviceUUIDTypes maps an advertised GATT service UUID (lower-case) to a
// semantic device type. Centralised so adding a custom fan UUID — or another
// transport's service — later is a one-line change.
var serviceUUIDTypes = map[string]string{ //nolint:gochecknoglobals
	// nusServiceUUID is transitional: kept so un-reflashed NUS-firmware devices
	// are still classified as "fan" until they are all upgraded. Remove once all
	// devices are running the custom GATT profile firmware.
	nusServiceUUID: "fan",
	// Fan GATT profile service UUID (new firmware).
	"7a3e0001-87d1-3091-411d-000002373705": "fan",
}

// classifyDevice maps a device to a semantic, presentation-agnostic device
// type. Audio devices are recognised first by their BlueZ "Icon" hint, then by
// their Class-of-Device; fan devices by an advertised service UUID. Anything
// else returns "" and is filtered out by listDevices.
func classifyDevice(bluezIcon string, cod uint32, serviceUUIDs []string) string {
	switch bluezIcon {
	case "audio-card":
		return "speaker"
	case "audio-headphones":
		return "headphones"
	case "audio-headset":
		return "headset"
	default:
		if strings.HasPrefix(bluezIcon, "audio-") {
			return "audio"
		}
	}

	for _, uuid := range serviceUUIDs {
		deviceType, ok := serviceUUIDTypes[strings.ToLower(uuid)]
		if ok {
			return deviceType
		}
	}

	// A paired device that advertises a standard A2DP/HFP audio profile is an
	// audio device even when BlueZ has not resolved an Icon or Class-of-Device
	// (common right after pairing). The specific sink/source shape is unknown
	// here, so classify as the generic "audio" type the bridge accepts.
	if hasAudioServiceUUID(serviceUUIDs) {
		return "audio"
	}

	// Fall back to the Class-of-Device. BlueZ derives "Icon" from the CoD only
	// after name/SDP resolution, so a freshly discovered (unpaired) device often
	// has an empty Icon during a scan even though its CoD is already known from
	// the inquiry result.
	return classifyByCoD(cod)
}

// audioServiceUUID16 holds the 16-bit Bluetooth SIG UUIDs of the audio profiles
// that mark a device as an audio sink/source: A2DP (AudioSource/Sink and the
// distribution profile) and the headset/hands-free profiles.
var audioServiceUUID16 = map[string]struct{}{ //nolint:gochecknoglobals
	"110a": {}, // AudioSource (A2DP)
	"110b": {}, // AudioSink (A2DP)
	"110d": {}, // AdvancedAudioDistribution (A2DP)
	"1108": {}, // Headset
	"1112": {}, // Headset Audio Gateway
	"111e": {}, // Handsfree
	"111f": {}, // Handsfree Audio Gateway
}

// hasAudioServiceUUID reports whether any advertised UUID is a standard audio
// profile. BlueZ reports full 128-bit UUIDs (e.g.
// "0000110b-0000-1000-8000-00805f9b34fb"); the 16-bit assigned number is the
// third group of the base UUID, so match on that segment.
func hasAudioServiceUUID(serviceUUIDs []string) bool {
	for _, uuid := range serviceUUIDs {
		short := strings.ToLower(uuid)
		// Reduce "0000110b-0000-1000-8000-..." to its 16-bit "110b".
		if len(short) >= 8 && strings.HasPrefix(short, "0000") {
			short = short[4:8]
		}

		if _, ok := audioServiceUUID16[short]; ok {
			return true
		}
	}

	return false
}

// hasFriendlyName reports whether BlueZ has a real (non-MAC) name for a device.
// An unpaired device with no resolved name is aliased to its address — BlueZ
// uses the '-' separated form, our own fallback the raw ':' form — so a name
// equal to either is not a friendly name.
func hasFriendlyName(device platform.CmdBTDevice) bool {
	if device.Name == "" {
		return false
	}

	dashForm := strings.ReplaceAll(device.Address, ":", "-")

	return !strings.EqualFold(device.Name, device.Address) &&
		!strings.EqualFold(device.Name, dashForm)
}

// classifyByCoD maps a Bluetooth Class-of-Device to an audio device type. It
// recognises the Audio/Video major device class (0x04) and uses the minor class
// to distinguish headsets, headphones and speakers. A zero CoD (e.g. BLE-only
// devices with no Class-of-Device) yields "".
func classifyByCoD(cod uint32) string {
	const (
		majorAudioVideo  = 0x04
		minorHeadset     = 0x01 // Wearable Headset
		minorHandsfree   = 0x02 // Hands-free
		minorLoudspeaker = 0x05
		minorHeadphones  = 0x06
	)

	if cod == 0 {
		return ""
	}

	major := (cod >> 8) & 0x1f
	if major != majorAudioVideo {
		return ""
	}

	switch minor := (cod >> 2) & 0x3f; minor {
	case minorHeadset, minorHandsfree:
		return "headset"
	case minorHeadphones:
		return "headphones"
	case minorLoudspeaker:
		return "speaker"
	default:
		return "audio"
	}
}

// sortDevices orders devices connected-first, then paired, then by name.
func sortDevices(devices []platform.CmdBTDevice) {
	sort.Slice(devices, func(left, right int) bool {
		if devices[left].Connected != devices[right].Connected {
			return devices[left].Connected
		}

		if devices[left].Paired != devices[right].Paired {
			return devices[left].Paired
		}

		return strings.ToLower(devices[left].Name) < strings.ToLower(devices[right].Name)
	})
}

// isManagedDevice reports whether a device is one we surface in the UI: an
// audio sink/source or a fan device. Unrecognised devices are left unclassified
// (empty Type) by classifyDevice and filtered out.
func isManagedDevice(device platform.CmdBTDevice) bool {
	return device.Type != ""
}

// listDevices enumerates audio Device1 objects belonging to the adapter. When
// pairedOnly is true, only paired or connected devices are returned.
func (c *btClient) listDevices(pairedOnly bool) ([]platform.CmdBTDevice, error) {
	objects, err := btManagedObjects(c.conn)
	if err != nil {
		return nil, err
	}

	prefix := string(c.adapter) + "/"
	devices := make([]platform.CmdBTDevice, 0)

	for path, ifaces := range objects {
		props, ok := ifaces[btDeviceIface]
		if !ok {
			continue
		}

		// Only include devices belonging to our adapter.
		if c.adapter != "" && !strings.HasPrefix(string(path), prefix) {
			continue
		}

		device := parseDevice(props)

		// Trace every discovered Device1 object and how it classified, so a
		// device that BlueZ sees but we drop (e.g. an unresolved Icon/CoD) is
		// visible in the logs at debug level.
		bluezIcon, _ := props["Icon"].Value().(string)
		cod, _ := props["Class"].Value().(uint32)
		uuids, _ := props["UUIDs"].Value().([]string)
		c.log.Debug().
			Str("address", device.Address).
			Str("name", device.Name).
			Str("icon", bluezIcon).
			Uint32("class", cod).
			Strs("uuids", uuids).
			Int16("rssi", device.RSSI).
			Str("classifiedType", device.Type).
			Bool("managed", isManagedDevice(device)).
			Msg("bt-scan: discovered device")

		if pairedOnly {
			// bt-list: only classified audio/fan devices that are paired or
			// connected.
			if !isManagedDevice(device) || (!device.Paired && !device.Connected) {
				continue
			}
		} else {
			// bt-scan: classified devices plus any device with a friendly name
			// BlueZ has not classified yet. An unpaired audio device advertises
			// over LE with no Class-of-Device or SDP, so it stays unclassified
			// until paired; surfacing named unknowns lets the user pick and pair
			// their headset, while MAC-only beacons stay hidden to keep the list
			// usable.
			if !isManagedDevice(device) && !hasFriendlyName(device) {
				continue
			}
		}

		devices = append(devices, device)
	}

	sortDevices(devices)

	return devices, nil
}

// devicePath resolves a MAC address to its BlueZ object path
// (/org/bluez/hciX/dev_AA_BB_CC_DD_EE_FF).
func (c *btClient) devicePath(address string) (dbus.ObjectPath, error) {
	if c.adapter == "" {
		return "", errNoAdapter
	}

	suffix := "dev_" + strings.ReplaceAll(strings.ToUpper(address), ":", "_")

	return dbus.ObjectPath(string(c.adapter) + "/" + suffix), nil
}

// callDevice invokes a no-argument Device1 method on the device for address.
func (c *btClient) callDevice(address, method string) error {
	path, err := c.devicePath(address)
	if err != nil {
		return err
	}

	obj := c.conn.Object(btService, path)

	err = obj.Call(btDeviceIface+"."+method, 0).Err
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}

	return nil
}

// btStatus reports adapter presence/power state. It always returns success so
// the caller can gate the UI; on platforms with no D-Bus/adapter it reports
// present=false.
func (p *manager) btStatus() exitcode.Code {
	client, err := p.btOpen()
	if err != nil {
		p.log.Debug().Err(err).Msg("Bluetooth unavailable")

		outputJSON(map[string]any{
			"result":  platform.Success,
			"adapter": &platform.CmdBTAdapter{Present: false},
		})

		return exitcode.Success
	}

	defer client.close()

	outputJSON(map[string]any{
		"result":  platform.Success,
		"adapter": client.adapterInfo(),
	})

	return exitcode.Success
}

// btList returns paired and connected devices without triggering a scan.
func (p *manager) btList() exitcode.Code {
	return p.btEnumerate(true, false)
}

// btScan powers on the adapter, runs discovery, and returns all discovered and
// paired devices.
func (p *manager) btScan() exitcode.Code {
	return p.btEnumerate(false, true)
}

// btEnumerate is the shared implementation for bt-list and bt-scan.
func (p *manager) btEnumerate(pairedOnly, scan bool) exitcode.Code {
	client, err := p.btOpen()
	if err != nil {
		return btFailure(p, "Bluetooth unavailable", err)
	}

	defer client.close()

	if client.adapter == "" {
		outputJSON(map[string]any{
			"result":    platform.Success,
			"adapter":   &platform.CmdBTAdapter{Present: false},
			"btDevices": []platform.CmdBTDevice{},
		})

		return exitcode.Success
	}

	if scan {
		err = client.runDiscovery()
		if err != nil {
			return btFailure(p, "Bluetooth scan failed", err)
		}
	}

	devices, err := client.listDevices(pairedOnly)
	if err != nil {
		return btFailure(p, "failed to list Bluetooth devices", err)
	}

	outputJSON(map[string]any{
		"result":    platform.Success,
		"adapter":   client.adapterInfo(),
		"btDevices": devices,
	})

	return exitcode.Success
}

// runDiscovery powers on the adapter and runs a bounded discovery session.
func (c *btClient) runDiscovery() error {
	err := c.setProp(c.adapter, btAdapterIface, "Powered", true)
	if err != nil {
		return err
	}

	obj := c.conn.Object(btService, c.adapter)

	// Set an explicit discovery filter with Transport "auto" so BlueZ scans
	// both BR/EDR (Classic) and LE. Without a filter the session inherits
	// whatever transport the adapter last used, which on some setups is BR/EDR
	// only — so LE / dual-mode headphones that a phone or macOS sees (they
	// always scan LE) never appear. DuplicateData surfaces repeated
	// advertisements so a device that starts advertising mid-scan is still
	// caught. Best effort: a filter rejection must not abort the scan.
	filter := map[string]dbus.Variant{
		"Transport":     dbus.MakeVariant("auto"),
		"DuplicateData": dbus.MakeVariant(true),
	}

	err = obj.Call(btAdapterIface+".SetDiscoveryFilter", 0, filter).Err
	if err != nil {
		c.log.Debug().Err(err).Msg("bt-scan: SetDiscoveryFilter failed; using adapter default")
	}

	err = obj.Call(btAdapterIface+".StartDiscovery", 0).Err
	if err != nil {
		return fmt.Errorf("start discovery: %w", err)
	}

	time.Sleep(btScanDuration)

	// Best effort; ignore errors stopping discovery.
	_ = obj.Call(btAdapterIface+".StopDiscovery", 0).Err

	return nil
}

// btConnect connects to an already-paired device.
func (p *manager) btConnect() exitcode.Code {
	return p.btDeviceAction(func(client *btClient, address string) error {
		return client.callDevice(address, "Connect")
	})
}

// btDisconnect disconnects a connected device.
func (p *manager) btDisconnect() exitcode.Code {
	return p.btDeviceAction(func(client *btClient, address string) error {
		return client.callDevice(address, "Disconnect")
	})
}

// btRemove unpairs ("forgets") a device.
func (p *manager) btRemove() exitcode.Code {
	return p.btDeviceAction(func(client *btClient, address string) error {
		path, err := client.devicePath(address)
		if err != nil {
			return err
		}

		obj := client.conn.Object(btService, client.adapter)

		err = obj.Call(btAdapterIface+".RemoveDevice", 0, path).Err
		if err != nil {
			return fmt.Errorf("remove device: %w", err)
		}

		return nil
	})
}

// btPair registers an auto-accept agent, pairs, trusts, and connects a device.
func (p *manager) btPair() exitcode.Code {
	return p.btDeviceAction(func(client *btClient, address string) error {
		return client.pairDevice(address)
	})
}

// pairDevice registers a pairing agent then pairs, trusts and connects address.
func (c *btClient) pairDevice(address string) error {
	err := c.registerAgent()
	if err != nil {
		return err
	}

	defer c.unregisterAgent()

	err = c.setProp(c.adapter, btAdapterIface, "Powered", true)
	if err != nil {
		return err
	}

	path, err := c.devicePath(address)
	if err != nil {
		return err
	}

	obj := c.conn.Object(btService, path)

	// BlueZ pairing is asynchronous. A device may already be paired (so Pair()
	// would report AlreadyExists), or a pairing kicked off by an earlier attempt
	// whose helper process was killed by a short client timeout may still be
	// running (Pair() reports InProgress). In both cases the device can end up
	// paired regardless of the error, so only treat Pair() as failed when the
	// device does not become paired within btPairWait.
	if !c.isPaired(path) {
		err = obj.Call(btDeviceIface+".Pair", 0).Err
		if err != nil && !c.waitPaired(path, btPairWait) {
			return fmt.Errorf("pair: %w", err)
		}
	}

	err = c.setProp(path, btDeviceIface, "Trusted", true)
	if err != nil {
		return err
	}

	// Connect immediately after pairing. BlueZ may still be settling, so a
	// Connect issued the instant pairing completes can transiently fail
	// (org.bluez.Error.InProgress / le-connection-abort-by-local); retry until
	// the device reports Connected or btPairWait elapses. This stays best
	// effort: pairing already succeeded, so a device that never connects is
	// still left paired and trusted for the user to connect manually.
	c.connectDevice(path, obj)

	return nil
}

// deviceBoolProp reads a boolean Device1 property for the device at path,
// treating a failed read or non-bool value as false.
func (c *btClient) deviceBoolProp(path dbus.ObjectPath, name string) bool {
	props, err := c.allProps(path, btDeviceIface)
	if err != nil {
		return false
	}

	value, _ := props[name].Value().(bool)

	return value
}

// isPaired reports whether the device at path currently has Paired=true.
func (c *btClient) isPaired(path dbus.ObjectPath) bool {
	return c.deviceBoolProp(path, "Paired")
}

// isConnected reports whether the device at path currently has Connected=true.
func (c *btClient) isConnected(path dbus.ObjectPath) bool {
	return c.deviceBoolProp(path, "Connected")
}

// waitPaired polls the device's Paired property until it is true or timeout
// elapses, so an asynchronous or in-progress pairing can complete before
// pairDevice gives up.
func (c *btClient) waitPaired(path dbus.ObjectPath, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for {
		if c.isPaired(path) {
			return true
		}

		if time.Now().After(deadline) {
			return false
		}

		time.Sleep(btPairPollInterval)
	}
}

// connectDevice establishes a connection to a freshly paired device, retrying
// while BlueZ settles. It returns once the device is connected or btPairWait
// elapses, and reports whether a connection was established.
func (c *btClient) connectDevice(path dbus.ObjectPath, obj dbus.BusObject) bool {
	deadline := time.Now().Add(btPairWait)

	for {
		if c.isConnected(path) {
			return true
		}

		if obj.Call(btDeviceIface+".Connect", 0).Err == nil {
			return true
		}

		if time.Now().After(deadline) {
			return false
		}

		time.Sleep(btPairPollInterval)
	}
}

// registerAgent exports and registers a NoInputNoOutput pairing agent and makes
// it the default, so pairing proceeds without user interaction.
func (c *btClient) registerAgent() error {
	agent := btAgent{}

	err := c.conn.Export(agent, btAgentPath, btAgentIface)
	if err != nil {
		return fmt.Errorf("export agent: %w", err)
	}

	mgr := c.conn.Object(btService, btAgentManagerPath)

	err = mgr.Call(btAgentMgrIface+".RegisterAgent", 0, btAgentPath, btAgentCapability).Err
	if err != nil {
		return fmt.Errorf("register agent: %w", err)
	}

	err = mgr.Call(btAgentMgrIface+".RequestDefaultAgent", 0, btAgentPath).Err
	if err != nil {
		return fmt.Errorf("request default agent: %w", err)
	}

	return nil
}

// unregisterAgent removes the pairing agent (best effort).
func (c *btClient) unregisterAgent() {
	mgr := c.conn.Object(btService, btAgentManagerPath)
	_ = mgr.Call(btAgentMgrIface+".UnregisterAgent", 0, btAgentPath).Err
	_ = c.conn.Export(nil, btAgentPath, btAgentIface)
}

// btDeviceAction wraps the common open/read-address/run/respond flow for the
// device-targeted commands.
func (p *manager) btDeviceAction(action func(client *btClient, address string) error) exitcode.Code {
	req, err := btReadRequest()
	if err != nil {
		return btFailure(p, err.Error(), err)
	}

	client, err := p.btOpen()
	if err != nil {
		return btFailure(p, "Bluetooth unavailable", err)
	}

	defer client.close()

	if client.adapter == "" {
		return btFailure(p, errNoAdapter.Error(), nil)
	}

	err = action(client, req.Address)
	if err != nil {
		return btFailure(p, err.Error(), err)
	}

	outputJSON(map[string]any{
		"result":  platform.Success,
		"adapter": client.adapterInfo(),
	})

	return exitcode.Success
}

// btFailure logs and emits a JSON failure response, returning a general error
// exit code.
func btFailure(p *manager, msg string, err error) exitcode.Code {
	if err != nil {
		p.log.Debug().Err(err).Msg(msg)
	} else {
		p.log.Debug().Msg(msg)
	}

	outputJSON(map[string]any{
		"error":  msg,
		"result": platform.Failure,
	})

	return exitcode.GeneralErr
}
