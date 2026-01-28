package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
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

type platform struct {
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

	platform := newPlatform(zerolog.InfoLevel, baseDir)

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

		platform.log = platform.log.Level(level).With().Logger()
	}

	if len(flag.Args()) == 1 {
		action = flag.Arg(0)
	} else {
		platform.log.Debug().Int("count", len(flag.Args())).Msg("Invalid arg count")
	}

	var exitCode exitcode.Code

	switch action {
	case "setup-disable":
		exitCode = platform.disableSetupModeFlag()
	case "setup-enable":
		exitCode = platform.enableSetupModeFlag()
	case "init":
		exitCode = platform.init()
	case "mode-run":
		exitCode = platform.enterRunMode()
	case "mode-setup":
		exitCode = platform.enterSetupMode()
	case "reset":
		exitCode = platform.reset()
	case "status":
		exitCode = platform.status()
	case "ssh-enable":
		exitCode = platform.enableSSH()
	case "ssh-disable":
		exitCode = platform.disableSSH()
	case "ssh-provision":
		exitCode = platform.provisionSSH()
	case "wifi-access":
		exitCode = platform.wifiDetails()
	case "wifi-provision":
		exitCode = platform.provisionRunModeConnection()
	case "wifi-scan":
		exitCode = platform.scanWiFi()
	case "update-apply":
		exitCode = platform.updateApply()
	case "update-rollback":
		exitCode = platform.updateRollback()
	case "version":
		exitCode = printVersion()
	case "help":
		fallthrough
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
	fmt.Fprintf(os.Stderr, "  ssh-provision     Provision SSH access\n")
	fmt.Fprintf(os.Stderr, "  status            Check current environment status\n")
	fmt.Fprintf(os.Stderr, "  update-apply      Apply a pending update (extracts, installs, swaps binaries)\n")
	fmt.Fprintf(os.Stderr, "  update-rollback   Rollback to the previous version\n")
	fmt.Fprintf(os.Stderr, "  version           Print version information\n")
	fmt.Fprintf(os.Stderr, "  wifi-access       Provide the network access details for the setup mode network\n")
	fmt.Fprintf(os.Stderr, "  wifi-provision    Provision network connection\n")
	fmt.Fprintf(os.Stderr, "  wifi-scan         Scan for available WiFi networks\n")
	fmt.Fprintf(os.Stderr, "\n  provision takes JSON on stdin with the following format:\n")
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

	return exitcode.CommandUsageErr
}

// printVersion outputs version information including version number, commit hash,
// build time, and platform to stdout.
func printVersion() exitcode.Code {
	fmt.Printf("Version: %s  Commit Hash: %s  Build Time: %s  Platform: %s\n", app.Version, app.CommitHash, app.BuildTime, app.Platform) //nolint:forbidigo // Allow for version output

	return exitcode.Success
}

// newPlatform creates and initializes a new platform instance with the specified
// log level, base directory, and default configuration for setup mode networking.
func newPlatform(logLevel zerolog.Level, baseDir string) *platform {
	mgr := platform{
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
