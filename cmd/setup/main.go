package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
)

type (
	systemctlCmd string
)

const (
	wlanInterface    = "wlan0"
	runModeProfile   = "RunMode"
	setupModeProfile = "SetupMode"
	setupModeFlag    = "/boot/firmware/simtezilo/SETUPMODE"

	dnsmasqStart systemctlCmd = "start"
	dnsmasqStop  systemctlCmd = "stop"

	securityNone = "none"
	securityWEP  = "wep"
	securityWPA  = "wpa"
	securityWPA2 = "wpa2"
	securityWPA3 = "wpa3"

	keyMgmtNone = "none"
	keyMgmtPSK  = "wpa-psk"
	keyMgmtSAE  = "sae"
)

type networkConfig struct {
	name        string
	autoconnect bool
	ssid        string
	mode        string
	band        string
	psk         string
	security    string
	method      string
	ipAddr      string
	prefix      string
	gateway     string
	dns         []string
}

type manager struct {
	log              zerolog.Logger
	wlanInterface    string
	setupModeConfig  networkConfig
	runModeProfile   string
	setupModeProfile string
	setupModeFlag    string
}

func main() {
	mgr := newManager(zerolog.InfoLevel)

	var (
		action   string
		help     bool
		logLevel string
		version  bool
	)

	flag.BoolVar(&help, "h", false, "Show help message")
	flag.StringVar(&logLevel, "l", "info", "Log level. Default is 'info'")
	flag.BoolVar(&version, "v", false, "Print version information")
	flag.Parse()

	if version {
		action = "version"
	}

	if help {
		action = "help"
	}

	if logLevel != "" {
		level, err := zerolog.ParseLevel(logLevel)
		if err != nil {
			level = zerolog.InfoLevel
		}

		mgr.log = mgr.log.Level(level).With().Logger()
	}

	if len(flag.Args()) == 1 {
		action = flag.Arg(0)
	} else {
		mgr.log.Debug().Int("count", len(flag.Args())).Msg("Invalid arg count")
	}

	var exitCode exitcode.Code

	switch action {
	case "access":
		exitCode = mgr.wifiDetails()
	case "disable":
		exitCode = mgr.disableSetupModeFlag()
	case "enable":
		exitCode = mgr.enableSetupModeFlag()
	case "init":
		exitCode = mgr.init()
	case "mode-run":
		exitCode = mgr.enterRunMode()
	case "mode-setup":
		exitCode = mgr.enterSetupMode()
	case "provision":
		exitCode = mgr.provisionRunModeConnection()
	case "reset":
		exitCode = mgr.reset()
	case "scan":
		exitCode = mgr.scanWiFi()
	case "status":
		exitCode = mgr.status()
	case "version":
		exitCode = printVersion()
	case "help":
		fallthrough
	default:
		exitCode = printUsage()
	}

	os.Exit(int(exitCode))
}

func printUsage() exitcode.Code {
	fmt.Fprintf(os.Stderr, "Usage: %s <command>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  access       Provide the network access detaisl for the setup mode network\n")
	fmt.Fprintf(os.Stderr, "  disable      Disable setup mode flag\n")
	fmt.Fprintf(os.Stderr, "  enable       Enable setup mode flag\n")
	fmt.Fprintf(os.Stderr, "  init         Initialize setup mode connection if not present\n")
	fmt.Fprintf(os.Stderr, "  mode-run     Enter run mode\n")
	fmt.Fprintf(os.Stderr, "  mode-setup   Enter setup mode\n")
	fmt.Fprintf(os.Stderr, "  provision    Provision network connection\n")
	fmt.Fprintf(os.Stderr, "  reset        Delete all connections and reinitialize setup mode\n")
	fmt.Fprintf(os.Stderr, "  scan         Scan for available WiFi networks\n")
	fmt.Fprintf(os.Stderr, "  status       Check current environment status\n")
	fmt.Fprintf(os.Stderr, "  version      Print version information\n")
	fmt.Fprintf(os.Stderr, "\n  provision takes JSON on stdin with the following format:\n")
	fmt.Fprintf(os.Stderr, "  [{\n")
	fmt.Fprintf(os.Stderr, `    "ssid":"<string>",`+"\n")
	fmt.Fprintf(os.Stderr, `    "psk":"<string>",`+"\n")
	fmt.Fprintf(os.Stderr, `    "security":"<wpa2|wpa3>",`+"\n")
	fmt.Fprintf(os.Stderr, `    "method":"<dhcp|static>",`+"\n")
	fmt.Fprintf(os.Stderr, `    "ip":"<address>",`+"\n")
	fmt.Fprintf(os.Stderr, `    "prefix":"<bits>",`+"\n")
	fmt.Fprintf(os.Stderr, `    "gateway":"<address>",`+"\n")
	fmt.Fprintf(os.Stderr, `    "dns":"<address>"`+"\n")
	fmt.Fprintf(os.Stderr, "  }]\n")

	return exitcode.CommandUsageErr
}

