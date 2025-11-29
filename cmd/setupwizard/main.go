package main

import (
	_ "embed"

	"context"
	"errors"
	"fmt"
	"image"
	"log"
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
	"github.com/rs/zerolog"
	"github.com/skip2/go-qrcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
)

//go:embed html/index.html
var indexHTML string

func handleRoot(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(writer, indexHTML)
}

func handleNetworks(writer http.ResponseWriter, request *http.Request) {
	networks, err := getAvailableNetworks(request.Context())
	if err != nil {
		log.Printf("Failed to get available networks: %v\n", err)
		http.Error(writer, fmt.Sprintf("Error fetching networks: %v", err), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(writer, `{"networks":%s}`, networks)
}

func getAvailableNetworks(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "sudo", "nmcli", "-f", "SSID", "-t", "d", "wifi", "list", "ifname", "wlan0", "--rescan", "auto")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get networks: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	seen := make(map[string]bool)

	var networks []string

	for _, line := range lines {
		ssid := strings.TrimSpace(line)
		if ssid != "" && !seen[ssid] {
			seen[ssid] = true
			networks = append(networks, fmt.Sprintf(`"%s"`, ssid))
		}
	}

	return "[" + strings.Join(networks, ",") + "]", nil
}

func handleSave(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ssid := request.FormValue("ssid")
	password := request.FormValue("password")
	ipConfig := request.FormValue("ipConfig")
	ipAddress := request.FormValue("ipAddress")
	netmask := request.FormValue("netmask")
	gateway := request.FormValue("gateway")
	dns := request.FormValue("dns")

	err := saveNetworkConfiguration(request.Context(), ssid, password, ipConfig, ipAddress, netmask, gateway, dns)

	writer.Header().Set("Content-Type", "application/json")

	if err != nil {
		log.Printf("Save configuration failed: %v\n", err)
		writer.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(writer, `{"success":false,"error":"%s"}`, err.Error())

		return
	}

	fmt.Fprint(writer, `{"success":true}`)

	// Configuration saved successfully, trigger automatic reboot
	go func() { //nolint:contextcheck
		time.Sleep(1 * time.Second)

		ctx := context.Background()
		cmd := exec.CommandContext(ctx, "sudo", "reboot")
		_ = cmd.Run()
	}()
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
		log.Printf("Received %v signal, shutting down\n", sig)

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
		panic(err)
	}

	lcd.Wakeup()

	// Wait for shutdown signal
	<-done

	log.Println("Shutting down...")

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
}

// WIFI:S:<SSID>;T:<AUTH>;P:<PASSWORD>;H:<true|false|blank>;;
// S (SSID): *required* The network name (SSID) of the Wi-Fi network.
// T (authentication type): The network encryption type (WPA, WPA2, WPA3, or WEP). Leave empty for open networks with no password.
// P (password): The network password. This field is ignored if the network does not have authentication.
// H (hidden network): *optional* Set to "true" if the SSID is not broadcast.
func genQRcode() image.Image {
	networkSSID, err := getNetworkSSID()
	if err != nil {
		panic(err)
	}

	networkPassword := "5imtezil0"
	networkAuth := "WPA2"
	networkHidden := "false"

	networkDef := "WIFI:S:" + networkSSID + ";T:" + networkAuth + ";P:" + networkPassword + ";H:" + networkHidden + ";"

	code, err := qrcode.New(networkDef, qrcode.Medium)
	if err != nil {
		panic(err)
	}

	code.BackgroundColor = image.Black
	code.ForegroundColor = image.White

	return code.Image(240)
}

func getNetworkSSID() (string, error) {
	wlanIface := "wlan0"

	output, err := runNmcliDevShowCommand(wlanIface)
	if err != nil {
		return "", errors.New("nmcli command failed: " + err.Error())
	}

	ssid := extractSSIDFromOutput(output)
	if ssid != "" {
		return ssid, nil
	}

	return "", errors.New("SSID not found in nmcli output")
}

func runNmcliDevShowCommand(iface string) (string, error) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "nmcli", "-f", "AP", "dev", "show", iface)

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(output), nil
}

func extractSSIDFromOutput(output string) string {
	lines := strings.Split(output, "\n")

	inUseIndex := findInUseAPIndex(lines)
	if inUseIndex < 0 {
		return ""
	}

	return findSSIDFromIndex(lines, inUseIndex)
}

func findInUseAPIndex(lines []string) int {
	for index, line := range lines {
		if !strings.Contains(line, "AP[") || !strings.Contains(line, ".IN-USE:") || !strings.Contains(line, "*") {
			continue
		}

		// Extract the AP index number
		re := regexp.MustCompile(`AP\[(\d+)\]\.IN-USE:`)
		matches := re.FindStringSubmatch(line)

		if len(matches) == 2 {
			return index
		}
	}

	return -1
}

