package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
)

const (
	securityNone = "none"
	securityWEP  = "wep"
	securityWPA  = "wpa"
	securityWPA2 = "wpa2"
	securityWPA3 = "wpa3"

	keyMgmtNone = "none"
	keyMgmtPSK  = "wpa-psk"
	keyMgmtSAE  = "sae"
)

// scanWiFi triggers a WiFi network scan and returns the list of available networks
// as JSON output including SSID and security type for each network.
func (m *manager) scanWiFi() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	networks, err := m.getAvailableNetworks()
	if err != nil {
		errMsg := "failed to scan WiFi networks"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	outputJSON(map[string]any{
		"result":   setupmode.ResultSuccess,
		"networks": networks,
	})

	return exitcode.Success
}

// wifiDetails returns the network access details for the setup mode access point
// including SSID, PSK, and security type. Only works when setup mode is active.
func (m *manager) wifiDetails() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	connections, err := m.getConnections()
	if err != nil {
		errMsg := "failed to get connections"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	// Check if SetupMode profile is active
	isActive, exists := connections[m.setupModeProfile]
	if !exists || !isActive {
		errMsg := "SetupMode profile is not active"
		m.log.Debug().Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	outputJSON(setupmode.CmdResponse{
		Result: setupmode.ResultSuccess,
		WiFi: &setupmode.CmdNetworkInfo{
			SSID:     m.setupModeConfig.ssid,
			PSK:      m.setupModeConfig.psk,
			Security: m.setupModeConfig.security,
		},
	})

	return exitcode.Success
}

// provisionSetupModeConnection creates the setup mode access point network
// configuration using the manager's default setup mode settings.
func (m *manager) provisionSetupModeConnection() error {
	m.log.Debug().Msg("Provisioning SetupMode connection")

	err := m.saveNetworkConfiguration(m.setupModeConfig)
	if err != nil {
		return fmt.Errorf("failed to provision network: %w", err)
	}

	return nil
}

// provisionRunModeConnection reads network configuration from stdin as JSON and
// creates the run mode connection profile for connecting to a user's WiFi network.
func (m *manager) provisionRunModeConnection() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	// Read JSON from stdin
	var inputConfig []struct {
		SSID     string `json:"ssid"`
		PSK      string `json:"psk"`
		Security string `json:"security"`
		Method   string `json:"method"`
		IP       string `json:"ip"`
		Prefix   string `json:"prefix"`
		Gateway  string `json:"gateway"`
		DNS      string `json:"dns"`
	}

	decoder := json.NewDecoder(os.Stdin)

	err := decoder.Decode(&inputConfig)
	if err != nil {
		errMsg := "failed to parse JSON input"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	if len(inputConfig) == 0 {
		errMsg := "no network configuration provided"
		m.log.Debug().Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	connData := inputConfig[0]

	config := networkConfig{
		name:        runModeProfile,
		autoconnect: true,
		ssid:        connData.SSID,
		mode:        "infrastructure",
		band:        "bg",
		psk:         connData.PSK,
		security:    connData.Security,
		method:      connData.Method,
		ipAddr:      connData.IP,
		prefix:      connData.Prefix,
		gateway:     connData.Gateway,
		dns:         strings.Split(connData.DNS, ","),
	}

	err = m.saveNetworkConfiguration(config)
	if err != nil {
		errMsg := "failed to provision RunMode connection"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

// getConnectionsFromFiles reads NetworkManager connection profiles directly from
// the filesystem. This is used as a fallback when NetworkManager D-Bus is not ready.
func (m *manager) getConnectionsFromFiles() map[string]bool {
	const connectionDir = "/etc/NetworkManager/system-connections"

	connections := make(map[string]bool)

	// Read all files in the connection directory
	entries, err := os.ReadDir(connectionDir)
	if err != nil {
		m.log.Debug().Err(err).Str("dir", connectionDir).Msg("Failed to read connection directory")

		return connections
	}

	// Find all .nmconnection files
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if strings.HasSuffix(filename, ".nmconnection") {
			// Strip the .nmconnection extension
			connName := strings.TrimSuffix(filename, ".nmconnection")
			m.log.Debug().Str("file", filename).Str("connection", connName).Msg("Found connection file")
			connections[connName] = false // Cannot determine if active from file
		}
	}

	return connections
}

// getConnections retrieves all WiFi connection profiles and their active status.
// Returns a map of connection names to boolean indicating if currently active.
func (m *manager) getConnections() (map[string]bool, error) {
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return map[string]bool{}, fmt.Errorf("get network settings: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		m.log.Debug().Err(err).Msg("NetworkManager not ready, checking connection files")

		fileConnections := m.getConnectionsFromFiles()
		if len(fileConnections) == 0 {
			return map[string]bool{}, nil
		}

		return fileConnections, nil
	}

	activeConnPaths, err := m.getActiveConnectionPaths()
	if err != nil {
		return map[string]bool{}, err
	}

	return m.buildWirelessConnectionMap(connections, activeConnPaths), nil
}

// getActiveConnectionPaths retrieves the D-Bus object paths of all currently
// active network connections from NetworkManager.
func (m *manager) getActiveConnectionPaths() (map[string]bool, error) {
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return nil, fmt.Errorf("get network manager: %w", err)
	}

	activeConnections, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return nil, fmt.Errorf("get active connections: %w", err)
	}

	activeConnPaths := make(map[string]bool)

	for _, activeConn := range activeConnections {
		connPath, err := activeConn.GetPropertyConnection()
		if err != nil {
			continue
		}

		activeConnPaths[string(connPath.GetPath())] = true
	}

	return activeConnPaths, nil
}

// buildWirelessConnectionMap filters the connection list to WiFi connections only
// and marks each as active or inactive based on the provided active paths.
func (m *manager) buildWirelessConnectionMap(connections []gonetworkmanager.Connection, activeConnPaths map[string]bool) map[string]bool {
	connIDs := map[string]bool{}

	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"] //nolint:varnamelen // descriptive enough
		if !ok {
			continue
		}

		connType, ok := connMap["type"].(string)
		if !ok || connType != "802-11-wireless" {
			continue
		}

		if id, ok := connMap["id"].(string); ok {
			connIDs[id] = activeConnPaths[string(conn.GetPath())]
		}
	}

	return connIDs
}

