package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

const (
	// loopbackModule is the ALSA loopback kernel module. snd-aloop creates a
	// "Loopback" card that PortAudio can open as a normal hw device; an alsaloop
	// bridge forwards it to the bluealsa speaker. This is the Linux shim that
	// makes a BT speaker behave like a native output device (as it already does
	// on macOS), keeping the app's PortAudio path platform-agnostic.
	loopbackModule  = "snd-aloop"
	modulesLoadFile = "/etc/modules-load.d/snd-aloop.conf"

	// alsaConfDir is the system ALSA drop-in directory, included by alsa-lib's
	// global config; per-device bluealsa PCMs are written here so they compose
	// with any hand-written /etc/asound.conf.
	alsaConfDir = "/etc/alsa/conf.d"

	// bridgeUnitPath is the templated systemd unit that runs one alsaloop bridge
	// per connected device (instance = sanitized MAC).
	bridgeUnitPath = "/etc/systemd/system/simtezilo-btbridge@.service"

	// loopbackCapturePCM is the capture side of the loopback the bridge reads
	// from; the app plays to device 0, which appears as capture on device 1.
	// alsaloop auto-detects the format/rate from the open playback side (the app's
	// persistent pit-radio sink, FLOAT_LE), so the bridge must NOT pin them.
	loopbackCapturePCM = "hw:Loopback,1,0"

	// btBridgeLatencyUs is alsaloop's target latency. Bluetooth A2DP needs a
	// large buffer (~200 ms); the default (~40 ms) underruns continuously.
	btBridgeLatencyUs = 200000

	// btBridgeSyncMode is alsaloop's clock-sync mode. 2 = captshift: rate-shift
	// the snd-aloop capture to track the Bluetooth playback clock. Without it the
	// two free-running clocks drift and produce periodic pops.
	btBridgeSyncMode = 2
)

// macRE validates a colon-separated MAC address. The address is interpolated
// into file paths, a systemd instance name and an ALSA config, so it must be
// strictly validated to avoid injection.
var macRE = regexp.MustCompile(`^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$`)

// btRouteRequest is the stdin payload for bt-audio-route.
type btRouteRequest struct {
	Address string `json:"address"`
	Enable  bool   `json:"enable"`
}

// btReadRouteRequest reads and validates the {address, enable} JSON payload.
func btReadRouteRequest() (btRouteRequest, error) {
	var req btRouteRequest

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return req, fmt.Errorf("read stdin: %w", err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return req, errNoAddress
	}

	err = json.Unmarshal(data, &req)
	if err != nil {
		return req, fmt.Errorf("invalid JSON: %w", err)
	}

	if !macRE.MatchString(strings.TrimSpace(req.Address)) {
		return req, errNoAddress
	}

	return req, nil
}

// btAudioRoute brings the snd-aloop → bluealsa bridge for a device up or down.
// enable=true ensures the loopback module, a plug-wrapped bluealsa PCM and the
// bridge unit exist, then (re)starts the bridge; enable=false stops it.
func (p *manager) btAudioRoute() exitcode.Code {
	req, err := btReadRouteRequest()
	if err != nil {
		return btFailure(p, err.Error(), err)
	}

	colonMAC := strings.ToUpper(strings.TrimSpace(req.Address))
	instance := strings.ReplaceAll(colonMAC, ":", "_") // systemd instance + PCM suffix
	pcmName := "bt_" + instance

	if !req.Enable {
		err := p.stopBridge(instance)
		if err != nil {
			return btFailure(p, "stop Bluetooth audio bridge", err)
		}

		outputJSON(map[string]any{"result": platform.Success, "address": colonMAC})

		return exitcode.Success
	}

	err = p.ensureLoopbackModule()
	if err != nil {
		return btFailure(p, "load snd-aloop module", err)
	}

	err = p.writeBluealsaPCM(pcmName, colonMAC)
	if err != nil {
		return btFailure(p, "write bluealsa PCM config", err)
	}

	changedUnit, err := p.ensureBridgeUnit()
	if err != nil {
		return btFailure(p, "install Bluetooth audio bridge unit", err)
	}

	// Restart only when the unit changed (first bring-up); otherwise start is a
	// no-op gap-free idempotent call.
	err = p.startBridge(instance, changedUnit)
	if err != nil {
		return btFailure(p, "start Bluetooth audio bridge", err)
	}

	outputJSON(map[string]any{"result": platform.Success, "address": colonMAC})

	return exitcode.Success
}

