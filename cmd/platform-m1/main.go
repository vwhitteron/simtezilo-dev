package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

type (
	systemctlCmd string
)

const (
	wlanInterface    = "wlan0"
	runModeProfile   = "RunMode"
	setupModeProfile = "SetupMode"
	setupModeFlag    = "/boot/firmware/simtezilo/SETUPMODE"
	sshUser          = "admin"

	sysctlEnable    systemctlCmd = "enable"
	sysctlDisable   systemctlCmd = "disable"
	sysctlStart     systemctlCmd = "start"
	sysctlStop      systemctlCmd = "stop"
	sysctlIsActive  systemctlCmd = "is-active"
	sysctlIsEnabled systemctlCmd = "is-enabled"
)

type networkConfig struct {
	name        string
	autoconnect bool
	ssid        string
	mode        string
	band        string
	psk         string
	security    string
	method      string
	ipAddr      string
	prefix      string
	gateway     string
	dns         []string
}

type manager struct {
	log              zerolog.Logger
	wlanInterface    string
	setupModeConfig  networkConfig
	runModeProfile   string
	setupModeProfile string
	setupModeFlag    string
	baseDir          string
}

// main is the entry point for the platform management command. It parses command-line
// arguments and dispatches to the appropriate subcommand handler.
func main() { //nolint:cyclop // easy enough to understand
	var (
		action   string
		baseDir  string
		help     bool
		logLevel string
		version  bool
	)

	flag.StringVar(&baseDir, "b", "/opt/simtezilo", "Base directory for Simtezilo installation")
	flag.BoolVar(&help, "h", false, "Show help message")
	flag.StringVar(&logLevel, "l", "info", "Log level. Default is 'info'")
	flag.BoolVar(&version, "v", false, "Print version information")
	flag.Parse()

	platformManager := newManager(zerolog.InfoLevel, baseDir)

	if version {
		action = "version"
	}

	if help {
		action = "help"
	}

	if logLevel != "" {
		level, err := zerolog.ParseLevel(logLevel)
		if err != nil {
			level = zerolog.InfoLevel
		}

		platformManager.log = platformManager.log.Level(level).With().Logger()
	}

	switch len(flag.Args()) {
	case 1, 2:
		// One arg is a plain command; two args is "help <command>".
		action = flag.Arg(0)
	default:
		platformManager.log.Debug().Int("count", len(flag.Args())).Msg("Invalid arg count")
	}

	var exitCode exitcode.Code

	switch action {
	case platform.SetupDisable.String():
		exitCode = platformManager.disableSetupModeFlag()
	case platform.SetupEnable.String():
		exitCode = platformManager.enableSetupModeFlag()
	case platform.Init.String():
		exitCode = platformManager.init()
	case platform.ModeRun.String():
		exitCode = platformManager.enterRunMode()
	case platform.ModeSetup.String():
		exitCode = platformManager.enterSetupMode()
	case platform.Reset.String():
		exitCode = platformManager.reset()
	case platform.Status.String():
		exitCode = platformManager.status()
	case platform.SSHEnable.String():
		exitCode = platformManager.enableSSH()
	case platform.SSHDisable.String():
		exitCode = platformManager.disableSSH()
	case platform.SSHProvision.String():
		exitCode = platformManager.provisionSSH()
	case platform.WifiAccess.String():
		exitCode = platformManager.wifiDetails()
	case platform.WifiProvision.String():
		exitCode = platformManager.provisionRunModeConnection()
	case platform.WifiScan.String():
		exitCode = platformManager.scanWiFi()
	case platform.UpdateApply.String():
		exitCode = platformManager.updateApply()
	case platform.UpdateRollback.String():
		exitCode = platformManager.updateRollback()
	case platform.SignalStart.String():
		exitCode = platformManager.signalStartupSuccess()
	case platform.BTStatus.String():
		exitCode = platformManager.btStatus()
	case platform.BTList.String():
		exitCode = platformManager.btList()
	case platform.BTScan.String():
		exitCode = platformManager.btScan()
	case platform.BTConnect.String():
		exitCode = platformManager.btConnect()
	case platform.BTDisconnect.String():
		exitCode = platformManager.btDisconnect()
	case platform.BTPair.String():
		exitCode = platformManager.btPair()
	case platform.BTRemove.String():
		exitCode = platformManager.btRemove()
	case platform.BTAudioRoute.String():
		exitCode = platformManager.btAudioRoute()
	case "version":
		exitCode = printVersion()
	case "help":
		if len(flag.Args()) > 1 {
			exitCode = printCommandHelp(flag.Arg(1))
		} else {
			exitCode = printUsage()
		}
	default:
		exitCode = printUsage()
	}

	os.Exit(int(exitCode))
}