// getAvailableNetworks scans for WiFi networks and returns a deduplicated list
// of discovered networks with their SSIDs and detected security types.
func (m *manager) getAvailableNetworks() ([]setupmode.CmdNetworkInfo, error) {
	m.log.Debug().Msg("Scanning for available WiFi networks")

	wifiDevice, err := m.findWiFiDevice()
	if err != nil {
		return nil, err
	}

	err = wifiDevice.RequestScan()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to request scan")
	}

	accessPoints, err := wifiDevice.GetAccessPoints()
	if err != nil {
		return nil, fmt.Errorf("failed to get access points: %w", err)
	}

	networks := m.buildNetworkList(accessPoints)
	m.log.Debug().Int("count", len(networks)).Msg("Scan results")

	return networks, nil
}

// findWiFiDevice locates and returns the WiFi device matching the configured
// WLAN interface name from NetworkManager's device list.
//
//nolint:ireturn // NetworkManager API requires interface type
func (m *manager) findWiFiDevice() (gonetworkmanager.DeviceWireless, error) {
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	devices, err := nm.GetDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}

	for _, device := range devices {
		devType, err := device.GetPropertyDeviceType()
		if err != nil {
			continue
		}

		if devType != gonetworkmanager.NmDeviceTypeWifi {
			continue
		}

		iface, err := device.GetPropertyInterface()
		if err != nil {
			continue
		}

		if iface == wlanInterface {
			wifiDevice, err := gonetworkmanager.NewDeviceWireless(device.GetPath())
			if err != nil {
				return nil, fmt.Errorf("failed to create WiFi device: %w", err)
			}

			return wifiDevice, nil
		}
	}

	return nil, fmt.Errorf("%s device not found", wlanInterface)
}

// buildNetworkList processes a list of access points and returns a deduplicated
// list of network information structs with SSID and security type.
func (m *manager) buildNetworkList(accessPoints []gonetworkmanager.AccessPoint) []setupmode.CmdNetworkInfo {
	seen := make(map[string]bool)

	var networks []setupmode.CmdNetworkInfo

	for _, accessPoint := range accessPoints {
		ssid, err := accessPoint.GetPropertySSID()
		if err != nil || ssid == "" {
			continue
		}

		if !seen[ssid] {
			seen[ssid] = true
			security := detectSecurityType(accessPoint)
			networks = append(networks, setupmode.CmdNetworkInfo{
				SSID:     ssid,
				Security: security,
			})
		}
	}

	return networks
}

