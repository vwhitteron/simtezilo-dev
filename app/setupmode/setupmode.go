package setupmode

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"mime"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/rs/zerolog"
	"github.com/skip2/go-qrcode"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
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

//go:embed static/*
var staticFiles embed.FS

func handleStaticFiles(writer http.ResponseWriter, request *http.Request, logger *zerolog.Logger) {
	filename := "static" + request.URL.Path

	content, err := staticFiles.ReadFile(filename)
	if err != nil {
		writer.WriteHeader(http.StatusNotFound)
		logger.Error().Err(err).Str("file", filename).Msg("Static file not found")

		return
	}

	contentType := getContentType(filename)
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Cache-Control", "public, max-age=31536000")

	length, err := writer.Write(content)
	if err != nil {
		logger.Error().Err(err).Int("bytes_written", length).Msg("Error writing static file")

		return
	}

	logger.Debug().Str("file", filename).Str("mime-type", contentType).Msg("Served static file")
}

func getContentType(filename string) string {
	ext := filepath.Ext(filename)
	contentType := mime.TypeByExtension(ext)

	if contentType == "" {
		return "application/octet-stream"
	}

	return contentType
}

func handleAPIGetLanguages(writer http.ResponseWriter, _ *http.Request, logger *zerolog.Logger) {
	// Create i18n instance to get available languages
	langCode := "en"

	i18nInstance, err := i18n.New(&langCode, *logger)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create i18n instance")
		http.Error(writer, fmt.Sprintf("Error fetching languages: %v", err), http.StatusInternalServerError)

		return
	}

	languagesMap := i18nInstance.Languages()

	// Build response as array of language objects
	type languageInfo struct {
		Code string `json:"code"` //nolint:tagliatelle
		Name string `json:"name"` //nolint:tagliatelle
	}

	languages := make([]languageInfo, 0, len(languagesMap))
	for code, metadata := range languagesMap {
		languages = append(languages, languageInfo{
			Code: code,
			Name: metadata.Name,
		})
	}

	data, err := json.Marshal(languages)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to marshal languages")
		http.Error(writer, fmt.Sprintf("Error encoding languages: %v", err), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "public, max-age=3600")

	length, err := writer.Write(data)
	if err != nil {
		logger.Error().Err(err).Int("bytes_written", length).Msg("Error writing languages response")

		return
	}

	logger.Debug().Int("count", len(languages)).Msg("Served languages list")
}

func handleAPIGetI18n(writer http.ResponseWriter, request *http.Request, logger *zerolog.Logger) {
	// Get language from query parameter, default to English
	lang := request.URL.Query().Get("lang")
	if lang == "" {
		lang = "en"
	}

	// Create i18n instance to get translations from languagedb
	i18nInstance, err := i18n.New(&lang, *logger)
	if err != nil {
		logger.Error().Err(err).Str("lang", lang).Msg("Failed to create i18n instance")
		http.Error(writer, fmt.Sprintf("Error loading language: %v", err), http.StatusInternalServerError)

		return
	}

	// Get all translations with the "setupmode." prefix
	translations := i18nInstance.GetStringsWithPrefix("setupmode.")

	data, err := json.Marshal(translations)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to marshal translations")
		http.Error(writer, fmt.Sprintf("Error encoding translations: %v", err), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "public, max-age=3600")

	length, err := writer.Write(data)
	if err != nil {
		logger.Error().Err(err).Int("bytes_written", length).Msg("Error writing i18n response")

		return
	}

	logger.Debug().Str("language", lang).Msg("Served i18n translations")
}

func handleAPIGetNetworks(writer http.ResponseWriter, _ *http.Request, logger *zerolog.Logger) {
	networks, err := getAvailableNetworks(logger)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get available networks")
		http.Error(writer, fmt.Sprintf("Error fetching networks: %v", err), http.StatusInternalServerError)

		return
	}

	// TODO: Remove this hardcoded test data
	// networks := `[
	// 	{"ssid": "Simtezilo-Setup", "security": "wpa2"},
	// 	{"ssid": "ExampleNetwork1", "security": "wpa2"},
	// 	{"ssid": "ExampleNetwork2", "security": "wep"},
	// 	{"ssid": "OpenNetwork",     "security": "none"}
	// ]`

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, networks)
}