func printVersion() exitcode.Code {
	fmt.Printf("Version: %s  Commit Hash: %s  Build Time: %s  Platform: %s\n", app.Version, app.CommitHash, app.BuildTime, app.Platform) //nolint:forbidigo // Allow for version output

	return exitcode.Success
}

func newManager(logLevel zerolog.Level) *manager {
	mgr := manager{
		log:              zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel),
		wlanInterface:    wlanInterface,
		runModeProfile:   runModeProfile,
		setupModeProfile: setupModeProfile,
		setupModeFlag:    setupModeFlag,
		setupModeConfig: networkConfig{
			name:        setupModeProfile,
			autoconnect: false,
			method:      "static",
			ipAddr:      "10.33.0.1",
			prefix:      "24",
			mode:        "ap",
			band:        "bg",
			psk:         "5imtezil0",
			security:    securityWPA2,
		},
	}

	mgr.setupModeConfig.ssid = "Simtezilo-" + mgr.getSerial()

	return &mgr
}

func (m *manager) isNetworkManagerReady() bool {
	_, err := gonetworkmanager.NewNetworkManager()

	return err == nil
}

func (m *manager) waitForNetworkManager() (ok bool) {
	const (
		maxWaitAttempts = 30
		waitInterval    = 1 * time.Second
	)

	for attempt := 1; attempt <= maxWaitAttempts; attempt++ {
		if m.isNetworkManagerReady() {
			m.log.Debug().Int("attempt", attempt).Msg("NetworkManager is ready")

			return true
		}

		if attempt == maxWaitAttempts {
			errMsg := "NetworkManager not available after waiting"
			m.log.Error().Msg(errMsg)
			outputJSON(map[string]any{
				"error":  errMsg,
				"result": setupmode.ResultFailure,
			})

			return false
		}

		m.log.Debug().Int("attempt", attempt).Int("max_attempts", maxWaitAttempts).Msg("Waiting for NetworkManager to start")
		time.Sleep(waitInterval)
	}

	return true
}

func (m *manager) status() exitcode.Code {
	status := setupmode.CmdStatus{
		Available:        true,
		ActiveConn:       "",
		FlagEnabled:      false,
		RunModePresent:   false,
		SetupModePresent: false,
		SetupRequired:    true,
		Ready:            m.isNetworkManagerReady(),
		LCDPresent:       true,
	}

	// Check if setup mode flag file exists
	_, err := os.Stat(m.setupModeFlag)
	if err == nil {
		m.log.Debug().Str("flag", m.setupModeFlag).Msg("Setup mode flag file exists, setup required")

		status.FlagEnabled = true
	}

	connections, err := m.getConnections()
	if err != nil {
		errMsg := "failed to get network connections: " + err.Error()
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"status": status,
		})

		return exitcode.GeneralErr
	}

	m.log.Debug().Strs("connections", slices.Collect(maps.Keys(connections))).Msg("Existing connections")

	// Find the active connection
	for name, isActive := range connections {
		if isActive {
			status.ActiveConn = name

			break
		}
	}

	if _, exists := connections[setupModeProfile]; exists {
		status.SetupModePresent = true
	}

	if _, exists := connections[runModeProfile]; exists {
		status.RunModePresent = true
	}

	status.SetupRequired = !status.SetupModePresent || !status.RunModePresent

	outputJSON(map[string]any{
		"result": setupmode.ResultSuccess,
		"status": status,
	})

	if status.SetupRequired {
		return exitcode.SetupRequired
	}

	return exitcode.Success
}

