//go:build !linux && !darwin

package fancontroller

import (
	"errors"
)

func setupDefaultBLEDevice() error {
	return errors.New("windsim BLE is only supported on linux in this build")
}
