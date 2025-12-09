package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/rs/zerolog"
	"github.com/skip2/go-qrcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
)

const (
	wlanInterface        = "wlan0"
	runModeProfile       = "RunMode"
	runModeBackupProfile = runModeProfile + "-Backup"
	setupModeProfile     = "SetupMode"
)

//go:embed html/index.html
var indexHTML string

func handleRoot(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(writer, indexHTML)
}

func handleNetworks(writer http.ResponseWriter, _ *http.Request) {
	networks, err := getAvailableNetworks()
	if err != nil {
		log.Printf("Failed to get available networks: %v\n", err)
		http.Error(writer, fmt.Sprintf("Error fetching networks: %v", err), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, networks)
}

type networkInfo struct {
	SSID     string `json:"ssid"`     //nolint:tagliatelle // lowercase for compatibility with JS
	Security string `json:"security"` //nolint:tagliatelle // lowercase for compatibility with JS
}

func getAvailableNetworks() (string, error) { //nolint:cyclop // complexity from network scanning logic
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return "", fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	devices, err := nm.GetDevices()
	if err != nil {
		return "", fmt.Errorf("failed to get devices: %w", err)
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
					continue
				}

				break
			}
		}
	}

	if wifiDevice == nil {
		return "", fmt.Errorf("%s device not found", wlanInterface)
	}

	// Request scan
	err = wifiDevice.RequestScan()
	if err != nil {
		log.Printf("Warning: failed to request scan: %v\n", err)
	}

	// Get access points
	accessPoints, err := wifiDevice.GetAccessPoints()
	if err != nil {
		return "", fmt.Errorf("failed to get access points: %w", err)
	}

	seen := make(map[string]bool)

	var networks []networkInfo

	for _, accessPoint := range accessPoints {
		ssid, err := accessPoint.GetPropertySSID()
		if err != nil || ssid == "" {
			continue
		}

		if !seen[ssid] {
			seen[ssid] = true

			// Determine security type from WPA and RSN flags
			security := detectSecurityType(accessPoint)

			networks = append(networks, networkInfo{
				SSID:     ssid,
				Security: security,
			})
		}
	}

	data, err := json.Marshal(networks)
	if err != nil {
		return "", fmt.Errorf("failed to marshal networks: %w", err)
	}

	return string(data), nil
}

func detectSecurityType(accessPoint gonetworkmanager.AccessPoint) string {
	wpaFlags, err := accessPoint.GetPropertyWPAFlags()
	if err != nil {
		log.Printf("Warning: failed to get WPA flags: %v\n", err)
	}

	rsnFlags, err := accessPoint.GetPropertyRSNFlags()
	if err != nil {
		log.Printf("Warning: failed to get RSN flags: %v\n", err)
	}

	// Check for WPA3 (SAE)
	if (rsnFlags & uint32(gonetworkmanager.Nm80211APSecKeyMgmtSAE)) != 0 {
		return "wpa3"
	}

	// Check for WPA2 (RSN with PSK)
	if (rsnFlags & uint32(gonetworkmanager.Nm80211APSecKeyMgmtPSK)) != 0 {
		return "wpa2"
	}

	// Check for WPA (WPA flags with PSK)
	if (wpaFlags & uint32(gonetworkmanager.Nm80211APSecKeyMgmtPSK)) != 0 {
		return "wpa"
	}

	// Check for WEP
	if (wpaFlags&uint32(gonetworkmanager.Nm80211APSecPairWEP40)) != 0 ||
		(wpaFlags&uint32(gonetworkmanager.Nm80211APSecPairWEP104)) != 0 {
		return "wep"
	}

	// No security (open network)
	if wpaFlags == 0 && rsnFlags == 0 {
		return "none"
	}

	// Default to WPA2 if we can't determine
	return "wpa2"
}