// detectSecurityType examines the WPA and RSN flags of an access point to
// determine its security type (WPA3, WPA2, WPA, WEP, or none).
func detectSecurityType(accessPoint gonetworkmanager.AccessPoint) string {
	wpaFlags, err := accessPoint.GetPropertyWPAFlags()
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get WPA flags")
	}

	rsnFlags, err := accessPoint.GetPropertyRSNFlags()
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get RSN flags")
	}

	// Check for WPA3 (SAE)
	if (rsnFlags & uint32(gonetworkmanager.Nm80211APSecKeyMgmtSAE)) != 0 {
		return securityWPA3
	}

	// Check for WPA2 (RSN with PSK)
	if (rsnFlags & uint32(gonetworkmanager.Nm80211APSecKeyMgmtPSK)) != 0 {
		return securityWPA2
	}

	// Check for WPA (WPA flags with PSK)
	if (wpaFlags & uint32(gonetworkmanager.Nm80211APSecKeyMgmtPSK)) != 0 {
		return securityWPA
	}

	// Check for WEP
	if (wpaFlags&uint32(gonetworkmanager.Nm80211APSecPairWEP40)) != 0 ||
		(wpaFlags&uint32(gonetworkmanager.Nm80211APSecPairWEP104)) != 0 {
		return securityWEP
	}

	// No security (open network)
	if wpaFlags == 0 && rsnFlags == 0 {
		return securityNone
	}

	// Default to WPA2 if we can't determine
	return securityWPA2
}

// saveNetworkConfiguration validates, backs up any existing profile, and creates
// a new NetworkManager connection profile with the specified configuration.
func (m *manager) saveNetworkConfiguration(config networkConfig) error {
	m.log.Debug().Str("name", config.name).Msg("Saving network configuration")

	err := m.validateIPConfiguration(config)
	if err != nil {
		log.Debug().Err(err).Msg("IP configuration validation failed")

		return fmt.Errorf("invalid IP configuration: %w", err)
	}

	err = m.backupConnectionProfile(config.name)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to backup existing connection")
	}

	err = m.deleteConnectionProfile(config.name)
	if err != nil {
		m.restoreConnectionProfile(config.name)

		return fmt.Errorf("delete existing connection: %w", err)
	}

	settings := m.buildBaseConnectionSettings(config)
	m.addSecuritySettings(settings, config)

	err = m.addIPSettings(settings, config)
	if err != nil {
		return err
	}

	return m.addAndActivateConnection(settings, config.name)
}

// buildBaseConnectionSettings creates the base NetworkManager connection settings
// map with connection and WiFi configuration but without security or IP settings.
func (m *manager) buildBaseConnectionSettings(config networkConfig) gonetworkmanager.ConnectionSettings {
	return gonetworkmanager.ConnectionSettings{
		"connection": map[string]any{
			"id":             config.name,
			"type":           "802-11-wireless",
			"interface-name": wlanInterface,
			"autoconnect":    config.autoconnect,
		},
		"802-11-wireless": map[string]any{
			"ssid": []byte(config.ssid),
			"mode": config.mode,
		},
	}
}

// addSecuritySettings adds WiFi security settings to the connection configuration
// based on the specified security type (WPA3, WPA2, WPA, WEP, or none).
func (m *manager) addSecuritySettings(settings gonetworkmanager.ConnectionSettings, config networkConfig) {
	if config.security == "none" || config.psk == "" {
		return
	}

	securitySettings := map[string]any{
		"psk":       config.psk,
		"psk-flags": uint32(0),
	}

	switch config.security {
	case securityWPA3:
		securitySettings["key-mgmt"] = keyMgmtSAE
	case securityWPA2, securityWPA:
		securitySettings["key-mgmt"] = keyMgmtPSK
	case securityWEP:
		securitySettings["key-mgmt"] = keyMgmtNone
		securitySettings["wep-key0"] = config.psk
		securitySettings["wep-key-type"] = uint32(1)
		delete(securitySettings, "psk")
		delete(securitySettings, "psk-flags")
	default:
		securitySettings["key-mgmt"] = keyMgmtPSK
	}

	settings["802-11-wireless-security"] = securitySettings
}

