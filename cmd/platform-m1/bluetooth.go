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

	// btScanDuration is how long bt-scan keeps discovery running before
	// enumerating results.
	btScanDuration = 8 * time.Second
	// btAgentCapability requests no input/output, so simple devices pair
	// without prompting for a PIN or passkey confirmation.
	btAgentCapability = "NoInputNoOutput"
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

	return &btClient{conn: conn, adapter: adapter}, nil
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
	device.Type = deviceType(bluezIcon)
	device.Paired, _ = props["Paired"].Value().(bool)
	device.Trusted, _ = props["Trusted"].Value().(bool)
	device.Connected, _ = props["Connected"].Value().(bool)
	device.RSSI, _ = props["RSSI"].Value().(int16)

	return device
}

// deviceType maps the backend-specific BlueZ "Icon" hint to a semantic,
// presentation-agnostic device type. Only audio devices are classified; any
// non-audio device returns "" and is filtered out by listDevices.
func deviceType(bluezIcon string) string {
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

		return ""
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

// isAudioDevice reports whether a device is an audio sink/source. Only audio
// devices are surfaced, since they are the ones usable for haptics/pit-radio
// output. Non-audio devices are left unclassified (empty Type) by deviceType.
func isAudioDevice(device platform.CmdBTDevice) bool {
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

		// Only surface audio devices.
		if !isAudioDevice(device) {
			continue
		}

		if pairedOnly && !device.Paired && !device.Connected {
			continue
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

	err = obj.Call(btDeviceIface+".Pair", 0).Err
	if err != nil {
		return fmt.Errorf("pair: %w", err)
	}

	err = c.setProp(path, btDeviceIface, "Trusted", true)
	if err != nil {
		return err
	}

	// Connect is best effort; some devices auto-connect after pairing.
	_ = obj.Call(btDeviceIface+".Connect", 0).Err

	return nil
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