func handleSave(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ssid := request.FormValue("ssid")
	password := request.FormValue("password")
	security := request.FormValue("security")
	ipConfig := request.FormValue("ipConfig")
	ipAddress := request.FormValue("ipAddress")
	netmask := request.FormValue("netmask")
	gateway := request.FormValue("gateway")
	dns := request.FormValue("dns")

	err := saveNetworkConfiguration(request.Context(), ssid, password, security, ipConfig, ipAddress, netmask, gateway, dns)
	if err != nil {
		log.Printf("Save configuration failed: %v\n", err)
		log.Println("Network configuration failed, exiting with code 1")
		os.Exit(1)
	}

	log.Println("Network configuration completed successfully, exiting with code 0")
	os.Exit(0)
}

func main() {
	hardware.Init()

	// Create channels for shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan bool, 1)

	// Handle OS signals (Ctrl+C)
	go func() {
		sig := <-sigChan
		log.Printf("Received %v signal, shutting down without completing setup\n", sig)

		done <- true
	}()

	// Handle keyboard input
	go func() {
		_ = keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			switch key.Code { //nolint:exhaustive
			case keys.CtrlC, keys.Escape:
				log.Println("Escape key pressed, shutting down")

				done <- true

				return true, nil
			}

			return false, nil
		})
	}()

	// Start web server
	go func() {
		http.HandleFunc("/", handleRoot)
		http.HandleFunc("/networks", handleNetworks)
		http.HandleFunc("/save", handleSave)
		log.Println("Starting web server on port 80...")

		server := &http.Server{
			Addr:              ":80",
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
		}

		err := server.ListenAndServe()
		if err != nil {
			log.Printf("Web server error: %v\n", err)
		}
	}()

	langCode := "en"

	lang, err := i18n.New(&langCode, zerolog.Logger{})
	if err != nil {
		fmt.Printf("Failed to initialize i18n: %v\n", err) //nolint:forbidigo
	}

	lcd, err := pirateaudio.NewDisplay(pirateaudio.DisplayOptions{
		Orientation: 0,
		I18n:        lang,
	})
	if err != nil {
		fmt.Printf("Failed to initialize display: %v\n", err) //nolint:forbidigo
	}

	code := genQRcode()

	// display the qrcode image on the lcd
	canvas := imageToRGBA(code)
	content := &display.Content{
		Canvas: canvas,
	}

	err = lcd.Write(content)
	if err != nil {
		log.Printf("Failed to write to display: %v\n", err)
	} else {
		lcd.Wakeup()
	}

	// Wait for shutdown signal
	<-done

	log.Println("Shutting down without completing setup, exiting with code 1")

	// Clear display to black
	blackCanvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	blackContent := &display.Content{
		Canvas: blackCanvas,
	}

	err = lcd.Write(blackContent)
	if err != nil {
		log.Printf("Failed to clear display: %v\n", err)
	}

	lcd.Sleep()

	// Exit with error code since setup was not completed
	os.Exit(1)
}

// WIFI:S:<SSID>;T:<AUTH>;P:<PASSWORD>;H:<true|false|blank>;;
// S (SSID): *required* The network name (SSID) of the Wi-Fi network.
// T (authentication type): The network encryption type (WPA, WPA2, WPA3, or WEP). Leave empty for open networks with no password.
// P (password): The network password. This field is ignored if the network does not have authentication.
// H (hidden network): *optional* Set to "true" if the SSID is not broadcast.
func genQRcode() image.Image {
	for {
		networkSSID, err := getNetworkSSID()
		if err != nil {
			time.Sleep(2 * time.Second)

			continue
		}

		networkPassword := "5imtezil0"
		networkAuth := "WPA2"
		networkHidden := "false"

		networkDef := "WIFI:S:" + networkSSID + ";T:" + networkAuth + ";P:" + networkPassword + ";H:" + networkHidden + ";"

		code, err := qrcode.New(networkDef, qrcode.Medium)
		if err != nil {
			log.Printf("Failed to generate QR code: %v\n", err)

			return image.Black
		}

		code.BackgroundColor = image.Black
		code.ForegroundColor = image.White

		return code.Image(240)
	}
}