// ensureLoopbackModule loads snd-aloop now and persists it for boot. modprobe is
// a no-op when the module is already loaded.
func (p *manager) ensureLoopbackModule() error {
	_, err := writeFileIfChanged(modulesLoadFile, []byte(loopbackModule+"\n"), 0o644)
	if err != nil {
		return err
	}

	out, err := exec.CommandContext(context.Background(), "modprobe", loopbackModule).CombinedOutput()
	if err != nil {
		return fmt.Errorf("modprobe %s: %w: %s", loopbackModule, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// writeBluealsaPCM writes a plug-wrapped bluealsa PCM for the device into the
// ALSA drop-in dir. The plug wrapper resamples the app's rate to the SBC rate.
func (p *manager) writeBluealsaPCM(pcmName, colonMAC string) error {
	err := os.MkdirAll(alsaConfDir, 0o755)
	if err != nil {
		return fmt.Errorf("mkdir %s: %w", alsaConfDir, err)
	}

	conf := fmt.Sprintf(`pcm.%s {
    type plug
    slave.pcm {
        type bluealsa
        device "%s"
        profile "a2dp"
    }
    hint { show on; description "Bluetooth Speaker" }
}
`, pcmName, colonMAC)

	path := filepath.Join(alsaConfDir, pcmName+".conf")

	_, err = writeFileIfChanged(path, []byte(conf), 0o644)
	if err != nil {
		return err
	}

	return nil
}

// ensureBridgeUnit writes the templated alsaloop bridge unit (reloading systemd
// only when it changes) and reports whether it changed. %i is the sanitized MAC,
// so the unit plays to bt_%i (the PCM written above). The format/rate are NOT
// pinned: alsaloop auto-detects them from the open playback side (the app's
// persistent pit-radio sink), which is the only side that can set the loopback
// params. StartLimitIntervalSec=0 disables systemd's start-rate limiting so a
// bridge that briefly can't open keeps retrying instead of giving up.
func (p *manager) ensureBridgeUnit() (bool, error) {
	unit := fmt.Sprintf(`[Unit]
Description=Simtezilo Bluetooth audio bridge for %%i
After=bluealsa.service sound.target
Requires=bluealsa.service
StartLimitIntervalSec=0

[Service]
ExecStart=/usr/bin/alsaloop -C %s -P bt_%%i -t %d -S %d
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
`, loopbackCapturePCM, btBridgeLatencyUs, btBridgeSyncMode)

	changed, err := writeFileIfChanged(bridgeUnitPath, []byte(unit), 0o644)
	if err != nil {
		return false, err
	}

	// Only reload when the unit actually changed; the reconciler calls this
	// repeatedly and a reload on every tick is needless churn.
	if changed {
		out, err := exec.CommandContext(context.Background(), "systemctl", "daemon-reload").CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	return changed, nil
}

// startBridge ensures the bridge unit for an instance is running. It first clears
// any prior failed/start-limit state so a corrected config can come up. When
// restart is true (rate/unit changed) it restarts to pick up the new params;
// otherwise it uses start, which is a no-op on an already-active unit so the
// reconciler is gap-free.
func (p *manager) startBridge(instance string, restart bool) error {
	unit := "simtezilo-btbridge@" + instance + ".service"

	// Best-effort: reset-failed is a no-op on a healthy/active unit.
	_ = exec.CommandContext(context.Background(), "systemctl", "reset-failed", unit).Run()

	verb := "start"
	if restart {
		verb = "restart"
	}

	out, err := exec.CommandContext(context.Background(), "systemctl", verb, unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", verb, unit, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// writeFileIfChanged writes content to path only when it differs from the
// current contents, reporting whether a write occurred. This keeps the
// reconciler from rewriting config/units (and triggering reloads) every tick.
func writeFileIfChanged(path string, content []byte, perm os.FileMode) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return false, nil
	}

	err = os.WriteFile(path, content, perm)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}

	return true, nil
}

// stopBridge stops the bridge unit for an instance. systemctl stop on an
// inactive unit is a no-op success.
func (p *manager) stopBridge(instance string) error {
	unit := "simtezilo-btbridge@" + instance + ".service"

	out, err := exec.CommandContext(context.Background(), "systemctl", "stop", unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
	}

	return nil
}
