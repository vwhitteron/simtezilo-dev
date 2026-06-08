package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

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
)

// runPlatform invokes the platform helper with the given action and optional
// stdin payload, returning the decoded response. It reuses the same helper
// gateway the WebUI already uses for SSH/reset/wifi. It returns an error when
// the helper is not available (non-simtezilo builds).
func (w *WebUI) runPlatform(ctx context.Context, action platform.Command, stdin []byte) (*platform.CmdResponse, error) {
	if w.setupMode == nil || !w.setupMode.IsAvailable() {
		return nil, platform.ErrHelperUnavailable
	}

	return w.setupMode.PlatformAction(ctx, action, stdin)
}

// bluetoothAvailable reports whether Bluetooth management is available, gating
// purely on the presence of the platform helper — the same gate used for SSH,
// setup and wifi. When no adapter is present a scan simply returns an empty
// list, so there is no need to probe the hardware here.
func (w *WebUI) bluetoothAvailable() bool {
	return w.setupMode != nil && w.setupMode.IsAvailable()
}

// handleBluetoothDevices lists Bluetooth devices. With ?scan=true it triggers a
// discovery scan first; otherwise it returns the paired/connected devices only.
func (w *WebUI) handleBluetoothDevices(response http.ResponseWriter, request *http.Request) {
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

	resp, err := w.runPlatform(ctx, action, nil)
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
		"available": w.bluetoothAvailable(),
		"adapter":   resp.Adapter,
		"devices":   resp.BTDevices,
	})
}

// handleBluetoothAction performs a connect/disconnect/pair/remove on a device.
func (w *WebUI) handleBluetoothAction(response http.ResponseWriter, request *http.Request) {
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
		w.writeBluetoothError(response, http.StatusBadRequest, "Invalid request body")

		return
	}

	if reqData.Address == "" {
		w.writeBluetoothError(response, http.StatusBadRequest, "Missing device address")

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
		w.writeBluetoothError(response, http.StatusBadRequest, "Unknown action")

		return
	}

	payload, _ := json.Marshal(map[string]string{"address": reqData.Address}) //nolint:errchkjson // simple encoding

	ctx, cancel := context.WithTimeout(request.Context(), bluetoothCmdTimeout)
	defer cancel()

	_, err = w.runPlatform(ctx, action, payload)
	if err != nil {
		w.writeBluetoothError(response, http.StatusInternalServerError, err.Error())

		return
	}

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Bluetooth " + reqData.Action + " succeeded",
	})
}

// writeBluetoothError writes a JSON error response with the given status code.
func (w *WebUI) writeBluetoothError(response http.ResponseWriter, code int, message string) {
	response.WriteHeader(code)
	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "error",
		"message": message,
	})
}
