package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
)

const (
	// bluetoothScanTimeout bounds a bt-scan invocation. It must exceed the
	// helper's internal discovery window (~8s). A deadline under 90s keeps
	// platform.RunCommand from clamping to its 10s default.
	bluetoothScanTimeout = 25 * time.Second
	// bluetoothCmdTimeout bounds the quick Bluetooth commands (status, list,
	// connect, disconnect, remove).
	bluetoothCmdTimeout = 15 * time.Second
	// bluetoothPairTimeout bounds a bt-pair invocation. Pairing is asynchronous
	// in BlueZ and can take well over the quick-command budget; a too-short
	// deadline kills the helper mid-pair while bluetoothd carries on, so a later
	// retry reports a spurious "In Progress". Kept under 90s so
	// platform.RunCommand does not clamp it to its 10s default.
	bluetoothPairTimeout = 60 * time.Second
)

// runPlatform invokes the platform helper with the given action and optional
// stdin payload, returning the decoded response. It reuses the same helper
// gateway the WebUI already uses for SSH/reset/wifi. It returns an error when
// the helper is not available (non-simtezilo builds).
func (h *systemHandler) runPlatform(ctx context.Context, action platform.Command, stdin []byte) (*platform.CmdResponse, error) {
	if h.setupMode == nil || !h.setupMode.IsAvailable() {
		return nil, platform.ErrHelperUnavailable
	}

	return h.setupMode.PlatformAction(ctx, action, stdin)
}

// bluetoothAvailable reports whether Bluetooth management is available, gating
// purely on the presence of the platform helper — the same gate used for SSH,
// setup and wifi. When no adapter is present a scan simply returns an empty
// list, so there is no need to probe the hardware here.
func (h *systemHandler) bluetoothAvailable() bool {
	return h.setupMode != nil && h.setupMode.IsAvailable()
}

// btDeviceList returns the current paired/connected Bluetooth devices via the
// platform helper, used to cross-reference friendly names into the audio device
// list. Best-effort: returns nil when the helper is unavailable or errors, so
// callers degrade to the raw (MAC-derived) name.
func (h *systemHandler) btDeviceList(ctx context.Context) []platform.CmdBTDevice {
	if !h.bluetoothAvailable() {
		return nil
	}

	resp, err := h.runPlatform(ctx, platform.BTList, nil)
	if err != nil || resp == nil {
		return nil
	}

	return resp.BTDevices
}

// handleBluetoothDevices lists Bluetooth devices. With ?scan=true it triggers a
// discovery scan first; otherwise it returns the paired/connected devices only.
func (h *systemHandler) handleBluetoothDevices(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	scan := request.URL.Query().Get("scan") == "true"

	action := platform.BTList
	timeout := bluetoothCmdTimeout

	if scan {
		action = platform.BTScan
		timeout = bluetoothScanTimeout
	}

	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()

	resp, err := h.runPlatform(ctx, action, nil)
	if err != nil {
		// Degrade gracefully: report unavailable rather than erroring so the UI
		// can simply hide the Bluetooth section.
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":    "success",
			"available": false,
			"devices":   []platform.CmdBTDevice{},
		})

		return
	}

	// Gate the panel on helper availability (consistent with /api/config and the
	// LCD), not on adapter presence: when no adapter is present the device list
	// is simply empty. The adapter object is still returned for the status line.
	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"status":    "success",
		"available": h.bluetoothAvailable(),
		"adapter":   resp.Adapter,
		"devices":   resp.BTDevices,
	})
}

// handleBluetoothAction performs a connect/disconnect/pair/remove on a device.
func (h *systemHandler) handleBluetoothAction(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	var reqData struct {
		Action  string `json:"action"`
		Address string `json:"address"`
	}

	err := json.NewDecoder(request.Body).Decode(&reqData)
	if err != nil {
		h.writeBluetoothError(response, http.StatusBadRequest, "Invalid request body")

		return
	}

	if reqData.Address == "" {
		h.writeBluetoothError(response, http.StatusBadRequest, "Missing device address")

		return
	}

	var action platform.Command

	switch reqData.Action {
	case "connect":
		action = platform.BTConnect
	case "disconnect":
		action = platform.BTDisconnect
	case "pair":
		action = platform.BTPair
	case "remove":
		action = platform.BTRemove
	default:
		h.writeBluetoothError(response, http.StatusBadRequest, "Unknown action")

		return
	}

	payload, _ := json.Marshal(map[string]string{"address": reqData.Address}) //nolint:errchkjson // simple encoding

	// Pairing needs a longer budget than the quick commands (see the constant).
	timeout := bluetoothCmdTimeout
	if action == platform.BTPair {
		timeout = bluetoothPairTimeout
	}

	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()

	resp, err := h.runPlatform(ctx, action, payload)
	if err != nil {
		// The raw helper error (e.g. "platform command bt-connect failed with no
		// output: signal: killed") is useful for diagnosis but too noisy for the
		// UI. Log the detail and surface a concise message: the helper's own
		// structured error when it provided one, else a generic fallback.
		h.log.Debug().
			Err(err).
			Str("action", reqData.Action).
			Str("address", reqData.Address).
			Msg("bluetooth action failed")

		message := "the device did not respond"
		if resp != nil && resp.Error != "" {
			message = resp.Error
		}

		h.writeBluetoothError(response, http.StatusInternalServerError, message)

		return
	}

	// The audio bridge is reconciled by the app (driven by the pit-radio sink
	// lifecycle + a periodic reconcile), so connect/disconnect here doesn't touch
	// it directly.

	// Forgetting a device removes its bluealsa PCM: drop any saved pit-radio
	// selection that pointed at it, so the app stops trying to open a device that
	// no longer exists (which otherwise spams sink-open errors on every startup
	// until the setting is changed by hand).
	if action == platform.BTRemove {
		h.clearRemovedBluetoothDevice(reqData.Address)
	}

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Bluetooth " + reqData.Action + " succeeded",
	})
}

// clearRemovedBluetoothDevice clears the saved pit-radio audio selection when it
// references the just-forgotten device, matched by the Bluetooth MAC embedded in
// the stored bluealsa device name/ID. This keeps a stale selection from driving a
// doomed sink-open on the next start. Best-effort: persisted only when the
// selection actually pointed at the removed device. Selections that carry no MAC
// (e.g. the snd-aloop "Bluetooth" bridge, whose device survives the unpair) are
// left untouched.
func (h *systemHandler) clearRemovedBluetoothDevice(address string) {
	target := strings.ToUpper(address)
	if target == "" {
		return
	}

	name := h.config.GetAudioPitRadioDeviceName()
	id := h.config.GetAudioPitRadioDevice()

	if audio.BTAddress(name) != target && audio.BTAddress(id) != target {
		return
	}

	h.config.SetAudioPitRadioDevice("")
	h.config.SetAudioPitRadioDeviceName("")

	if err := h.config.SaveConfigToFile(); err != nil {
		h.log.Warn().Err(err).Msg("failed to save config after clearing removed bluetooth device")

		return
	}

	h.log.Info().Str("address", target).Msg("cleared pit-radio selection for forgotten bluetooth device")
}

// writeBluetoothError writes a JSON error response with the given status code.
func (h *systemHandler) writeBluetoothError(response http.ResponseWriter, code int, message string) {
	response.WriteHeader(code)
	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "error",
		"message": message,
	})
}
