package wifi

import (
	"context"
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// List all wifi networks
// nmcli dev wifi list
//
// List all wifi networks machine readable
// nmcli -t -f SSID,SIGNAL dev wifi list
// net-jvdx0n:17
// net5-jvdx0n:14
//
// New wifi entry
// sudo nmcli dev wifi connect <SSID> password <PASSWORD> ifname wlan0
//
// Existing wifi entry
// sudo nmcli dev wifi connect <SSID>

type WifiNetwork struct {
	SSID   string
	Signal int
}

func Scan() []WifiNetwork {
	cmd := exec.CommandContext(context.Background(), "nmcli", "-t", "-f", "SSID,SIGNAL", "dev", "wifi", "list")

	output, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}

	wifiNetworks := []WifiNetwork{}

	for _, line := range strings.Split(string(output), "\n") {
		if len(line) == 0 {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}

		SSID := fields[0]

		signal, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		wifiNetworks = append(wifiNetworks, WifiNetwork{SSID: SSID, Signal: signal})
	}

	return wifiNetworks
}

func Connect(ssid string, password string) error {
	cmd := exec.CommandContext(context.Background(), "nmcli", "dev", "wifi", "connect", ssid, "password", password)

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
