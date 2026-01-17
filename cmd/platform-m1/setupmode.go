package main

import (
	"maps"
	"os"
	"slices"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
)

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