func getNetworkSSID() (string, error) {
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return "", fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	devices, err := nm.GetDevices()
	if err != nil {
		return "", fmt.Errorf("failed to get devices: %w", err)
	}

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
				wifiDevice, err := gonetworkmanager.NewDeviceWireless(device.GetPath())
				if err != nil {
					return "", fmt.Errorf("failed to create wireless device: %w", err)
				}

				activeAP, err := wifiDevice.GetPropertyActiveAccessPoint()
				if err != nil {
					return "", fmt.Errorf("failed to get active access point: %w", err)
				}

				if activeAP != nil {
					ssid, err := activeAP.GetPropertySSID()
					if err != nil {
						return "", fmt.Errorf("failed to get SSID: %w", err)
					}

					return ssid, nil
				}
			}
		}
	}

	return "", fmt.Errorf("%s device not found or not connected", wlanInterface)
}

// imageToRGBA converts an image.Image to *image.RGBA.
func imageToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}

	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}

	return rgba
}

func saveNetworkConfiguration(ctx context.Context, ssid, password, security, ipConfig, ipAddress, netmask, gateway, dns string) error { //nolint:cyclop
	// Validate static IP configuration if provided
	if ipConfig == "static" {
		err := validateIPConfiguration(ipAddress, netmask, gateway, dns)
		if err != nil {
			log.Printf("IP configuration validation failed: %v\n", err)

			return fmt.Errorf("invalid IP configuration: %w", err)
		}
	}

	// Backup existing RunMode connection before deleting
	backupRunModeConnection()

	// Delete any existing RunMode connection
	deleteRunModeConnection()

	// Build connection settings
	settings := gonetworkmanager.ConnectionSettings{
		"connection": map[string]interface{}{
			"id":             runModeProfile,
			"type":           "802-11-wireless",
			"interface-name": wlanInterface,
			"autoconnect":    true,
		},
		"802-11-wireless": map[string]interface{}{
			"ssid": []byte(ssid),
			"mode": "infrastructure",
		},
	}

	// Configure WiFi security based on security type
	if security != "none" && password != "" {
		securitySettings := map[string]interface{}{
			"psk":       password,
			"psk-flags": uint32(0), // NM_SETTING_SECRET_FLAG_NONE - store password
		}

		switch security {
		case "wpa3":
			securitySettings["key-mgmt"] = "sae"
		case "wpa2":
			securitySettings["key-mgmt"] = "wpa-psk"
		case "wpa":
			securitySettings["key-mgmt"] = "wpa-psk"
		case "wep":
			securitySettings["key-mgmt"] = "none"
			securitySettings["wep-key0"] = password
			securitySettings["wep-key-type"] = uint32(1) // NM_WEP_KEY_TYPE_PASSPHRASE
			delete(securitySettings, "psk")
			delete(securitySettings, "psk-flags")
		default:
			// Default to WPA2 for unknown types
			securitySettings["key-mgmt"] = "wpa-psk"
		}

		settings["802-11-wireless-security"] = securitySettings
	}

	// Configure IP settings
	if ipConfig == "static" {
		prefix := netmaskToCIDR(netmask)

		// Parse IP addresses
		ipAddr := net.ParseIP(ipAddress)
		gatewayAddr := net.ParseIP(gateway)

		if ipAddr == nil || gatewayAddr == nil {
			return errors.New("invalid IP address format")
		}

		// Build address data: array of [address, prefix, gateway]
		addressData := []map[string]any{
			{
				"address": ipAddress,
				"prefix":  prefix,
			},
		}

		// Parse DNS servers - convert to uint32 array (network byte order)
		dnsServers := strings.Split(dns, ",")

		var dnsAddresses []uint32

		for _, dnsServer := range dnsServers {
			dnsIP := net.ParseIP(strings.TrimSpace(dnsServer))
			if dnsIP != nil {
				// Convert IPv4 to uint32 (network byte order)
				ipv4 := dnsIP.To4()
				if ipv4 != nil {
					dnsUint32 := uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 | uint32(ipv4[2])<<8 | uint32(ipv4[3])
					dnsAddresses = append(dnsAddresses, dnsUint32)
				}
			}
		}

		settings["ipv4"] = map[string]any{
			"method":       "manual",
			"address-data": addressData,
			"gateway":      gateway,
			"dns":          dnsAddresses,
		}
	} else {
		settings["ipv4"] = map[string]any{
			"method": "auto",
		}
	}

	// Add the connection
	settingsObj, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	_, err = settingsObj.AddConnection(settings)
	if err != nil {
		return fmt.Errorf("failed to add connection: %w", err)
	}

	// Switch from SetupMode to RunMode
	err = switchToRunMode(ctx)
	if err != nil {
		return err
	}

	return nil
}