func (m *manager) init() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	connections, err := m.getConnections()
	if err != nil {
		errMsg := "failed to get network connections"
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	m.log.Debug().Strs("connections", slices.Collect(maps.Keys(connections))).Msg("Checking for SetupMode connection")

	if _, exists := connections[setupModeProfile]; !exists {
		m.log.Info().Msg("SetupMode connection not found, provisioning")

		err := m.provisionSetupModeConnection()
		if err != nil {
			errMsg := "failed to provision SetupMode connection"
			m.log.Error().Err(err).Msg(errMsg)
			outputJSON(map[string]any{
				"error":  errMsg,
				"result": setupmode.ResultFailure,
			})

			return exitcode.GeneralErr
		}

		m.log.Debug().Msg("SetupMode connection provisioned successfully")
		outputJSON(map[string]any{
			"result": setupmode.ResultSuccess,
			"action": "create",
		})

		return exitcode.Success
	}

	m.log.Debug().Msg("SetupMode connection already exists")
	outputJSON(map[string]any{
		"result": setupmode.ResultSuccess,
		"action": "none",
	})

	return exitcode.Success
}

func (m *manager) reset() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	err := m.deleteConnectionProfile(setupModeProfile)
	if err != nil {
		m.log.Warn().Err(err).Msgf("Failed to delete %s connection", setupModeProfile)
	}

	err = m.deleteConnectionProfile(runModeProfile)
	if err != nil {
		m.log.Warn().Err(err).Msgf("Failed to delete %s connection", runModeProfile)
	}

	m.log.Debug().Msgf("Connections deleted, reinitializing %s connection", setupModeProfile)

	return m.init()
}