// printUsage outputs the command-line usage information to stderr and returns
// an error exit code indicating incorrect command usage.
func printUsage() exitcode.Code {
	fmt.Fprintf(os.Stderr, "Usage: %s <command>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\nOptions:\n")
	fmt.Fprintf(os.Stderr, "  -b <dir>          Base directory for installation (default: /opt/simtezilo)\n")
	fmt.Fprintf(os.Stderr, "  -h                Show this help message\n")
	fmt.Fprintf(os.Stderr, "  -l <level>        Set log level (debug, info, warn, error)\n")
	fmt.Fprintf(os.Stderr, "  -v                Show version information\n")
	fmt.Fprintf(os.Stderr, "\nCommands:\n")
	fmt.Fprintf(os.Stderr, "  init              Initialize setup mode connection if not present\n")
	fmt.Fprintf(os.Stderr, "  mode-run          Enter run mode\n")
	fmt.Fprintf(os.Stderr, "  mode-setup        Enter setup mode\n")
	fmt.Fprintf(os.Stderr, "  reset             Delete all connections and reinitialize setup mode\n")
	fmt.Fprintf(os.Stderr, "  setup-disable     Disable setup mode flag\n")
	fmt.Fprintf(os.Stderr, "  setup-enable      Enable setup mode flag\n")
	fmt.Fprintf(os.Stderr, "  ssh-enable        Enable SSH service\n")
	fmt.Fprintf(os.Stderr, "  ssh-disable       Disable SSH service\n")
	fmt.Fprintf(os.Stderr, "  ssh-provision     Provision SSH access [stdin]\n")
	fmt.Fprintf(os.Stderr, "  status            Check current environment status\n")
	fmt.Fprintf(os.Stderr, "  update-apply      Apply a pending update (extracts, installs, swaps binaries)\n")
	fmt.Fprintf(os.Stderr, "  update-rollback   Rollback to the previous version\n")
	fmt.Fprintf(os.Stderr, "  version           Print version information\n")
	fmt.Fprintf(os.Stderr, "  wifi-access       Provide the network access details for the setup mode network\n")
	fmt.Fprintf(os.Stderr, "  wifi-provision    Provision network connection [stdin]\n")
	fmt.Fprintf(os.Stderr, "  wifi-scan         Scan for available WiFi networks\n")
	fmt.Fprintf(os.Stderr, "  signal-start      Signal successful startup\n")
	fmt.Fprintf(os.Stderr, "  bt-status         Report Bluetooth adapter presence and power state\n")
	fmt.Fprintf(os.Stderr, "  bt-list           List paired and connected Bluetooth devices\n")
	fmt.Fprintf(os.Stderr, "  bt-scan           Scan for nearby Bluetooth devices\n")
	fmt.Fprintf(os.Stderr, "  bt-connect        Connect a paired Bluetooth device [stdin]\n")
	fmt.Fprintf(os.Stderr, "  bt-disconnect     Disconnect a Bluetooth device [stdin]\n")
	fmt.Fprintf(os.Stderr, "  bt-pair           Pair, trust and connect a Bluetooth device [stdin]\n")
	fmt.Fprintf(os.Stderr, "  bt-remove         Forget a Bluetooth device [stdin]\n")
	fmt.Fprintf(os.Stderr, "  bt-audio-route    Bring the Bluetooth audio bridge up/down [stdin]\n")
	fmt.Fprintf(os.Stderr, "\n  [stdin] reads a payload on stdin. Run '%s help <command>' for the format.\n", os.Args[0])

	return exitcode.CommandUsageErr
}