func waitForConnectionState(ctx context.Context, activeConn gonetworkmanager.ActiveConnection, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		state, err := activeConn.GetPropertyState()
		if err != nil {
			return fmt.Errorf("failed to get connection state: %w", err)
		}

		if state == gonetworkmanager.NmActiveConnectionStateActivated {
			return nil
		}

		if state >= gonetworkmanager.NmActiveConnectionStateDeactivating {
			return fmt.Errorf("connection failed or deactivated (state: %d)", state)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Continue waiting
		}
	}

	return errors.New("timeout waiting for connection to activate")
}

func verifyIPAddress(device gonetworkmanager.Device) error {
	// Get IP4Config
	ip4Config, err := device.GetPropertyIP4Config()
	if err != nil {
		return fmt.Errorf("failed to get IP4Config: %w", err)
	}

	if ip4Config == nil {
		return errors.New("no IP configuration available")
	}

	addresses, err := ip4Config.GetPropertyAddressData()
	if err != nil {
		return fmt.Errorf("failed to get addresses: %w", err)
	}

	if len(addresses) == 0 {
		return errors.New("no IP addresses assigned")
	}

	log.Printf("IP address assigned: %s/%d\n", addresses[0].Address, addresses[0].Prefix)

	return nil
}

func testConnectivity(ctx context.Context) error {
	// Try to ping the gateway first
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Get the default gateway
	nm, err := gonetworkmanager.NewNetworkManager()
	if err != nil {
		return fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	devices, err := nm.GetDevices()
	if err != nil {
		return fmt.Errorf("failed to get devices: %w", err)
	}

	var gateway string

	for _, device := range devices {
		iface, err := device.GetPropertyInterface()
		if err != nil || iface != wlanInterface {
			continue
		}

		ip4Config, err := device.GetPropertyIP4Config()
		if err != nil || ip4Config == nil {
			continue
		}

		gateway, err = ip4Config.GetPropertyGateway()
		if err == nil && gateway != "" {
			break
		}
	}

	if gateway == "" {
		// Try DNS as fallback
		gateway = "1.1.1.1"
	}

	log.Printf("Testing connectivity to %s...\n", gateway)

	// Ping test with 3 packets, 2 second timeout per packet
	cmd := "ping -c 3 -W 2 " + gateway

	_, err = runCommandWithContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("connectivity test failed: %w", err)
	}

	log.Println("Connectivity test passed")

	return nil
}

func runCommandWithContext(ctx context.Context, cmdStr string) (string, error) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", errors.New("empty command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...) //nolint:gosec
	output, err := cmd.CombinedOutput()

	return string(output), err
}

