package app

import (
	"fmt"
	"image"
	"os"
	"os/exec"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/wifi"
)

const readyFile = "/opt/simtezilo/.ready"

func isSetupComplete(log zerolog.Logger) bool {
	_, err := os.Stat(readyFile)
	if os.IsNotExist(err) {
		log.Debug().Str("file", readyFile).Str("status", "not found").Msg("setup not complete")

		return false
	} else if err != nil {
		log.Debug().Str("file", readyFile).Str("status", err.Error()).Msg("setup not complete")

		return false
	}

	return true
}

func markSetupComplete() error {
	file, err := os.Create(readyFile)
	if err != nil {
		return fmt.Errorf("creating %q: %w", readyFile, err)
	}
	file.Close()

	return nil
}

func setupRpiHardware(hw string) error {
	cmd := exec.Command("sudo", "/opt/simtezilo/setup.sh", hw)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("setting up RPI hardware: %q", output)
	}

	return nil
}

func reboot() error {
	cmd := exec.Command("sudo", "reboot")
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("rebooting: %w", err)
	}

	return nil
}

func runSetupWizard(lcdDevice hardware.LCD) error {
	// err := setupRpiHardware("waveshare")
	// if err != nil {
	// 	return fmt.Errorf("setting up RPi hardware: %w", err)
	// }

	// err = reboot()
	// if err != nil {
	// 	return fmt.Errorf("rebooting: %w", err)
	// }

	networks := wifi.Scan()

	displayString := ""
	for _, network := range networks {
		displayString += fmt.Sprintf("%s\n", network.SSID)
	}

	lcdDevice.PowerOn()
	canvas := image.NewRGBA(image.Rect(0, 0, 240, 240))
	lcdDevice.ShowTextCentered(canvas, displayString, 10)

	err := markSetupComplete()
	if err != nil {
		return fmt.Errorf("marking setup complete: %w", err)
	}

	return nil
}