func (m *manager) disableSetupModeFlag() exitcode.Code {
	err := os.Remove(setupModeFlag)
	if err != nil && !os.IsNotExist(err) {
		errMsg := "failed to remove setup mode flag"
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

func (m *manager) enableSetupModeFlag() exitcode.Code {
	file, err := os.Create(setupModeFlag)
	if err != nil {
		errMsg := "failed to create setup mode flag"
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	err = file.Close()
	if err != nil {
		errMsg := "failed to close setup mode flag file"
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

func (m *manager) enterRunMode() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	err := m.activateConnection(runModeProfile)
	if err != nil {
		errMsg := "failed to activate RunMode connection"
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	// Stop dnsmasq service when entering run mode
	err = m.controlDNSMasq(dnsmasqStop)
	if err != nil {
		m.log.Warn().Err(err).Msg("failed to stop dnsmasq service")
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

func (m *manager) enterSetupMode() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	err := m.activateConnection(setupModeProfile)
	if err != nil {
		errMsg := "failed to activate SetupMode connection"
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	// Give some time for the new connection to stabilize
	time.Sleep(2 * time.Second)

	// Start dnsmasq service when entering setup mode
	err = m.controlDNSMasq(dnsmasqStart)
	if err != nil {
		m.log.Warn().Err(err).Msg("failed to start dnsmasq service")
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

func (m *manager) provisionSetupModeConnection() error {
	m.log.Debug().Msg("Provisioning SetupMode connection")

	err := m.saveNetworkConfiguration(m.setupModeConfig)
	if err != nil {
		return fmt.Errorf("failed to provision network: %w", err)
	}

	return nil
}

func (m *manager) provisionRunModeConnection() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	// Read JSON from stdin
	var inputConfig []struct {
		SSID     string `json:"ssid"`     //nolint:tagliatelle // lowercase for easier compatibility
		PSK      string `json:"psk"`      //nolint:tagliatelle
		Security string `json:"security"` //nolint:tagliatelle
		Method   string `json:"method"`   //nolint:tagliatelle
		IP       string `json:"ip"`       //nolint:tagliatelle
		Prefix   string `json:"prefix"`   //nolint:tagliatelle
		Gateway  string `json:"gateway"`  //nolint:tagliatelle
		DNS      string `json:"dns"`      //nolint:tagliatelle
	}

	decoder := json.NewDecoder(os.Stdin)

	err := decoder.Decode(&inputConfig)
	if err != nil {
		errMsg := "failed to parse JSON input"
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	if len(inputConfig) == 0 {
		errMsg := "no network configuration provided"
		m.log.Error().Msg(errMsg)
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
		m.log.Error().Err(err).Msg(errMsg)
		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

func (m *manager) scanWiFi() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	networks, err := m.getAvailableNetworks()
	if err != nil {
		errMsg := "failed to scan WiFi networks"
		m.log.Error().Err(err).Msg(errMsg)
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

func (m *manager) wifiDetails() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	connections, err := m.getConnections()
	if err != nil {
		errMsg := "failed to get connections"
		m.log.Error().Err(err).Msg(errMsg)
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
		m.log.Error().Msg(errMsg)
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

func (m *manager) getConnections() (map[string]bool, error) {
	// Check if RunMode connection exists
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return map[string]bool{}, fmt.Errorf("get network settings: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		// Fall back to checking files if NetworkManager is not fully ready
		m.log.Debug().Err(err).Msg("NetworkManager not ready, checking connection files")

		fileConnections := m.getConnectionsFromFiles()
		if len(fileConnections) == 0 {
			return map[string]bool{}, nil
		}

		return fileConnections, nil
	}

	// Get active connections
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return map[string]bool{}, fmt.Errorf("get network manager: %w", err)
	}

	activeConnections, err := nm.GetPropertyActiveConnections()
	if err != nil {
		return map[string]bool{}, fmt.Errorf("get active connections: %w", err)
	}

	// Build map of active connection paths
	activeConnPaths := make(map[string]bool)

	for _, activeConn := range activeConnections {
		connPath, err := activeConn.GetPropertyConnection()
		if err != nil {
			continue
		}

		activeConnPaths[string(connPath.GetPath())] = true
	}

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

		// Only include 802-11-wireless connections
		connType, ok := connMap["type"].(string)
		if !ok || connType != "802-11-wireless" {
			continue
		}

		if id, ok := connMap["id"].(string); ok {
			// Check if this connection is active
			connIDs[id] = activeConnPaths[string(conn.GetPath())]
		}
	}

	return connIDs, nil
}

// func (m *manager) getAvailableNetworks() (string, error) {
// 	m.log.Debug().Msg("Scanning for available WiFi networks")

// 	return `[
// 		{"ssid":"Network1","security":"none"},
// 		{"ssid":"Network2","security":"wpa2"},
// 		{"ssid":"Network3","security":"wpa3"}
// 	]`, nil
// }

func (m *manager) getAvailableNetworks() ([]setupmode.CmdNetworkInfo, error) {
	m.log.Debug().Msg("Scanning for available WiFi networks")

	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	devices, err := nm.GetDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}

	var wifiDevice gonetworkmanager.DeviceWireless

	for _, device := range devices {
		devType, err := device.GetPropertyDeviceType()
		if err != nil {
			continue
		}

		if devType == gonetworkmanager.NmDeviceTypeWifi {
			iface, err := device.GetPropertyInterface()
			if err != nil {
				continue
			}

			if iface == wlanInterface {
				wifiDevice, err = gonetworkmanager.NewDeviceWireless(device.GetPath())
				if err != nil {
					return nil, fmt.Errorf("failed to create WiFi device: %w", err)
				}

				break
			}
		}
	}

	if wifiDevice == nil {
		return nil, fmt.Errorf("%s device not found", wlanInterface)
	}

	// Request scan
	err = wifiDevice.RequestScan()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to request scan")
	}

	// Get access points
	accessPoints, err := wifiDevice.GetAccessPoints()
	if err != nil {
		return nil, fmt.Errorf("failed to get access points: %w", err)
	}

	seen := make(map[string]bool)

	var networks []setupmode.CmdNetworkInfo

	for _, accessPoint := range accessPoints {
		ssid, err := accessPoint.GetPropertySSID()
		if err != nil || ssid == "" {
			continue
		}

		if !seen[ssid] {
			seen[ssid] = true

			// Determine security type from WPA and RSN flags
			security := detectSecurityType(accessPoint)

			networks = append(networks, setupmode.CmdNetworkInfo{
				SSID:     ssid,
				Security: security,
			})
		}
	}

	m.log.Debug().Int("count", len(networks)).Msg("Scan results")

	return networks, nil
}

func detectSecurityType(accessPoint gonetworkmanager.AccessPoint) string {
	wpaFlags, err := accessPoint.GetPropertyWPAFlags()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get WPA flags")
	}

	rsnFlags, err := accessPoint.GetPropertyRSNFlags()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get RSN flags")
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

func (m *manager) saveNetworkConfiguration(config networkConfig) error {
	m.log.Debug().Str("name", config.name).Msg("Saving network configuration")

	// Validate static IP configuration if provided
	err := m.validateIPConfiguration(config)
	if err != nil {
		log.Error().Err(err).Msg("IP configuration validation failed")

		return fmt.Errorf("invalid IP configuration: %w", err)
	}

	err = m.backupConnectionProfile(config.name)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to backup existing connection")
	}

	// Delete any existing connection
	err = m.deleteConnectionProfile(config.name)
	if err != nil {
		m.restoreConnectionProfile(config.name)

		return fmt.Errorf("delete existing connection: %w", err)
	}

	// Build connection settings
	settings := gonetworkmanager.ConnectionSettings{
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

	// Configure WiFi security based on security type
	if config.security != "none" && config.psk != "" {
		securitySettings := map[string]any{
			"psk":       config.psk,
			"psk-flags": uint32(0), // NM_SETTING_SECRET_FLAG_NONE - store password
		}

		switch config.security {
		case securityWPA3:
			securitySettings["key-mgmt"] = keyMgmtSAE
		case securityWPA2:
			securitySettings["key-mgmt"] = keyMgmtPSK
		case securityWPA:
			securitySettings["key-mgmt"] = keyMgmtPSK
		case securityWEP:
			securitySettings["key-mgmt"] = keyMgmtNone
			securitySettings["wep-key0"] = config.psk
			securitySettings["wep-key-type"] = uint32(1) // NM_WEP_KEY_TYPE_PASSPHRASE
			delete(securitySettings, "psk")
			delete(securitySettings, "psk-flags")
		default:
			// Default to WPA2 for unknown types
			securitySettings["key-mgmt"] = keyMgmtPSK
		}

		settings["802-11-wireless-security"] = securitySettings
	}

	// Configure IP settings
	switch config.method {
	case "static":
		// Parse DNS servers - convert to uint32 array (network byte order)
		var dnsAddr []uint32

		for _, dnsServer := range config.dns {
			dnsIP := net.ParseIP(dnsServer)
			if dnsIP != nil {
				// Convert IPv4 to uint32 (network byte order)
				ipv4 := dnsIP.To4()
				if ipv4 != nil {
					dnsUint32 := uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 | uint32(ipv4[2])<<8 | uint32(ipv4[3])
					dnsAddr = append(dnsAddr, dnsUint32)
				}
			}
		}

		// Convert prefix from string to uint32
		var prefixUint uint32

		_, err := fmt.Sscanf(config.prefix, "%d", &prefixUint)
		if err != nil {
			return fmt.Errorf("invalid prefix format: %w", err)
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

		// Only add gateway if non-empty
		if config.gateway != "" {
			ipv4Settings["gateway"] = config.gateway
		}

		// Only add DNS if there are servers
		if len(dnsAddr) > 0 {
			ipv4Settings["dns"] = dnsAddr
		}

		settings["ipv4"] = ipv4Settings
	case "dhcp":
		settings["ipv4"] = map[string]any{
			"method": "auto",
		}
	default:
		return fmt.Errorf("unsupported IP method: %s", config.method)
	}

	// Add the connection
	settingsObj, err := gonetworkmanager.NewSettings()
	if err != nil {
		m.restoreConnectionProfile(config.name)

		return fmt.Errorf("get network settings: %w", err)
	}

	_, err = settingsObj.AddConnection(settings)
	if err != nil {
		m.restoreConnectionProfile(config.name)

		return fmt.Errorf("add connection: %w", err)
	}

	// Only activate and switch if this is RunMode configuration
	if config.name == runModeProfile {
		// Switch from SetupMode to RunMode
		err = m.activateConnection(runModeProfile)
		if err != nil {
			m.restoreConnectionProfile(config.name)

			return fmt.Errorf("switch to run mode: %w", err)
		}
	}

	return nil
}

func (m *manager) activateConnection(connName string) error {
	m.log.Debug().Str("profile", connName).Msg("Activating connection profile")

	nm, err := gonetworkmanager.NewNetworkManager() //nolint:varnamelen // not confusing
	if err != nil {
		return fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to list connections: %w", err)
	}

	var conn gonetworkmanager.Connection

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
			conn = nmConn

			break
		}
	}

	if conn == nil {
		return fmt.Errorf("no cnnection with name %q", connName)
	}

	devices, err := nm.GetDevices()
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	var wlanDevice gonetworkmanager.Device

	for _, device := range devices {
		iface, err := device.GetPropertyInterface()
		if err == nil && iface == wlanInterface {
			wlanDevice = device

			break
		}
	}

	if wlanDevice == nil {
		return fmt.Errorf("%s device not found", wlanInterface)
	}

	_, err = nm.ActivateConnection(conn, wlanDevice, nil)
	if err != nil {
		return fmt.Errorf("failed to bring up connection %q: %w", connName, err)
	}

	m.log.Debug().Str("result", "success").Str("name", connName).Msg("Activate connection profile")

	return nil
}

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

func (m *manager) restoreConnectionProfile(profileName string) {
	backupName := profileName + "-Backup"
	m.log.Debug().Str("profile", profileName).Msg("Restoring connection profile from backup")

	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		m.log.Warn().Err(err).Str("profile", profileName).Msg("Failed to restore connection profile")

		return
	}

	connections, err := settings.ListConnections()
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to list connections for restore")

		return
	}

	// Find the backup connection
	var backupConn gonetworkmanager.Connection

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
			backupConn = conn

			break
		}
	}

	if backupConn == nil {
		m.log.Debug().Msg("No backup connection found to restore")

		return
	}

	// Get backup settings
	backupSettings, err := backupConn.GetSettings()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get backup settings")

		return
	}

	err = m.deleteConnectionProfile(profileName)
	if err != nil {
		m.log.Warn().Err(err).Str("profile", profileName).Msg("Delete connection profile")
	}

	// Rename backup back to target profile name
	if connMap, ok := backupSettings["connection"]; ok {
		connMap["id"] = profileName
		connMap["autoconnect"] = true
	}

	// Create the restored connection
	_, err = settings.AddConnection(backupSettings)
	if err != nil {
		m.log.Warn().Err(err).Str("profile", profileName).Msg("Restore connection profile")

		return
	}

	// Delete the backup
	err = backupConn.Delete()
	if err != nil {
		m.log.Warn().Err(err).Str("profile", backupName).Msg("Delete connection profile")
	}
}

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

func (m *manager) getSerial() string {
	const defaultSerial = "00000000"

	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to read /proc/cpuinfo")

		return defaultSerial
	}

	lines := strings.SplitSeq(string(data), "\n")
	for line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Serial") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				serial := strings.TrimSpace(parts[1])

				// Strip leading zeros until serial is 8 characters long
				for len(serial) > 8 && serial[0] == '0' {
					serial = serial[1:]
				}

				m.log.Debug().Str("serial", serial).Msg("Retrieved device serial number")

				return serial
			}
		}
	}

	m.log.Warn().Msg("Serial number not found in /proc/cpuinfo")

	return defaultSerial
}