func switchToRunMode(ctx context.Context) error {
	nm, err := gonetworkmanager.NewNetworkManager() //nolint:varnamelen // not confusing
	if err != nil {
		return fmt.Errorf("failed to connect to NetworkManager: %w", err)
	}

	// Get all connections
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to list connections: %w", err)
	}

	var setupModeConn, runModeConn gonetworkmanager.Connection

	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok {
			switch id {
			case setupModeProfile:
				setupModeConn = conn
			case runModeProfile:
				runModeConn = conn
			}
		}
	}

	if runModeConn == nil {
		return fmt.Errorf("connection profile %q not found", runModeProfile)
	}

	// Deactivate SetupMode
	if setupModeConn != nil {
		activeConns, err := nm.GetPropertyActiveConnections()
		if err == nil {
			for _, activeConn := range activeConns {
				connPath, err := activeConn.GetPropertyConnection()
				if err == nil && connPath.GetPath() == setupModeConn.GetPath() {
					err = nm.DeactivateConnection(activeConn)
					if err != nil {
						log.Printf("Warning: failed to deactivate SetupMode: %v\n", err)
					}

					break
				}
			}
		}

		// Disable SetupMode autoconnect
		setupSettings, err := setupModeConn.GetSettings()
		if err == nil {
			connMap, ok := setupSettings["connection"]

			if ok {
				connMap["autoconnect"] = false

				err = setupModeConn.Update(setupSettings)
				if err != nil {
					log.Printf("Warning: failed to disable SetupMode autoconnect: %v\n", err)
				}
			}
		}
	}

	// Activate RunMode
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

	// Activate the RunMode connection
	log.Printf("Activating connection profile %q...\n", runModeProfile)

	activeConn, err := nm.ActivateConnection(runModeConn, wlanDevice, nil)
	if err != nil {
		log.Printf("Failed to activate connection profile %q: %v\n", runModeProfile, err)
		restoreRunModeConnection()

		_ = switchToSetupMode()

		return fmt.Errorf("failed to activate connection profile %q: %w", runModeProfile, err)
	}

	// Wait for connection to reach activated state (30 second timeout)
	log.Println("Waiting for connection to activate...")

	err = waitForConnectionState(ctx, activeConn, 30*time.Second)
	if err != nil {
		log.Printf("Failed to activate connection profile %q: %v\n", runModeProfile, err)
		restoreRunModeConnection()

		_ = switchToSetupMode()

		return fmt.Errorf("failed to activate connection profile %q: %w", runModeProfile, err)
	}

	log.Println("Connection activated successfully")

	// Verify IP address assignment (wait up to 15 seconds)
	log.Println("Verifying IP address assignment...")

	var ipVerified bool

	for range 30 {
		err = verifyIPAddress(wlanDevice)
		if err == nil {
			ipVerified = true

			break
		}

		select {
		case <-ctx.Done():
			restoreRunModeConnection()

			_ = switchToSetupMode()

			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Continue waiting
		}
	}

	if !ipVerified {
		log.Println("Failed to obtain IP address")
		restoreRunModeConnection()

		_ = switchToSetupMode()

		return errors.New("failed to obtain IP address")
	}

	// Test connectivity
	log.Println("Testing network connectivity...")

	err = testConnectivity(ctx)
	if err != nil {
		log.Printf("Connectivity test failed: %v\n", err)
		restoreRunModeConnection()

		_ = switchToSetupMode()

		return fmt.Errorf("connectivity test failed: %w", err)
	}

	log.Println("Network configuration verified successfully")

	// Delete the backup since the new configuration is working
	deleteRunModeBackup()

	return nil
}

func switchToSetupMode() error {
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

	var setupModeConn gonetworkmanager.Connection

	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok && id == "SetupMode" {
			setupModeConn = conn

			break
		}
	}

	if setupModeConn == nil {
		return errors.New("SetupMode connection not found")
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

	_, err = nm.ActivateConnection(setupModeConn, wlanDevice, nil)
	if err != nil {
		return fmt.Errorf("failed to bring up SetupMode connection: %w", err)
	}

	return nil
}

func backupRunModeConnection() {
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		log.Printf("Warning: failed to get settings for backup: %v\n", err)

		return
	}

	connections, err := settings.ListConnections()
	if err != nil {
		log.Printf("Warning: failed to list connections for backup: %v\n", err)

		return
	}

	// First, delete any existing backup
	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok && id == runModeBackupProfile {
			err = conn.Delete()
			if err != nil {
				log.Printf("Warning: failed to delete backup connection profile %q: %v\n", runModeBackupProfile, err)
			}

			break
		}
	}

	// Now find and backup the current RunMode connection
	for _, conn := range connections {
		connSettings, err := conn.GetSettings()
		if err != nil {
			continue
		}

		connMap, ok := connSettings["connection"]
		if !ok {
			continue
		}

		if id, ok := connMap["id"].(string); ok && id == runModeProfile {
			// Clone the settings and rename to RunMode-Backup
			connMap["id"] = runModeBackupProfile
			connMap["autoconnect"] = false // Don't auto-connect to backup

			_, err = settings.AddConnection(connSettings)
			if err != nil {
				log.Printf("Warning: failed to create backup connection profile %q: %v\n", runModeBackupProfile, err)
			} else {
				log.Printf("Existing connection profile %q backed up to %q\n", runModeProfile, runModeBackupProfile)
			}

			return
		}
	}

	log.Printf("No existing connection profile %q to backup\n", runModeProfile)
}