func findSSIDFromIndex(lines []string, startIndex int) string {
	maxLines := startIndex + 50
	if maxLines > len(lines) {
		maxLines = len(lines)
	}

	for index := startIndex; index < maxLines; index++ {
		line := lines[index]
		if !strings.Contains(line, ".SSID:") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) >= 2 {
			ssid := strings.TrimSpace(strings.Join(parts[1:], ":"))
			if ssid != "" {
				return ssid
			}
		}

		break
	}

	return ""
}

// GenerateNetworkSSID generates the WiFi network name (SSID) based on the device serial number.
func generateNetworkSSID() string {
	serial := getDeviceSerial()

	return "Simtezilo-" + serial
}

// getDeviceSerial reads the Raspberry Pi serial number from /proc/cpuinfo.
// Returns the last 8 characters of the serial for use in the WiFi SSID.
func getDeviceSerial() string {
	fallbackSerial := "00000000"

	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return fallbackSerial
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Serial") {
			parts := strings.Split(line, ":")

			if len(parts) == 2 {
				serial := strings.TrimSpace(parts[1])

				if len(serial) >= 8 {
					return serial[len(serial)-8:]
				}

				return serial
			}
		}
	}

	return fallbackSerial
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

func saveNetworkConfiguration(ctx context.Context, ssid, password, ipConfig, ipAddress, netmask, gateway, dns string) error { //nolint:cyclop
	// Validate static IP configuration if provided
	if ipConfig == "static" {
		err := validateIPConfiguration(ipAddress, netmask, gateway, dns)
		if err != nil {
			log.Printf("IP configuration validation failed: %v\n", err)

			return fmt.Errorf("invalid IP configuration: %w", err)
		}
	}

	// Delete any existing RunMode connection first
	deleteCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "delete", "RunMode")
	_ = deleteCmd.Run()

	// Recreate connection for permanent save
	addCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "add", "type", "wifi", "ifname", "wlan0", "con-name", "RunMode", "ssid", ssid)

	err := addCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to add connection: %w", err)
	}

	// Configure IP method
	var ipMethod string
	if ipConfig == "static" {
		ipMethod = "manual"
	} else {
		ipMethod = "auto"
	}

	modifyIPCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "modify", "RunMode", "ipv4.method", ipMethod)

	err = modifyIPCmd.Run()
	if err != nil {
		_ = exec.CommandContext(ctx, "sudo", "nmcli", "connection", "delete", "RunMode").Run()

		return fmt.Errorf("failed to set IP method: %w", err)
	}

	// Configure static IP if selected
	if ipConfig == "static" {
		prefix := netmaskToCIDR(netmask)
		addressWithPrefix := fmt.Sprintf("%s/%d", ipAddress, prefix)

		modifyAddrCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "modify", "RunMode", "ipv4.addresses", addressWithPrefix)

		err = modifyAddrCmd.Run()
		if err != nil {
			_ = exec.CommandContext(ctx, "sudo", "nmcli", "connection", "delete", "RunMode").Run()

			return fmt.Errorf("failed to set IP address: %w", err)
		}

		modifyGWCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "modify", "RunMode", "ipv4.gateway", gateway)

		err = modifyGWCmd.Run()
		if err != nil {
			_ = exec.CommandContext(ctx, "sudo", "nmcli", "connection", "delete", "RunMode").Run()

			return fmt.Errorf("failed to set gateway: %w", err)
		}

		modifyDNSCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "modify", "RunMode", "ipv4.dns", dns)

		err = modifyDNSCmd.Run()
		if err != nil {
			_ = exec.CommandContext(ctx, "sudo", "nmcli", "connection", "delete", "RunMode").Run()

			return fmt.Errorf("failed to set DNS: %w", err)
		}
	}

	// Configure WiFi security
	if password != "" {
		modifyKeyMgmtCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "modify", "RunMode", "wifi-sec.key-mgmt", "wpa-psk")

		err = modifyKeyMgmtCmd.Run()
		if err != nil {
			_ = exec.CommandContext(ctx, "sudo", "nmcli", "connection", "delete", "RunMode").Run()

			return fmt.Errorf("failed to set key management: %w", err)
		}

		modifyPSKCmd := exec.CommandContext(ctx, "sudo", "nmcli", "connection", "modify", "RunMode", "wifi-sec.psk", password)

		err = modifyPSKCmd.Run()
		if err != nil {
			_ = exec.CommandContext(ctx, "sudo", "nmcli", "connection", "delete", "RunMode").Run()

			return fmt.Errorf("failed to set password: %w", err)
		}
	}

	// Disable SetupMode autoconnect so RunMode takes precedence on reboot
	modifySetupAutoconnectCmd := exec.CommandContext(ctx, "sudo", "nmcli", "con", "modify", "SetupMode", "autoconnect", "no")

	err = modifySetupAutoconnectCmd.Run()
	if err != nil {
		log.Printf("Warning: failed to disable SetupMode autoconnect: %v\n", err)
	}

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
