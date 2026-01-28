package setupmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CmdStatus represents the status information returned by the setup CLI tool.
type CmdStatus struct {
	ActiveConn       string `json:"activeConn"`
	Available        bool   `json:"available,omitempty"`
	FlagEnabled      bool   `json:"flagEnabled"`
	Ready            bool   `json:"ready"`
	RunModePresent   bool   `json:"runModePresent"`
	SetupModePresent bool   `json:"setupModePresent"`
	SetupRequired    bool   `json:"setupRequired"`
	LCDPresent       bool   `json:"lcdPresent"`
	SSHEnabled       bool   `json:"sshEnabled"`
}

// CmdNetworkInfo represents WiFi network information returned by the setup CLI tool.
type CmdNetworkInfo struct {
	SSID     string `json:"ssid"`
	PSK      string `json:"psk"`
	Security string `json:"security"`
}

type CmdResult string

const (
	ResultSuccess CmdResult = "success"
	ResultFailure CmdResult = "failure"
	ResultUnknown CmdResult = "unknown"
)

type CmdAction string

const (
	CmdActionInit          CmdAction = "init"
	CmdActionModeRun       CmdAction = "mode-run"
	CmdActionModeSetup     CmdAction = "mode-setup"
	CmdActionReset         CmdAction = "reset"
	CmdActionSetupEnable   CmdAction = "setup-enable"
	CmdActionSetupDisable  CmdAction = "setup-disable"
	CmdActionSSHEnable     CmdAction = "ssh-enable"
	CmdActionSSHDisable    CmdAction = "ssh-disable"
	CmdActionSSHProvision  CmdAction = "ssh-provision"
	CmdActionStatus        CmdAction = "status"
	CmdActionVersion       CmdAction = "version"
	CmdActionWifiAccess    CmdAction = "wifi-access"
	CmdActionWifiScan      CmdAction = "wifi-scan"
	CmdActionWifiProvision CmdAction = "wifi-provision"
)

// CmdResponse represents a response from the setup CLI tool.
// Different commands populate different optional fields.
type CmdResponse struct {
	Result   CmdResult        `json:"result"`
	Error    string           `json:"error,omitempty"`
	Action   CmdAction        `json:"action,omitempty"`
	Networks []CmdNetworkInfo `json:"networks,omitempty"`
	Status   *CmdStatus       `json:"status,omitempty"`
	WiFi     *CmdNetworkInfo  `json:"wifi,omitempty"`
}

// Status checks the current status of setup mode by running the setup command's status action.
// It returns CmdStatus and a boolean indicating whether the status command was run or not.
func (s *SetupMode) Status(ctx context.Context) (status CmdStatus) {
	defaultStatus := CmdStatus{
		Available: false,
	}

	// Check if command exists
	if s.command == "" {
		return defaultStatus
	}

	// Check if the command file exists and is executable
	_, err := exec.LookPath(s.command)
	if err != nil {
		s.log.Debug().Err(err).Str("command", s.command).Msg("Setup command not found")

		return defaultStatus
	}

	// Run the status action
	response, err := s.RunPlatformCommand(ctx, CmdActionStatus, nil)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to run status command")

		return defaultStatus
	}

	if response.Status == nil {
		s.log.Error().Msg("Status command returned no status data")

		return defaultStatus
	}

	return *response.Status
}

// RunPlatformCommand executes a platform management command with optional input and unmarshals the response.
func (s *SetupMode) RunPlatformCommand(ctx context.Context, action CmdAction, stdin []byte) (*CmdResponse, error) {
	// Apply 10-second maximum timeout as safety net, or use existing if shorter
	var (
		cmdCtx context.Context
		cancel context.CancelFunc
	)

	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < 90*time.Second {
			cmdCtx = ctx
		} else {
			cmdCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
		}
	} else {
		cmdCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	// Get the current log level and pass it to the platform command
	logLevel := s.log.GetLevel().String()
	cmd := exec.CommandContext(cmdCtx, s.command, "-l", logLevel, action.String()) //nolint:gosec // path is trusted

	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}

	exitCode := 0

	output, cmdErr := cmd.Output()
	if cmdErr != nil {
		exitErr := &exec.ExitError{}
		if errors.As(cmdErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	var response CmdResponse

	err := json.Unmarshal(output, &response)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON from setup command %s: %w", action, err)
	}

	if response.Result != ResultSuccess {
		return &response, fmt.Errorf("setup command %q failed: %s (exit code %d)", action, response.Error, exitCode)
	}

	return &response, nil
}

// String returns the string representation of the CmdAction.
func (c *CmdAction) String() string {
	return string(*c)
}