func restoreRunModeConnection() {
	log.Printf("Restoring connection profile %q from backup...\n", runModeProfile)

	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		log.Printf("Warning: failed to restore connection profile %q: %v\n", runModeProfile, err)

		return
	}

	connections, err := settings.ListConnections()
	if err != nil {
		log.Printf("Warning: failed to list connections for restore: %v\n", err)

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

		if id, ok := connMap["id"].(string); ok && id == runModeBackupProfile {
			backupConn = conn

			break
		}
	}

	if backupConn == nil {
		log.Println("No backup connection found to restore")

		return
	}

	// Get backup settings
	backupSettings, err := backupConn.GetSettings()
	if err != nil {
		log.Printf("Warning: failed to get backup settings: %v\n", err)

		return
	}

	// Delete current failed RunMode connection
	deleteRunModeConnection()

	// Rename backup back to RunMode
	if connMap, ok := backupSettings["connection"]; ok {
		connMap["id"] = runModeProfile
		connMap["autoconnect"] = true
	}

	// Create the restored connection
	_, err = settings.AddConnection(backupSettings)
	if err != nil {
		log.Printf("Warning: failed to restore connection profile %q: %v\n", runModeProfile, err)

		return
	}

	log.Printf("Successfully restored previous connection profile %q from backup\n", runModeProfile)

	// Delete the backup
	err = backupConn.Delete()
	if err != nil {
		log.Printf("Warning: failed to delete backup connection profile %q after restore: %v\n", runModeBackupProfile, err)
	}
}

func deleteRunModeBackup() {
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return
	}

	connections, err := settings.ListConnections()
	if err != nil {
		return
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

		if id, ok := connMap["id"].(string); ok && id == runModeBackupProfile {
			err = conn.Delete()
			if err != nil {
				log.Printf("Warning: failed to delete backup connection profile %q: %v\n", runModeBackupProfile, err)
			}

			return
		}
	}
}

func deleteRunModeConnection() {
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		log.Printf("Warning: failed to get settings: %v\n", err)

		return
	}

	connections, err := settings.ListConnections()
	if err != nil {
		log.Printf("Warning: failed to list connections: %v\n", err)

		return
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

		if id, ok := connMap["id"].(string); ok && id == runModeProfile {
			err = conn.Delete()
			if err != nil {
				log.Printf("Warning: failed to delete %s connection: %v\n", runModeProfile, err)
			}

			return
		}
	}
}

func validateIPConfiguration(ipAddress, netmask, gateway, dns string) error {
	// IP address regex pattern
	ipRegex := regexp.MustCompile(`^((25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`)

	if ipAddress == "" || !ipRegex.MatchString(ipAddress) {
		return errors.New("invalid IP address")
	}

	if netmask == "" || !ipRegex.MatchString(netmask) {
		return errors.New("invalid netmask")
	}

	if gateway == "" || !ipRegex.MatchString(gateway) {
		return errors.New("invalid gateway")
	}

	if dns == "" {
		return errors.New("DNS servers required")
	}

	// Validate DNS servers (comma-separated)
	dnsServers := strings.Split(dns, ",")
	for _, server := range dnsServers {
		server = strings.TrimSpace(server)

		if !ipRegex.MatchString(server) {
			return fmt.Errorf("invalid DNS server: %s", server)
		}
	}

	return nil
}

func netmaskToCIDR(netmask string) int {
	parts := strings.Split(netmask, ".")
	if len(parts) != 4 {
		return 24 // default
	}

	var cidr int

	for _, part := range parts {
		var octet int

		_, _ = fmt.Sscanf(part, "%d", &octet)

		for octet > 0 {
			if octet&1 == 1 {
				cidr++
			}

			octet >>= 1
		}
	}

	return cidr
}