// printCommandHelp prints detailed usage for a single command, including the
// payload format for commands that read from stdin. Commands without a stdin
// payload fall back to the general usage output.
func printCommandHelp(command string) exitcode.Code {
	switch command {
	case platform.SSHProvision.String():
		fmt.Fprintf(os.Stderr, "Usage: %s ssh-provision\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Provision SSH access for the '%s' user.\n", sshUser)
		fmt.Fprintf(os.Stderr, "Reads a single SSH public key (OpenSSH authorized_keys format) on stdin:\n\n")
		fmt.Fprintf(os.Stderr, "  ssh-ed25519 AAAAC3Nz...key... user@host\n")
	case platform.WifiProvision.String():
		fmt.Fprintf(os.Stderr, "Usage: %s wifi-provision\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Provision the run mode WiFi connection.\n")
		fmt.Fprintf(os.Stderr, "Reads JSON on stdin with the following format:\n\n")
		fmt.Fprintf(os.Stderr, "  [{\n")
		fmt.Fprintf(os.Stderr, `    "ssid":"<string>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "psk":"<string>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "security":"<wpa2|wpa3>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "method":"<dhcp|static>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "ip":"<address>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "prefix":"<bits>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "gateway":"<address>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "dns":"<address>"`+"\n")
		fmt.Fprintf(os.Stderr, "  }]\n")
	case platform.BTConnect.String(), platform.BTDisconnect.String(),
		platform.BTPair.String(), platform.BTRemove.String():
		fmt.Fprintf(os.Stderr, "Usage: %s %s\n\n", os.Args[0], command)
		fmt.Fprintf(os.Stderr, "Reads JSON on stdin identifying the target device by address:\n\n")
		fmt.Fprintf(os.Stderr, "  {\n")
		fmt.Fprintf(os.Stderr, `    "address":"<AA:BB:CC:DD:EE:FF>"`+"\n")
		fmt.Fprintf(os.Stderr, "  }\n")
	case platform.BTAudioRoute.String():
		fmt.Fprintf(os.Stderr, "Usage: %s %s\n\n", os.Args[0], command)
		fmt.Fprintf(os.Stderr, "Brings the snd-aloop→bluealsa audio bridge for a device up or down.\n")
		fmt.Fprintf(os.Stderr, "Reads JSON on stdin:\n\n")
		fmt.Fprintf(os.Stderr, "  {\n")
		fmt.Fprintf(os.Stderr, `    "address":"<AA:BB:CC:DD:EE:FF>",`+"\n")
		fmt.Fprintf(os.Stderr, `    "enable":true`+"\n")
		fmt.Fprintf(os.Stderr, "  }\n")
	default:
		fmt.Fprintf(os.Stderr, "No additional help available for '%s'.\n\n", command)

		return printUsage()
	}

	return exitcode.Success
}

// printVersion outputs version information including version number, commit hash,
// build time, and platform to stdout.
func printVersion() exitcode.Code {
	fmt.Printf("Version: %s  Commit Hash: %s  Build Time: %s  Platform: %s\n", app.Version, app.CommitHash, app.BuildTime, app.Platform) //nolint:forbidigo // Allow for version output

	return exitcode.Success
}

// newManager creates and initializes a newplatform  manager instance with the specified
// log level, base directory, and default configuration for setup mode networking.
func newManager(logLevel zerolog.Level, baseDir string) *manager {
	mgr := manager{
		log:              zerolog.New(os.Stderr).With().Timestamp().Logger().Level(logLevel),
		wlanInterface:    wlanInterface,
		runModeProfile:   runModeProfile,
		setupModeProfile: setupModeProfile,
		setupModeFlag:    setupModeFlag,
		baseDir:          baseDir,
		setupModeConfig: networkConfig{
			name:        setupModeProfile,
			autoconnect: false,
			method:      "static",
			ipAddr:      "10.33.0.1",
			prefix:      "24",
			mode:        "ap",
			band:        "bg",
			psk:         "5imtezil0",
			security:    securityWPA2,
		},
	}

	mgr.setupModeConfig.ssid = "Simtezilo-" + mgr.getSerial()

	return &mgr
}
