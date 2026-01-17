package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
	"golang.org/x/crypto/ssh"
)

// isSSHEnabled checks whether the SSH service is both enabled and currently active.
func (m *manager) isSSHEnabled() bool {
	enabled, _ := m.controlSystemd("ssh.service", sysctlIsEnabled)
	active, _ := m.controlSystemd("ssh.service", sysctlIsActive)

	return enabled == "enabled" && active == "active"
}

// enableSSH enables and starts the SSH service using systemctl.
func (m *manager) enableSSH() exitcode.Code {
	return m.controlSSH([]systemctlCmd{sysctlEnable, sysctlStart})
}

// disableSSH stops and disables the SSH service using systemctl.
func (m *manager) disableSSH() exitcode.Code {
	return m.controlSSH([]systemctlCmd{sysctlStop, sysctlDisable})
}

// controlSSH executes a sequence of systemctl actions on the SSH service.
func (m *manager) controlSSH(actions []systemctlCmd) exitcode.Code {
	for _, action := range actions {
		_, err := m.controlSystemd("ssh.service", sysctlStart)
		if err != nil {
			m.log.Debug().Err(err).Msgf("failed to %s sshd service", action)

			outputJSON(map[string]any{
				"result": setupmode.ResultFailure,
				"error":  fmt.Errorf("failed to %s sshd service", action),
			})

			return exitcode.GeneralErr
		}
	}

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}

// provisionSSH reads an SSH public key from stdin and installs it to the admin
// user's authorized_keys file, creating the .ssh directory if needed.
func (m *manager) provisionSSH() exitcode.Code {
	// Read public key from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		errMsg := "failed to read SSH key from stdin"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	sshKey := strings.TrimSpace(string(data))
	if sshKey == "" {
		errMsg := "no SSH key provided"
		m.log.Debug().Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	// Validate the SSH public key
	_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(sshKey)) //nolint:dogsled // validation only
	if err != nil {
		errMsg := "invalid SSH public key format"
		m.log.Debug().Err(err).Msg(errMsg)

		outputJSON(map[string]any{
			"error":  errMsg,
			"result": setupmode.ResultFailure,
		})

		return exitcode.DataFormatErr
	}

	// Ensure the .ssh directory exists
	sshDir := "/home/" + sshUser + "/.ssh"

	err = os.MkdirAll(sshDir, 0o700)
	if err != nil {
		m.log.Debug().
			Err(err).
			Str("path", sshDir).
			Msg("Create .ssh directory")

		outputJSON(map[string]any{
			"error":  "failed to create .ssh directory",
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	// Write the authorized_keys file
	authorizedKeysPath := "/home/" + sshUser + "/.ssh/authorized_keys"

	err = os.WriteFile(authorizedKeysPath, []byte(sshKey+"\n"), 0o600)
	if err != nil {
		m.log.Debug().
			Err(err).
			Str("path", authorizedKeysPath).
			Msg("Write authorized_keys file")

		outputJSON(map[string]any{
			"error":  "failed to write authorized_keys file",
			"result": setupmode.ResultFailure,
		})

		return exitcode.GeneralErr
	}

	m.log.Debug().Str("path", authorizedKeysPath).Msg("SSH key provisioned successfully")

	outputJSON(map[string]any{"result": setupmode.ResultSuccess})

	return exitcode.Success
}
