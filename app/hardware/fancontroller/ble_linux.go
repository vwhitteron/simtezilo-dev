//go:build linux

package fancontroller

import (
	"sync"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
)

var setupBLEDeviceOnce = sync.OnceValue(func() error { //nolint:gochecknoglobals
	device, err := linux.NewDevice()
	if err != nil {
		return err
	}

	ble.SetDefaultDevice(device)

	return nil
})

func setupDefaultBLEDevice() error {
	return setupBLEDeviceOnce()
}
