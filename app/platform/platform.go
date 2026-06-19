package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ErrHelperUnavailable is returned when a platform helper command is requested
// but no helper binary path is configured (e.g. on non-simtezilo builds).
var ErrHelperUnavailable = errors.New("platform helper not available")

// Command represents a platform management command.
type Command string

// Platform management commands.
const (
	BTAudioRoute   Command = "bt-audio-route"
	BTConnect      Command = "bt-connect"
	BTDisconnect   Command = "bt-disconnect"
	BTList         Command = "bt-list"
	BTPair         Command = "bt-pair"
	BTRemove       Command = "bt-remove"
	BTScan         Command = "bt-scan"
	BTStatus       Command = "bt-status"
	Init           Command = "init"
	ModeRun        Command = "mode-run"
	ModeSetup      Command = "mode-setup"
	Reset          Command = "reset"
	SetupEnable    Command = "setup-enable"
	SetupDisable   Command = "setup-disable"
	SSHEnable      Command = "ssh-enable"
	SSHDisable     Command = "ssh-disable"
	SSHProvision   Command = "ssh-provision"
	Status         Command = "status"
	Version        Command = "version"
	WifiAccess     Command = "wifi-access"
	WifiScan       Command = "wifi-scan"
	WifiProvision  Command = "wifi-provision"
	UpdateApply    Command = "update-apply"
	UpdateRollback Command = "update-rollback"
	SignalStart    Command = "signal-start"
)

// String returns the string representation of the Command.
func (c Command) String() string {
	return string(c)
}

// Result represents the result status from a platform command.
type Result string

// Command result statuses.
const (
	Success Result = "success"
	Failure Result = "failure"
	Unknown Result = "unknown"
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

// CmdBTAdapter represents the state of the Bluetooth adapter returned by the
// bt-status/bt-list/bt-scan commands.
type CmdBTAdapter struct {
	Present     bool   `json:"present"`
	Powered     bool   `json:"powered"`
	Discovering bool   `json:"discovering"`
	Address     string `json:"address,omitempty"`
}

// CmdBTDevice represents a single Bluetooth device as reported by the helper.
// Type is a backend-agnostic, semantic device classification (e.g. speaker,
// headphones, headset, audio) that the UI maps to an icon; it deliberately does
// not expose any backend-specific presentation hint.
type CmdBTDevice struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Paired    bool   `json:"paired"`
	Trusted   bool   `json:"trusted"`
	Connected bool   `json:"connected"`
	RSSI      int16  `json:"rssi,omitempty"`
}

// CmdResponse represents a response from the setup CLI tool.
// Different commands populate different optional fields.
type CmdResponse struct {
	Result    Result           `json:"result"`
	Error     string           `json:"error,omitempty"`
	Action    Command          `json:"action,omitempty"`
	Networks  []CmdNetworkInfo `json:"networks,omitempty"`
	Status    *CmdStatus       `json:"status,omitempty"`
	WiFi      *CmdNetworkInfo  `json:"wifi,omitempty"`
	Adapter   *CmdBTAdapter    `json:"adapter,omitempty"`
	BTDevices []CmdBTDevice    `json:"btDevices,omitempty"`
}

// RunCommand executes a platform management command with optional input and unmarshals the response.
// This is a reusable function for invoking the platform binary from any part of the application.
func RunCommand(ctx context.Context, commandPath string, log zerolog.Logger, action Command, stdin []byte) (*CmdResponse, error) {
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
	logLevel := log.GetLevel().String()
	cmd := exec.CommandContext(cmdCtx, commandPath, "-l", logLevel, action.String())

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

		// If we got no output at all, return the command error directly
		if len(output) == 0 {
			return nil, fmt.Errorf("platform command %s failed with no output: %w", action, cmdErr)
		}
	}

	var response CmdResponse

	err := json.Unmarshal(output, &response)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON from platform command %s: %w", action, err)
	}

	if response.Result != Success {
		return &response, fmt.Errorf("platform command %q failed: %s (exit code %d)", action, response.Error, exitCode)
	}

	return &response, nil
}
