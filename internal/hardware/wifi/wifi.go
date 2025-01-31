package wifi

import (
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
// KoalaArt:74
// DaikinAP10879:67
// Firetooth:65
// yarn:65
// DaikinAP57931:65
// yarn:61
// Firetooth:39
// ctc-jvdx0n:17
// ctc-2g-jvdx0n:14
//
// New wifi entry
// sudo nmcli dev wifi connect SSID password PASSWORD ifname wlan0
//
// Existing wifi entry
// sudo nmcli dev wifi connect yarn
// sudo nmcli dev wifi connect KoalaArt

type WifiNetwork struct {
	SSID   string
	Signal int
}

func Scan() []WifiNetwork {
	cmd := exec.Command("nmcli", "-t", "-f", "SSID,SIGNAL", "dev", "wifi", "list")
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
	cmd := exec.Command("nmcli", "dev", "wifi", "connect", ssid, "password", password)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