// addIPSettings adds IPv4 configuration to the connection settings based on
// whether DHCP or static IP addressing is specified.
func (m *manager) addIPSettings(settings gonetworkmanager.ConnectionSettings, config networkConfig) error {
	switch config.method {
	case "static":
		ipv4Settings, err := m.buildStaticIPSettings(config)
		if err != nil {
			return err
		}

		settings["ipv4"] = ipv4Settings
	case "dhcp":
		settings["ipv4"] = map[string]any{"method": "auto"}
	default:
		return fmt.Errorf("unsupported IP method: %s", config.method)
	}

	return nil
}

// buildStaticIPSettings constructs the IPv4 settings map for static IP configuration
// including address, prefix, gateway, and DNS servers.
func (m *manager) buildStaticIPSettings(config networkConfig) (map[string]any, error) {
	var dnsAddr []uint32

	for _, dnsServer := range config.dns {
		dnsIP := net.ParseIP(dnsServer)
		if dnsIP != nil {
			ipv4 := dnsIP.To4()
			if ipv4 != nil {
				dnsUint32 := uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 | uint32(ipv4[2])<<8 | uint32(ipv4[3])
				dnsAddr = append(dnsAddr, dnsUint32)
			}
		}
	}

	var prefixUint uint32

	_, err := fmt.Sscanf(config.prefix, "%d", &prefixUint)
	if err != nil {
		return nil, fmt.Errorf("invalid prefix format: %w", err)
	}

	ipv4Settings := map[string]any{
		"method": "manual",
		"address-data": []map[string]any{
			{
				"address": config.ipAddr,
				"prefix":  prefixUint,
			},
		},
	}

	if config.gateway != "" {
		ipv4Settings["gateway"] = config.gateway
	}

	if len(dnsAddr) > 0 {
		ipv4Settings["dns"] = dnsAddr
	}

	return ipv4Settings, nil
}

// addAndActivateConnection adds the connection to NetworkManager and optionally
// activates it if the profile is the run mode connection.
func (m *manager) addAndActivateConnection(settings gonetworkmanager.ConnectionSettings, profileName string) error {
	settingsObj, err := gonetworkmanager.NewSettings()
	if err != nil {
		m.restoreConnectionProfile(profileName)

		return fmt.Errorf("get network settings: %w", err)
	}

	_, err = settingsObj.AddConnection(settings)
	if err != nil {
		m.restoreConnectionProfile(profileName)

		return fmt.Errorf("add connection: %w", err)
	}

	if profileName == runModeProfile {
		err = m.activateConnection(runModeProfile)
		if err != nil {
			m.restoreConnectionProfile(profileName)

			return fmt.Errorf("switch to run mode: %w", err)
		}
	}

	return nil
}

// activateConnection activates the specified network connection profile on the
// WLAN interface through NetworkManager.
func (m *manager) activateConnection(connName string) error {
	m.log.Debug().Str("profile", connName).Msg("Activating connection profile")

	nm, err := gonetworkmanager.NewNetworkManager() //nolint:varnamelen // not confusing
	if err != nil {
		return fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	conn, err := m.findConnectionByName(connName)
	if err != nil {
		return err
	}

	wlanDevice, err := m.findWlanDevice(nm)
	if err != nil {
		return err
	}

	_, err = nm.ActivateConnection(conn, wlanDevice, nil)
	if err != nil {
		return fmt.Errorf("failed to bring up connection %q: %w", connName, err)
	}

	m.log.Debug().Str("result", "success").Str("name", connName).Msg("Activate connection profile")

	return nil
}

// findConnectionByName searches NetworkManager connections and returns the
// connection object matching the specified name.
//
//nolint:ireturn // NetworkManager API requires interface type
func (m *manager) findConnectionByName(connName string) (gonetworkmanager.Connection, error) {
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}

	for _, nmConn := range connections {
		connSettings, err := nmConn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok && id == connName {
			return nmConn, nil
		}
	}

	return nil, fmt.Errorf("no connection with name %q", connName)
}

// findWlanDevice locates and returns the WLAN network device from NetworkManager.
//
//nolint:ireturn // NetworkManager API requires interface type
func (m *manager) findWlanDevice(nm gonetworkmanager.NetworkManager) (gonetworkmanager.Device, error) {
	devices, err := nm.GetDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}

	for _, device := range devices {
		iface, err := device.GetPropertyInterface()
		if err == nil && iface == wlanInterface {
			return device, nil
		}
	}

	return nil, fmt.Errorf("%s device not found", wlanInterface)
}

