//go:build darwin

package fancontroller

import (
	"sync"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/darwin"
)

var setupBLEDeviceOnce = sync.OnceValue(func() error { //nolint:gochecknoglobals
	device, err := darwin.NewDevice()
	if err != nil {
		return err
	}

	ble.SetDefaultDevice(device)

	return nil
})

func setupDefaultBLEDevice() error {
	return setupBLEDeviceOnce()
}