func (m *manager) validateIPConfiguration(config networkConfig) error {
	m.log.Debug().Str("method", config.method).Msg("Validating IP configuration")

	if config.method != "static" {
		return nil
	}

	_, _, err := net.ParseCIDR(config.ipAddr + "/" + config.prefix)
	if err != nil {
		return errors.New("invalid CIDR format")
	}

	if config.gateway != "" && net.ParseIP(config.gateway) == nil {
		return errors.New("invalid gateway")
	}

	for _, server := range config.dns {
		if net.ParseIP(server) == nil {
			return fmt.Errorf("invalid DNS server: %s", server)
		}
	}

	return nil
}

func (m *manager) controlDNSMasq(command systemctlCmd) error {
	m.log.Debug().Str("action", string(command)).Msg("Managing dnsmasq service")

	const maxAttempts = 5

	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cmd := exec.CommandContext(context.Background(), "systemctl", string(command), "dnsmasq") //nolint:gosec // action is controlled internally

		err := cmd.Run()
		if err == nil {
			m.log.Debug().Str("action", string(command)).Msg("Successfully managed dnsmasq service")

			return nil
		}

		lastErr = err
		m.log.Warn().
			Err(err).
			Str("action", string(command)).
			Int("attempt", attempt).
			Int("max_attempts", maxAttempts).
			Msgf("Failed to %s dnsmasq, retrying...", command)

		if attempt < maxAttempts {
			time.Sleep(1 * time.Second)
		}
	}

	return fmt.Errorf("failed to %s dnsmasq after %d attempts: %w", string(command), maxAttempts, lastErr)
}

func outputJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Printf("{\"error\":\"%s\"}\n", err.Error()) //nolint:forbidigo // Allow for error output

		return
	}

	fmt.Fprint(os.Stdout, string(data))
}
