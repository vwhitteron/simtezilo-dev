package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
	"golang.org/x/crypto/ssh"
)

type (
	systemctlCmd string
)

const (
	wlanInterface    = "wlan0"
	runModeProfile   = "RunMode"
	setupModeProfile = "SetupMode"
	setupModeFlag    = "/boot/firmware/simtezilo/SETUPMODE"
	sshUser          = "admin"

	sysctlEnable    systemctlCmd = "enable"
	sysctlDisable   systemctlCmd = "disable"
	sysctlStart     systemctlCmd = "start"
	sysctlStop      systemctlCmd = "stop"
	sysctlIsActive  systemctlCmd = "is-active"
	sysctlIsEnabled systemctlCmd = "is-enabled"

	securityNone = "none"
	securityWEP  = "wep"
	securityWPA  = "wpa"
	securityWPA2 = "wpa2"
	securityWPA3 = "wpa3"

	keyMgmtNone = "none"
	keyMgmtPSK  = "wpa-psk"
	keyMgmtSAE  = "sae"

	// Update-related paths.
	updateInstallDir = "/opt/simtezilo/bin"
	updateInitDir    = "/opt/simtezilo/init"
	updateDataDir    = "/opt/simtezilo/data/update"
	updateBinaryName = "simtezilo"
	updateStateFile  = "/opt/simtezilo/data/update/update-state.json"

	// Update status constants.
	updateStatusPending    = "pending"
	updateStatusInstalling = "installing"
	updateStatusComplete   = "complete"
	updateStatusFailed     = "failed"
	updateStatusRolledBack = "rolled_back"
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

// main is the entry point for the platform management command. It parses command-line
// arguments and dispatches to the appropriate subcommand handler.
func main() { //nolint:cyclop // easy enough to understand
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
	case "setup-disable":
		exitCode = mgr.disableSetupModeFlag()
	case "setup-enable":
		exitCode = mgr.enableSetupModeFlag()
	case "init":
		exitCode = mgr.init()
	case "mode-run":
		exitCode = mgr.enterRunMode()
	case "mode-setup":
		exitCode = mgr.enterSetupMode()
	case "reset":
		exitCode = mgr.reset()
	case "status":
		exitCode = mgr.status()
	case "ssh-enable":
		exitCode = mgr.enableSSH()
	case "ssh-disable":
		exitCode = mgr.disableSSH()
	case "ssh-provision":
		exitCode = mgr.provisionSSH()
	case "wifi-access":
		exitCode = mgr.wifiDetails()
	case "wifi-provision":
		exitCode = mgr.provisionRunModeConnection()
	case "wifi-scan":
		exitCode = mgr.scanWiFi()
	case "update-apply":
		exitCode = mgr.updateApply()
	case "update-rollback":
		exitCode = mgr.updateRollback()
	case "version":
		exitCode = printVersion()
	case "help":
		fallthrough
	default:
		exitCode = printUsage()
	}

	os.Exit(int(exitCode))
}