type networkInfo struct {
	SSID     string `json:"ssid"`     //nolint:tagliatelle // lowercase for compatibility with JS
	Security string `json:"security"` //nolint:tagliatelle // lowercase for compatibility with JS
}

func getAvailableNetworks(logger *zerolog.Logger) (string, error) { //nolint:cyclop // complexity from network scanning logic
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
		logger.Warn().Err(err).Msg("Failed to request scan")
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
			security := detectSecurityType(accessPoint, logger)

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

func detectSecurityType(accessPoint gonetworkmanager.AccessPoint, logger *zerolog.Logger) string {
	wpaFlags, err := accessPoint.GetPropertyWPAFlags()
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to get WPA flags")
	}

	rsnFlags, err := accessPoint.GetPropertyRSNFlags()
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to get RSN flags")
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

func handleAPIConfigSave(writer http.ResponseWriter, request *http.Request, done chan<- exitcode.ExitCode, shutdown chan struct{}, logger *zerolog.Logger) {
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

	err := saveNetworkConfiguration(request.Context(), ssid, password, security, ipConfig, ipAddress, netmask, gateway, dns, logger)
	if err != nil {
		logger.Error().Err(err).Msg("Save configuration failed")

		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"success":false,"error":"Failed to save configuration: %v"}`, err)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, `{"success":true,"message":"Configuration saved successfully"}`)

	logger.Info().Int("exitCode", int(exitcode.Success)).Msg("Network configuration completed successfully, sending exit code")

	go func() {
		time.Sleep(100 * time.Millisecond)

		done <- exitcode.Success

		close(shutdown)
	}()
}

func handleModeRun(writer http.ResponseWriter, _ *http.Request, done chan<- exitcode.ExitCode, shutdown chan struct{}, logger *zerolog.Logger) {
	logger.Info().Msg("User cancelled setup, returning to run mode without saving")

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, `{"success":true,"message":"Returning to run mode"}`)

	logger.Info().Int("exitCode", int(exitcode.Success)).Msg("Setup cancelled by user, sending success exit code")

	go func() {
		time.Sleep(100 * time.Millisecond)

		done <- exitcode.Success

		close(shutdown)
	}()
}

// Run starts the setup wizard for configuring WiFi network.
func Run(done chan<- exitcode.ExitCode, logger *zerolog.Logger) {
	hardware.Init()

	// Create a local channel to signal when we should exit
	shutdown := make(chan struct{})

	// Handle keyboard input
	go func() {
		_ = keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			switch key.Code { //nolint:exhaustive
			case keys.CtrlC, keys.Escape:
				logger.Info().Msg("Escape key pressed, shutting down")

				done <- exitcode.GeneralFailure

				close(shutdown)

				return true, nil
			}

			return false, nil
		})
	}()

	// Start web server
	go func() {
		http.HandleFunc("/", handleRoot)
		http.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
			handleStaticFiles(w, r, logger)
		})
		http.HandleFunc("/css/", func(w http.ResponseWriter, r *http.Request) {
			handleStaticFiles(w, r, logger)
		})
		http.HandleFunc("/js/", func(w http.ResponseWriter, r *http.Request) {
			handleStaticFiles(w, r, logger)
		})
		http.HandleFunc("/api/languages", func(w http.ResponseWriter, r *http.Request) {
			handleAPIGetLanguages(w, r, logger)
		})
		http.HandleFunc("/api/i18n", func(w http.ResponseWriter, r *http.Request) {
			handleAPIGetI18n(w, r, logger)
		})
		http.HandleFunc("/api/getnetworks", func(w http.ResponseWriter, r *http.Request) {
			handleAPIGetNetworks(w, r, logger)
		})
		http.HandleFunc("/api/config/save", func(w http.ResponseWriter, r *http.Request) {
			handleAPIConfigSave(w, r, done, shutdown, logger)
		})
		http.HandleFunc("/api/mode/run", func(w http.ResponseWriter, r *http.Request) {
			handleModeRun(w, r, done, shutdown, logger)
		})
		logger.Info().Msg("Starting web server on port 80...")

		server := &http.Server{
			Addr:              ":80",
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
		}

		err := server.ListenAndServe()
		if err != nil {
			logger.Error().Err(err).Msg("Web server error")
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

	code := genQRcode(logger)

	// display the qrcode image on the lcd
	canvas := imageToRGBA(code)
	content := &display.Content{
		Canvas: canvas,
	}

	err = lcd.Write(content)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to write to display")
	} else {
		lcd.Wakeup()
	}

	// Wait for shutdown signal
	// This will be triggered by either successful configuration (handleSave)
	// or keyboard interrupt, or signal from main
	<-shutdown
}

// WIFI:S:<SSID>;T:<AUTH>;P:<PASSWORD>;H:<true|false|blank>;;
// S (SSID): *required* The network name (SSID) of the Wi-Fi network.
// T (authentication type): The network encryption type (WPA, WPA2, WPA3, or WEP). Leave empty for open networks with no password.
// P (password): The network password. This field is ignored if the network does not have authentication.
// H (hidden network): *optional* Set to "true" if the SSID is not broadcast.
func genQRcode(logger *zerolog.Logger) image.Image {
	for {
		networkSSID, err := getNetworkSSID(logger)
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
			logger.Error().Err(err).Msg("Failed to generate QR code")

			return image.Black
		}

		code.BackgroundColor = image.Black
		code.ForegroundColor = image.White

		return code.Image(240)
	}
}

func getNetworkSSID(logger *zerolog.Logger) (string, error) {
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

func saveNetworkConfiguration(ctx context.Context, ssid, password, security, ipConfig, ipAddress, netmask, gateway, dns string, logger *zerolog.Logger) error { //nolint:cyclop
	// Validate static IP configuration if provided
	if ipConfig == "static" {
		err := validateIPConfiguration(ipAddress, netmask, gateway, dns)
		if err != nil {
			logger.Error().Err(err).Msg("IP configuration validation failed")

			return fmt.Errorf("invalid IP configuration: %w", err)
		}
	}

	// Backup existing RunMode connection before deleting
	// err := backupRunModeConnection(logger)
	// if err != nil {
	// 	return fmt.Errorf("backup existing connection: %w", err)
	// }

	// Delete any existing RunMode connection
	err := deleteRunModeConnection(logger)
	if err != nil {
		return fmt.Errorf("delete existing connection: %w", err)
	}

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
		return fmt.Errorf("get network settings: %w", err)
	}

	_, err = settingsObj.AddConnection(settings)
	if err != nil {
		return fmt.Errorf("add connection: %w", err)
	}

	// Switch from SetupMode to RunMode
	err = switchToRunMode(ctx, logger)
	if err != nil {
		return fmt.Errorf("switch to run mode: %w", err)
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

func verifyIPAddress(device gonetworkmanager.Device, logger *zerolog.Logger) error {
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

	logger.Info().Str("address", addresses[0].Address).Uint8("prefix", addresses[0].Prefix).Msg("IP address assigned")

	return nil
}

func testConnectivity(ctx context.Context, logger *zerolog.Logger) error {
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

	logger.Info().Str("gateway", gateway).Msg("Testing connectivity")

	// Ping test with 3 packets, 2 second timeout per packet
	cmd := "ping -c 3 -W 2 " + gateway

	_, err = runCommandWithContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("connectivity test failed: %w", err)
	}

	logger.Info().Msg("Connectivity test passed")

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

func switchToRunMode(ctx context.Context, logger *zerolog.Logger) error {
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
						logger.Warn().Err(err).Msg("Failed to deactivate SetupMode")
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
					logger.Warn().Err(err).Msg("Failed to disable SetupMode autoconnect")
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
	logger.Info().Str("profile", runModeProfile).Msg("Activating connection profile")

	activeConn, err := nm.ActivateConnection(runModeConn, wlanDevice, nil)
	if err != nil {
		logger.Error().Err(err).Str("profile", runModeProfile).Msg("Failed to activate connection profile")
		restoreRunModeConnection(logger)

		_ = switchToSetupMode(logger)

		return fmt.Errorf("failed to activate connection profile %q: %w", runModeProfile, err)
	}

	// Wait for connection to reach activated state (30 second timeout)
	logger.Info().Msg("Waiting for connection to activate")

	err = waitForConnectionState(ctx, activeConn, 30*time.Second)
	if err != nil {
		logger.Error().Err(err).Str("profile", runModeProfile).Msg("Failed to activate connection profile")
		restoreRunModeConnection(logger)

		_ = switchToSetupMode(logger)

		return fmt.Errorf("failed to activate connection profile %q: %w", runModeProfile, err)
	}

	logger.Info().Msg("Connection activated successfully")

	// Verify IP address assignment (wait up to 15 seconds)
	logger.Info().Msg("Verifying IP address assignment")

	var ipVerified bool

	for range 30 {
		err = verifyIPAddress(wlanDevice, logger)
		if err == nil {
			ipVerified = true

			break
		}

		select {
		case <-ctx.Done():
			restoreRunModeConnection(logger)

			_ = switchToSetupMode(logger)

			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			// Continue waiting
		}
	}

	if !ipVerified {
		logger.Error().Msg("Failed to obtain IP address")
		restoreRunModeConnection(logger)

		_ = switchToSetupMode(logger)

		return errors.New("failed to obtain IP address")
	}

	// Test connectivity
	logger.Info().Msg("Testing network connectivity")

	err = testConnectivity(ctx, logger)
	if err != nil {
		logger.Error().Err(err).Msg("Connectivity test failed")
		restoreRunModeConnection(logger)

		_ = switchToSetupMode(logger)

		return fmt.Errorf("connectivity test failed: %w", err)
	}

	logger.Info().Msg("Network configuration verified successfully")

	// Delete the backup since the new configuration is working
	deleteRunModeBackup()

	return nil
}

func switchToSetupMode(logger *zerolog.Logger) error {
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

func backupRunModeConnection(logger *zerolog.Logger) error {
	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		return fmt.Errorf("failed to get settings for backup: %w", err)
	}

	connections, err := settings.ListConnections()
	if err != nil {
		return fmt.Errorf("failed to list connections for backup: %w", err)
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
				return fmt.Errorf("failed to delete existing backup connection profile: %w", err)
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
				return fmt.Errorf("failed to create backup connection profile: %w", err)
			}

			logger.Info().Str("from", runModeProfile).Str("to", runModeBackupProfile).Msg("Connection profile backed up")

			return nil
		}
	}

	logger.Info().Str("profile", runModeProfile).Msg("No existing connection profile to backup")

	return nil
}

func restoreRunModeConnection(logger *zerolog.Logger) {
	logger.Info().Str("profile", runModeProfile).Msg("Restoring connection profile from backup")

	settings, err := gonetworkmanager.NewSettings()
	if err != nil {
		logger.Warn().Err(err).Str("profile", runModeProfile).Msg("Failed to restore connection profile")

		return
	}

	connections, err := settings.ListConnections()
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to list connections for restore")

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
		logger.Info().Msg("No backup connection found to restore")

		return
	}

	// Get backup settings
	backupSettings, err := backupConn.GetSettings()
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to get backup settings")

		return
	}

	// Delete current failed RunMode connection
	deleteRunModeConnection(logger)

	// Rename backup back to RunMode
	if connMap, ok := backupSettings["connection"]; ok {
		connMap["id"] = runModeProfile
		connMap["autoconnect"] = true
	}

	// Create the restored connection
	_, err = settings.AddConnection(backupSettings)
	if err != nil {
		logger.Warn().Err(err).Str("profile", runModeProfile).Msg("Failed to restore connection profile")

		return
	}

	logger.Info().Str("profile", runModeProfile).Msg("Successfully restored connection profile from backup")

	// Delete the backup
	err = backupConn.Delete()
	if err != nil {
		logger.Warn().Err(err).Str("profile", runModeBackupProfile).Msg("Failed to delete backup connection profile after restore")
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
				// Silent failure - deleteRunModeBackup has no logger parameter
			}

			return
		}
	}
}

func deleteRunModeConnection(logger *zerolog.Logger) error {
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

		if id, ok := connMap["id"].(string); ok && id == runModeProfile {
			err = conn.Delete()
			if err != nil {
				return fmt.Errorf("failed to delete connection profile: %w", err)
			}

			logger.Info().Str("profile", runModeProfile).Msg("Connection profile deleted")

			return nil
		}
	}

	logger.Info().Str("profile", runModeProfile).Msg("No existing connection profile to delete")

	return nil
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
