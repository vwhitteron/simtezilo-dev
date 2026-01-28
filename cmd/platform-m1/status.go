package main

import (
	"maps"
	"os"
	"slices"
	"time"

	"github.com/Wifx/gonetworkmanager/v2"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
)

// isNetworkManagerReady checks whether the NetworkManager D-Bus service is available
// and responding to requests.
func (p *platform) isNetworkManagerReady() bool {
	_, err := gonetworkmanager.NewNetworkManager()

	return err == nil
}

// waitForNetworkManager blocks until NetworkManager becomes available or the maximum
// wait time is exceeded. Returns true if NetworkManager is ready, false otherwise.
func (p *platform) waitForNetworkManager() (ok bool) {
	const (
		maxWaitAttempts = 30
		waitInterval    = 1 * time.Second
	)

	for attempt := 1; attempt <= maxWaitAttempts; attempt++ {
		if p.isNetworkManagerReady() {
			p.log.Debug().Int("attempt", attempt).Msg("NetworkManager is ready")

			return true
		}

		if attempt == maxWaitAttempts {
			errMsg := "NetworkManager not available after waiting"
			p.log.Debug().Msg(errMsg)

			outputJSON(map[string]any{
				"error":  errMsg,
				"result": setupmode.ResultFailure,
			})

			return false
		}

		p.log.Debug().Int("attempt", attempt).Int("max_attempts", maxWaitAttempts).Msg("Waiting for NetworkManager to start")

		time.Sleep(waitInterval)
	}

	return true
}

// status checks and reports the current environment status including setup mode flag,
// network connection profiles, SSH state, and whether setup is required.
func (p *platform) status() exitcode.Code {
	status := setupmode.CmdStatus{
		Available:        true,
		ActiveConn:       "",
		FlagEnabled:      false,
		RunModePresent:   false,
		SetupModePresent: false,
		SetupRequired:    true,
		Ready:            p.isNetworkManagerReady(),
		LCDPresent:       true,
		SSHEnabled:       p.isSSHEnabled(),
	}

	// Check if setup mode flag file exists
	_, err := os.Stat(p.setupModeFlag)
	if err == nil {
		p.log.Debug().Str("flag", p.setupModeFlag).Msg("Setup mode flag file exists, setup required")

		status.FlagEnabled = true
	}

	connections, err := p.getConnections()
	if err != nil {
		errMsg := "failed to get network connections: " + err.Error()
		p.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"status": status,
		})

		return exitcode.GeneralErr
	}

	p.log.Debug().Strs("connections", slices.Collect(maps.Keys(connections))).Msg("Existing connections")

	// Find the active connection
	for name, isActive := range connections {
		if isActive {
			status.ActiveConn = name

			break
		}
	}

	if _, exists := connections[setupModeProfile]; exists {
		status.SetupModePresent = true
	}

	if _, exists := connections[runModeProfile]; exists {
		status.RunModePresent = true
	}

	status.SetupRequired = !status.SetupModePresent || !status.RunModePresent

	outputJSON(map[string]any{
		"result": setupmode.ResultSuccess,
		"status": status,
	})

	if status.SetupRequired {
		return exitcode.SetupRequired
	}

	return exitcode.Success
}