// printUsage outputs the command-line usage information to stderr and returns
// an error exit code indicating incorrect command usage.
func printUsage() exitcode.Code {
	fmt.Fprintf(os.Stderr, "Usage: %s <command>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\nOptions:\n")
	fmt.Fprintf(os.Stderr, "  -h                Show this help message\n")
	fmt.Fprintf(os.Stderr, "  -l <level>        Set log level (debug, info, warn, error)\n")
	fmt.Fprintf(os.Stderr, "  -v                Show version information\n")
	fmt.Fprintf(os.Stderr, "\nCommands:\n")
	fmt.Fprintf(os.Stderr, "  init              Initialize setup mode connection if not present\n")
	fmt.Fprintf(os.Stderr, "  mode-run          Enter run mode\n")
	fmt.Fprintf(os.Stderr, "  mode-setup        Enter setup mode\n")
	fmt.Fprintf(os.Stderr, "  reset             Delete all connections and reinitialize setup mode\n")
	fmt.Fprintf(os.Stderr, "  setup-disable     Disable setup mode flag\n")
	fmt.Fprintf(os.Stderr, "  setup-enable      Enable setup mode flag\n")
	fmt.Fprintf(os.Stderr, "  ssh-enable        Enable SSH service\n")
	fmt.Fprintf(os.Stderr, "  ssh-disable       Disable SSH service\n")
	fmt.Fprintf(os.Stderr, "  ssh-provision     Provision SSH access\n")
	fmt.Fprintf(os.Stderr, "  status            Check current environment status\n")
	fmt.Fprintf(os.Stderr, "  update-apply      Apply a pending update (extracts, installs, swaps binaries)\n")
	fmt.Fprintf(os.Stderr, "  update-rollback   Rollback to the previous version\n")
	fmt.Fprintf(os.Stderr, "  version           Print version information\n")
	fmt.Fprintf(os.Stderr, "  wifi-access       Provide the network access details for the setup mode network\n")
	fmt.Fprintf(os.Stderr, "  wifi-provision    Provision network connection\n")
	fmt.Fprintf(os.Stderr, "  wifi-scan         Scan for available WiFi networks\n")
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

// printVersion outputs version information including version number, commit hash,
// build time, and platform to stdout.
func printVersion() exitcode.Code {
	fmt.Printf("Version: %s  Commit Hash: %s  Build Time: %s  Platform: %s\n", app.Version, app.CommitHash, app.BuildTime, app.Platform) //nolint:forbidigo // Allow for version output

	return exitcode.Success
}

// newManager creates and initializes a new manager instance with the specified
// log level and default configuration for setup mode networking.
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

// isNetworkManagerReady checks whether the NetworkManager D-Bus service is available
// and responding to requests.
func (m *manager) isNetworkManagerReady() bool {
	_, err := gonetworkmanager.NewNetworkManager()

	return err == nil
}

// waitForNetworkManager blocks until NetworkManager becomes available or the maximum
// wait time is exceeded. Returns true if NetworkManager is ready, false otherwise.
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
			m.log.Debug().Msg(errMsg)

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

// status checks and reports the current environment status including setup mode flag,
// network connection profiles, SSH state, and whether setup is required.
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
		SSHEnabled:       m.isSSHEnabled(),
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
		m.log.Debug().Err(err).Msg(errMsg)

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

// isSSHEnabled checks whether the SSH service is both enabled and currently active.
func (m *manager) isSSHEnabled() bool {
	enabled, _ := m.controlSystemd("ssh.service", sysctlIsEnabled)
	active, _ := m.controlSystemd("ssh.service", sysctlIsActive)

	return enabled == "enabled" && active == "active"
}

// enableSSH enables and starts the SSH service using systemctl.
func (m *manager) enableSSH() exitcode.Code {
	return m.controlSSH([]systemctlCmd{sysctlEnable, sysctlStart})
}

// disableSSH stops and disables the SSH service using systemctl.
func (m *manager) disableSSH() exitcode.Code {
	return m.controlSSH([]systemctlCmd{sysctlStop, sysctlDisable})
}

// controlSSH executes a sequence of systemctl actions on the SSH service.
func (m *manager) controlSSH(actions []systemctlCmd) exitcode.Code {
	for _, action := range actions {
		_, err := m.controlSystemd("ssh.service", sysctlStart)
		if err != nil {
			m.log.Debug().Err(err).Msgf("failed to %s sshd service", action)

			outputJSON(map[string]any{
				"result": setupmode.ResultFailure,
				"error":  fmt.Errorf("failed to %s sshd service", action),
			})

			return exitcode.GeneralErr
		}
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

// provisionSSH reads an SSH public key from stdin and installs it to the admin
// user's authorized_keys file, creating the .ssh directory if needed.
func (m *manager) provisionSSH() exitcode.Code {
	// Read public key from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		errMsg := "failed to read SSH key from stdin"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	sshKey := strings.TrimSpace(string(data))
	if sshKey == "" {
		errMsg := "no SSH key provided"
		m.log.Debug().Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	// Validate the SSH public key
	_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(sshKey)) //nolint:dogsled // validation only
	if err != nil {
		errMsg := "invalid SSH public key format"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	// Ensure the .ssh directory exists
	sshDir := "/home/" + sshUser + "/.ssh"

	err = os.MkdirAll(sshDir, 0o700)
	if err != nil {
		m.log.Debug().
			Err(err).
			Str("path", sshDir).
			Msg("Create .ssh directory")

		outputJSON(map[string]any{
			"error":  "failed to create .ssh directory",
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	// Write the authorized_keys file
	authorizedKeysPath := "/home/" + sshUser + "/.ssh/authorized_keys"

	err = os.WriteFile(authorizedKeysPath, []byte(sshKey+"\n"), 0o600)
	if err != nil {
		m.log.Debug().
			Err(err).
			Str("path", authorizedKeysPath).
			Msg("Write authorized_keys file")

		outputJSON(map[string]any{
			"error":  "failed to write authorized_keys file",
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	m.log.Debug().Str("path", authorizedKeysPath).Msg("SSH key provisioned successfully")

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

// init initializes the setup mode network connection if it does not already exist.
// This is typically called during first boot or after a factory reset.
func (m *manager) init() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	connections, err := m.getConnections()
	if err != nil {
		errMsg := "failed to get network connections"
		m.log.Debug().Err(err).Msg(errMsg)

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
			m.log.Debug().Err(err).Msg(errMsg)

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

// reset deletes all network connection profiles and reinitializes the setup mode
// connection, effectively performing a factory reset of network configuration.
func (m *manager) reset() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	err := m.deleteConnectionProfile(setupModeProfile)
	if err != nil {
		m.log.Debug().Err(err).Msgf("Failed to delete %s connection", setupModeProfile)
	}

	err = m.deleteConnectionProfile(runModeProfile)
	if err != nil {
		m.log.Debug().Err(err).Msgf("Failed to delete %s connection", runModeProfile)
	}

	m.log.Debug().Msgf("Connections deleted, reinitializing %s connection", setupModeProfile)

	return m.init()
}

// disableSetupModeFlag removes the setup mode flag file, indicating that initial
// setup has been completed and the device should boot into run mode.
func (m *manager) disableSetupModeFlag() exitcode.Code {
	err := os.Remove(setupModeFlag)
	if err != nil && !os.IsNotExist(err) {
		errMsg := "failed to remove setup mode flag"
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

// enableSetupModeFlag creates the setup mode flag file, which will cause the
// device to enter setup mode on the next boot.
func (m *manager) enableSetupModeFlag() exitcode.Code {
	file, err := os.Create(setupModeFlag)
	if err != nil {
		errMsg := "failed to create setup mode flag"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	err = file.Close()
	if err != nil {
		errMsg := "failed to close setup mode flag file"
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

// enterRunMode activates the run mode network connection and stops the dnsmasq
// service, switching the device from setup mode to normal operation.
func (m *manager) enterRunMode() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	err := m.activateConnection(runModeProfile)
	if err != nil {
		errMsg := "failed to activate RunMode connection"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	// Stop dnsmasq service when entering run mode
	_, err = m.controlSystemd("dnsmasq.service", sysctlStop)
	if err != nil {
		m.log.Debug().Err(err).Msg("failed to stop dnsmasq service")
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

// enterSetupMode activates the setup mode network connection (access point) and
// starts the dnsmasq service to provide DHCP and DNS for connected clients.
func (m *manager) enterSetupMode() exitcode.Code {
	if ok := m.waitForNetworkManager(); !ok {
		return exitcode.GeneralErr
	}

	err := m.activateConnection(setupModeProfile)
	if err != nil {
		errMsg := "failed to activate SetupMode connection"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	// Give some time for the new connection to stabilize
	time.Sleep(2 * time.Second)

	// Start dnsmasq service when entering setup mode
	_, err = m.controlSystemd("dnsmasq.service", sysctlStart)
	if err != nil {
		m.log.Debug().Err(err).Msg("failed to start dnsmasq service")
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

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

// getSerial reads the device serial number from /proc/cpuinfo and returns it
// truncated to 8 characters. Returns "00000000" if the serial cannot be read.
func (m *manager) getSerial() string {
	const defaultSerial = "00000000"

	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		m.log.Debug().Err(err).Msg("Failed to read /proc/cpuinfo")

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

	m.log.Debug().Msg("Serial number not found in /proc/cpuinfo")

	return defaultSerial
}

// validateIPConfiguration validates static IP configuration parameters including
// CIDR format, gateway address, and DNS server addresses.
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

// controlSystemd executes a systemctl command on the specified service with
// automatic retry logic. Returns the command output and any error.
func (m *manager) controlSystemd(service string, command systemctlCmd) (stdout string, err error) {
	m.log.Debug().
		Str("service", service).
		Str("action", string(command)).
		Msg("Controlling systemd service")

	const maxAttempts = 5

	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cmd := exec.CommandContext(context.Background(), "systemctl", string(command), service) //nolint:gosec // action is controlled internally

		output, err := cmd.Output()
		if err == nil {
			m.log.Debug().
				Str("service", service).
				Str("action", string(command)).
				Msg("Successfully controlled systemd service")

			return strings.TrimSpace(string(output)), nil
		}

		lastErr = err
		stdout = strings.TrimSpace(string(output))
		m.log.Debug().
			Err(err).
			Str("service", service).
			Str("action", string(command)).
			Int("attempt", attempt).
			Int("max_attempts", maxAttempts).
			Msg("Failed to control service, retrying...")

		if attempt < maxAttempts {
			time.Sleep(1 * time.Second)
		}
	}

	return stdout, fmt.Errorf("failed to %s %s after %d attempts: %w", string(command), service, maxAttempts, lastErr)
}

// outputJSON marshals the given value to JSON and writes it to stdout.
// Errors during marshaling are output as a JSON error message.
func outputJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Printf("{\"error\":\"%s\"}\n", err.Error()) //nolint:forbidigo // Allow for error output

		return
	}

	fmt.Fprint(os.Stdout, string(data))
}

// =============================================================================
// Update Management
// =============================================================================

// updateState tracks the state of a pending installation (matches app/updater/installer.go).
type updateState struct {
	PendingVersion string    `json:"pendingVersion"` //nolint:tagliatelle // external API format
	CurrentVersion string    `json:"currentVersion"` //nolint:tagliatelle
	DownloadPath   string    `json:"downloadPath"`   //nolint:tagliatelle
	ExtractDir     string    `json:"extractDir"`     //nolint:tagliatelle
	SHA256         string    `json:"sha256"`         //nolint:tagliatelle
	Timestamp      time.Time `json:"timestamp"`      //nolint:tagliatelle
	Status         string    `json:"status"`         //nolint:tagliatelle
	FailCount      int       `json:"failCount"`      //nolint:tagliatelle
	LastError      string    `json:"lastError"`      //nolint:tagliatelle
}

// loadUpdateState reads and parses the update state file from disk.
// Returns nil with no error if the state file does not exist.
func (m *manager) loadUpdateState() (*updateState, error) {
	data, err := os.ReadFile(updateStateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state updateState

	err = json.Unmarshal(data, &state)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// saveUpdateState persists the update state to disk as JSON, creating the
// data directory if it doesn't exist.
func (m *manager) saveUpdateState(state *updateState) error {
	err := os.MkdirAll(updateDataDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	err = os.WriteFile(updateStateFile, data, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// clearUpdateState removes the update state file from disk.
func (m *manager) clearUpdateState() error {
	err := os.Remove(updateStateFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state file: %w", err)
	}

	return nil
}

// updateApply processes a pending update based on the current update state.
// It handles pending, complete, failed, and rolled back states appropriately.
func (m *manager) updateApply() exitcode.Code {
	state, err := m.loadUpdateState()
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to load update state")

		outputJSON(map[string]any{
			"result": "failure",
			"error":  err.Error(),
		})

		return exitcode.GeneralErr
	}

	if state == nil {
		m.log.Debug().Msg("No update state file found, nothing to do")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
		})

		return exitcode.Success
	}

	m.log.Info().
		Str("status", state.Status).
		Str("pending", state.PendingVersion).
		Str("current", state.CurrentVersion).
		Int("failCount", state.FailCount).
		Msg("Update state loaded")

	switch state.Status {
	case updateStatusPending:
		return m.applyPendingUpdate(state)
	case updateStatusComplete:
		return m.handleCompleteState(state)
	case updateStatusFailed:
		// If failed, just log and exit - rescue script will handle if needed
		m.log.Warn().
			Int("failCount", state.FailCount).
			Str("lastError", state.LastError).
			Msg("Previous update failed")

		outputJSON(map[string]any{
			"result":    "failure",
			"status":    state.Status,
			"failCount": state.FailCount,
			"lastError": state.LastError,
		})

		return exitcode.GeneralErr
	case updateStatusRolledBack, updateStatusInstalling:
		m.log.Info().Str("status", state.Status).Msg("No action needed for current state")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
			"status": state.Status,
		})

		return exitcode.Success
	default:
		m.log.Warn().Str("status", state.Status).Msg("Unknown update state")

		outputJSON(map[string]any{
			"result": "success",
			"action": "none",
			"status": state.Status,
		})

		return exitcode.Success
	}
}

// applyPendingUpdate processes an update in pending state by verifying the
// download, extracting the archive, and installing the new binary.
func (m *manager) applyPendingUpdate(state *updateState) exitcode.Code {
	m.log.Info().
		Str("from", state.CurrentVersion).
		Str("to", state.PendingVersion).
		Msg("Applying pending update")

	// Verify download exists and checksum
	if code := m.verifyUpdateDownload(state); code != exitcode.Success {
		return code
	}

	// Mark as installing
	state.Status = updateStatusInstalling

	err := m.saveUpdateState(state)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to update state to installing")
	}

	// Extract and install update
	return m.extractAndInstallUpdate(state)
}

// verifyUpdateDownload checks that the downloaded update file exists and
// verifies its SHA256 checksum if one was provided in the state.
func (m *manager) verifyUpdateDownload(state *updateState) exitcode.Code {
	if state.DownloadPath == "" {
		return m.markUpdateFailed(state, "download path is empty")
	}

	_, err := os.Stat(state.DownloadPath)
	if err != nil {
		return m.markUpdateFailed(state, fmt.Sprintf("download file not found: %v", err))
	}

	if state.SHA256 != "" {
		checkErr := m.verifyChecksum(state.DownloadPath, state.SHA256)
		if checkErr != nil {
			return m.markUpdateFailed(state, fmt.Sprintf("checksum verification failed: %v", checkErr))
		}

		m.log.Debug().Msg("Checksum verified")
	}

	return exitcode.Success
}

// extractAndInstallUpdate extracts the downloaded archive to a temporary directory
// and proceeds with installation of the extracted files.
func (m *manager) extractAndInstallUpdate(state *updateState) exitcode.Code {
	extractDir := state.ExtractDir
	if extractDir == "" {
		extractDir = filepath.Join(updateDataDir, "extract")
	}

	_ = os.RemoveAll(extractDir)

	err := os.MkdirAll(extractDir, 0o755)
	if err != nil {
		return m.markUpdateFailed(state, fmt.Sprintf("failed to create extract directory: %v", err))
	}

	m.log.Info().Str("archive", state.DownloadPath).Str("dest", extractDir).Msg("Extracting archive")

	err = m.extractArchive(state.DownloadPath, extractDir)
	if err != nil {
		_ = os.RemoveAll(extractDir)

		return m.markUpdateFailed(state, fmt.Sprintf("failed to extract archive: %v", err))
	}

	extractRoot := m.findExtractRoot(extractDir)

	return m.installExtractedUpdate(state, extractDir, extractRoot)
}

// installExtractedUpdate installs the extracted update by backing up the current
// binary, installing the new one, and copying any additional binaries and init scripts.
func (m *manager) installExtractedUpdate(state *updateState, extractDir, extractRoot string) exitcode.Code {
	extractedBinary := filepath.Join(extractRoot, "bin", updateBinaryName)

	_, err := os.Stat(extractedBinary)
	if err != nil {
		_ = os.RemoveAll(extractDir)

		return m.markUpdateFailed(state, "extracted binary not found at "+extractedBinary)
	}

	currentBinary := filepath.Join(updateInstallDir, updateBinaryName)
	rollbackBinary := filepath.Join(updateInstallDir, updateBinaryName+".rollback")

	initSourceDir := filepath.Join(extractRoot, "init")

	err = m.installInitScripts(initSourceDir)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to install init scripts")
	}

	_, err = os.Stat(currentBinary)
	if err == nil {
		m.log.Debug().Str("to", rollbackBinary).Msg("Backing up current binary")

		copyErr := m.copyFile(currentBinary, rollbackBinary)
		if copyErr != nil {
			_ = os.RemoveAll(extractDir)

			return m.markUpdateFailed(state, fmt.Sprintf("failed to backup current binary: %v", copyErr))
		}
	}

	m.log.Debug().Str("from", extractedBinary).Str("to", currentBinary).Msg("Installing new binary")

	err = m.copyFile(extractedBinary, currentBinary)
	if err != nil {
		_ = m.copyFile(rollbackBinary, currentBinary)
		_ = os.RemoveAll(extractDir)

		return m.markUpdateFailed(state, fmt.Sprintf("failed to install new binary: %v", err))
	}

	err = os.Chmod(currentBinary, 0o755)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to set executable permissions")
	}

	binSourceDir := filepath.Join(extractRoot, "bin")

	err = m.installAdditionalBinaries(binSourceDir, updateInstallDir, updateBinaryName)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to install additional binaries")
	}

	_ = os.RemoveAll(extractDir)
	_ = os.Remove(state.DownloadPath)

	state.Status = updateStatusComplete

	err = m.saveUpdateState(state)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to save completion state")
	}

	m.log.Info().Str("version", state.PendingVersion).Msg("Update installed successfully")

	outputJSON(map[string]any{
		"result":  "success",
		"action":  "installed",
		"version": state.PendingVersion,
	})

	return exitcode.Success
}

// handleCompleteState handles an update that has already completed successfully
// by cleaning up the rollback binary and clearing the state file.
func (m *manager) handleCompleteState(_ *updateState) exitcode.Code {
	m.log.Info().Msg("Update already complete, cleaning up")

	// Remove rollback binary
	rollbackBinary := filepath.Join(updateInstallDir, updateBinaryName+".rollback")
	_ = os.Remove(rollbackBinary)

	// Clear state
	_ = m.clearUpdateState()

	outputJSON(map[string]any{
		"result": "success",
		"action": "cleanup",
	})

	return exitcode.Success
}

// markUpdateFailed records a failure in the update state, incrementing the fail
// count and persisting the error reason to the state file.
func (m *manager) markUpdateFailed(state *updateState, reason string) exitcode.Code {
	m.log.Error().Str("reason", reason).Msg("Update failed")

	state.Status = updateStatusFailed
	state.LastError = reason
	state.FailCount++

	err := m.saveUpdateState(state)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to save failed state")
	}

	outputJSON(map[string]any{
		"result":    "failure",
		"error":     reason,
		"failCount": state.FailCount,
	})

	return exitcode.GeneralErr
}

// updateRollback restores the previous binary version from the rollback backup,
// moving the current (failed) binary aside and updating the state file.
func (m *manager) updateRollback() exitcode.Code {
	currentBinary := filepath.Join(updateInstallDir, updateBinaryName)
	rollbackBinary := filepath.Join(updateInstallDir, updateBinaryName+".rollback")

	_, err := os.Stat(rollbackBinary)
	if err != nil {
		m.log.Error().Msg("No rollback binary available")

		outputJSON(map[string]any{
			"result": "failure",
			"error":  "no rollback binary available",
		})

		return exitcode.GeneralErr
	}

	m.log.Info().Msg("Rolling back to previous version")

	// Move current to .failed
	failedBinary := filepath.Join(updateInstallDir, updateBinaryName+".failed")

	_, err = os.Stat(currentBinary)
	if err == nil {
		renameErr := os.Rename(currentBinary, failedBinary)
		if renameErr != nil {
			m.log.Error().Err(renameErr).Msg("Failed to move current binary to .failed")

			outputJSON(map[string]any{
				"result": "failure",
				"error":  fmt.Sprintf("failed to move current binary: %v", renameErr),
			})

			return exitcode.GeneralErr
		}
	}

	// Restore rollback
	err = os.Rename(rollbackBinary, currentBinary)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to restore rollback binary")

		// Try to restore the failed binary
		_ = os.Rename(failedBinary, currentBinary)

		outputJSON(map[string]any{
			"result": "failure",
			"error":  fmt.Sprintf("failed to restore rollback binary: %v", err),
		})

		return exitcode.GeneralErr
	}

	// Update state
	state, _ := m.loadUpdateState()
	if state != nil {
		state.Status = updateStatusRolledBack
		_ = m.saveUpdateState(state)
	}

	m.log.Info().Msg("Rollback complete")

	outputJSON(map[string]any{
		"result": "success",
		"action": "rolled_back",
	})

	return exitcode.Success
}

// verifyChecksum calculates the SHA256 hash of a file and compares it against
// the expected value, returning an error if they don't match.
func (m *manager) verifyChecksum(filePath, expected string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()

	_, err = io.Copy(hasher, file)
	if err != nil {
		return fmt.Errorf("failed to hash file: %w", err)
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

// extractArchive extracts an archive file to the destination directory, automatically
// detecting the format based on file extension (.tar.gz, .tgz, .zip, or raw binary).
func (m *manager) extractArchive(archivePath, destDir string) error {
	switch {
	case strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz"):
		return m.extractTarGz(archivePath, destDir)
	case strings.HasSuffix(archivePath, ".zip"):
		return m.extractZip(archivePath, destDir)
	default:
		// Treat as raw binary
		destBinDir := filepath.Join(destDir, "Simtezilo", "bin")

		err := os.MkdirAll(destBinDir, 0o755)
		if err != nil {
			return err
		}

		return m.copyFile(archivePath, filepath.Join(destBinDir, updateBinaryName))
	}
}

// extractTarGz extracts a gzip-compressed tar archive to the destination directory,
// preserving file permissions for executable files.
func (m *manager) extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tarReader := tar.NewReader(gzr)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		err = m.extractTarEntry(header, tarReader, destDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractTarEntry processes a single entry from a tar archive, creating directories
// or extracting files as appropriate with path traversal protection.
func (m *manager) extractTarEntry(header *tar.Header, tarReader *tar.Reader, destDir string) error {
	target := filepath.Join(destDir, header.Name) //nolint:gosec // controlled input

	if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
		return fmt.Errorf("invalid path in archive: %s", header.Name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		err := os.MkdirAll(target, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	case tar.TypeReg:
		err := m.extractTarFile(header, tarReader, target)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractTarFile extracts a regular file from a tar archive to the target path,
// creating parent directories as needed and preserving executable permissions.
func (m *manager) extractTarFile(header *tar.Header, tarReader *tar.Reader, target string) error {
	err := os.MkdirAll(filepath.Dir(target), 0o755)
	if err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	outFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	_, err = io.Copy(outFile, tarReader)
	if err != nil {
		outFile.Close()

		return fmt.Errorf("failed to write file: %w", err)
	}

	outFile.Close()

	if header.Mode&0o111 != 0 {
		err = os.Chmod(target, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", target).Msg("Failed to set executable permissions")
		}
	}

	return nil
}

// extractZip extracts a zip archive to the destination directory, preserving
// file permissions for executable files.
func (m *manager) extractZip(archivePath, destDir string) error {
	zipReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer zipReader.Close()

	for _, zipFile := range zipReader.File {
		err = m.extractZipEntry(zipFile, destDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// extractZipEntry processes a single entry from a zip archive, creating directories
// or extracting files as appropriate with path traversal protection.
func (m *manager) extractZipEntry(zipFile *zip.File, destDir string) error {
	target := filepath.Join(destDir, zipFile.Name) //nolint:gosec // controlled input

	if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)) {
		return fmt.Errorf("invalid path in archive: %s", zipFile.Name)
	}

	if zipFile.FileInfo().IsDir() {
		err := os.MkdirAll(target, 0o755)
		if err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		return nil
	}

	return m.extractZipFile(zipFile, target)
}

// extractZipFile extracts a regular file from a zip archive to the target path,
// creating parent directories as needed and preserving executable permissions.
func (m *manager) extractZipFile(zipFile *zip.File, target string) error {
	err := os.MkdirAll(filepath.Dir(target), 0o755)
	if err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	fileReader, err := zipFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer fileReader.Close()

	outFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, fileReader) //nolint:gosec // controlled input
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if zipFile.Mode()&0o111 != 0 {
		err = os.Chmod(target, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", target).Msg("Failed to set executable permissions")
		}
	}

	return nil
}

// findExtractRoot determines the root directory of the extracted archive contents,
// checking for a "Simtezilo" subdirectory or returning the extract directory itself.
func (m *manager) findExtractRoot(extractDir string) string {
	// Check for Simtezilo/ subdirectory
	simteziloDir := filepath.Join(extractDir, "Simtezilo")

	_, err := os.Stat(simteziloDir)
	if err == nil {
		return simteziloDir
	}

	return extractDir
}

// installInitScripts copies init scripts from the extracted archive to the
// system init directory, setting executable permissions on each file.
func (m *manager) installInitScripts(sourceDir string) error {
	_, err := os.Stat(sourceDir)
	if err != nil {
		m.log.Debug().Msg("No init scripts to install")

		return nil //nolint:nilerr // intentionally return nil when source dir doesn't exist
	}

	m.log.Info().Str("from", sourceDir).Str("to", updateInitDir).Msg("Installing init scripts")

	err = os.MkdirAll(updateInitDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create init directory: %w", err)
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(sourceDir, entry.Name())
		dstPath := filepath.Join(updateInitDir, entry.Name())

		m.log.Debug().Str("script", entry.Name()).Msg("Installing init script")

		err := m.copyFile(srcPath, dstPath)
		if err != nil {
			return fmt.Errorf("failed to copy %s: %w", entry.Name(), err)
		}

		err = os.Chmod(dstPath, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", dstPath).Msg("Failed to set permissions")
		}
	}

	return nil
}

// installAdditionalBinaries copies any additional binaries from the extracted
// archive to the install directory, skipping the main binary which is handled separately.
func (m *manager) installAdditionalBinaries(sourceDir, destDir, mainBinary string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == mainBinary {
			continue // Already installed
		}

		srcPath := filepath.Join(sourceDir, name)
		dstPath := filepath.Join(destDir, name)

		m.log.Debug().Str("binary", name).Msg("Installing additional binary")

		err := m.copyFile(srcPath, dstPath)
		if err != nil {
			return fmt.Errorf("failed to copy %s: %w", name, err)
		}

		err = os.Chmod(dstPath, 0o755)
		if err != nil {
			m.log.Warn().Err(err).Str("file", dstPath).Msg("Failed to set permissions")
		}
	}

	return nil
}

// copyFile copies a file from src to dst, creating the destination file if it
// doesn't exist or truncating it if it does.
func (m *manager) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return nil
}