// backupConnectionProfile creates a backup copy of an existing connection profile
// with "-Backup" suffix for potential restoration if the new configuration fails.
func (m *manager) backupConnectionProfile(profileName string) error {
	backupName := profileName + "-Backup"
	m.log.Debug().Str("profile", profileName).Msg("Backing up connection profile")

	// First, delete any existing backup
	err := m.deleteConnectionProfile(backupName)
	if err != nil {
		return fmt.Errorf("failed to delete existing backup: %w", err)
	}

	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings for backup: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to list connections for backup: %w", err)
	}

	// Now find and backup the current connection
	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok && id == profileName {
			// Clone the settings and rename to backup
			connMap["id"] = backupName
			connMap["autoconnect"] = false // Don't auto-connect to backup

			_, err = settings.AddConnection(connSettings)
			if err != nil {
				return fmt.Errorf("failed to create backup connection profile: %w", err)
			}

			m.log.Debug().Str("from", profileName).Str("to", backupName).Msg("Connection profile backup")

			return nil
		}
	}

	m.log.Debug().Str("profile", profileName).Msg("No existing connection profile to backup")

	return nil
}

// restoreConnectionProfile restores a previously backed up connection profile,
// deleting the current profile and recreating it from the backup.
func (m *manager) restoreConnectionProfile(profileName string) {
	backupName := profileName + "-Backup"
	m.log.Debug().Str("profile", profileName).Msg("Restoring connection profile from backup")

	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		m.log.Debug().Err(err).Str("profile", profileName).Msg("Failed to restore connection profile")

		return
	}

	backupConn, backupSettings := m.findBackupConnection(settings, backupName)
	if backupConn == nil {
		m.log.Debug().Msg("No backup connection found to restore")

		return
	}

	m.performRestore(settings, backupConn, backupSettings, profileName, backupName)
}

// findBackupConnection searches for a backup connection profile by name and
// returns both the connection object and its settings for restoration.
//
//nolint:ireturn // NetworkManager API requires interface type
func (m *manager) findBackupConnection(
	settings gonetworkmanager.Settings,
	backupName string,
) (gonetworkmanager.Connection, gonetworkmanager.ConnectionSettings) {
	connections, err := settings.ListConnections()
	if err != nil {
		m.log.Debug().Err(err).Msg("Failed to list connections for restore")

		return nil, nil
	}

	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok && id == backupName {
			return conn, connSettings
		}
	}

	return nil, nil
}

// performRestore executes the actual restoration of a connection profile from
// backup, deleting the current profile and recreating it with the backup settings.
func (m *manager) performRestore(
	settings gonetworkmanager.Settings,
	backupConn gonetworkmanager.Connection,
	backupSettings gonetworkmanager.ConnectionSettings,
	profileName, backupName string,
) {
	err := m.deleteConnectionProfile(profileName)
	if err != nil {
		m.log.Debug().Err(err).Str("profile", profileName).Msg("Delete connection profile")
	}

	if connMap, ok := backupSettings["connection"]; ok {
		connMap["id"] = profileName
		connMap["autoconnect"] = true
	}

	_, err = settings.AddConnection(backupSettings)
	if err != nil {
		m.log.Debug().Err(err).Str("profile", profileName).Msg("Restore connection profile")

		return
	}

	err = backupConn.Delete()
	if err != nil {
		m.log.Debug().Err(err).Str("profile", backupName).Msg("Delete connection profile")
	}
}

// deleteConnectionProfile removes a NetworkManager connection profile by name.
// Returns nil if the profile doesn't exist.
func (m *manager) deleteConnectionProfile(profileName string) error {
	m.log.Debug().Str("profile", profileName).Msg("Deleting connection profile")

	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to list connections: %w", err)
	}

	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok && id == profileName {
			err = conn.Delete()
			if err != nil {
				return fmt.Errorf("failed to delete connection profile: %w", err)
			}

			m.log.Debug().Str("result", "success").Str("profile", profileName).Msg("Delete connection profile")

			return nil
		}
	}

	m.log.Debug().Str("profile", profileName).Msg("No existing connection profile to delete")

	return nil
}
