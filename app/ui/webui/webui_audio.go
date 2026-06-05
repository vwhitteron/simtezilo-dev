package webui

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

// testToneDuration is how long a device/channel test tone plays.
const testToneDuration = 1500 * time.Millisecond

// applyHapticsOutputConfig applies updates to the haptic output stream
// configuration. The incoming map mirrors the "output" object of the haptics
// section of the config JSON.
func (w *WebUI) applyHapticsOutputConfig(config map[string]any) []string {
	var errors []string

	changed := false

	if device, ok := config["device"]; ok {
		if value, ok := device.(string); ok {
			changed = changed || value != w.config.GetAudioHapticsDevice()
			w.config.SetAudioHapticsDevice(value)
		} else {
			errors = append(errors, "invalid haptics device value")
		}
	}

	if deviceName, ok := config["deviceName"]; ok {
		if value, ok := deviceName.(string); ok {
			// The name is metadata for device resolution; a device change is saved
			// alongside it and already triggers the restart, so don't restart again.
			w.config.SetAudioHapticsDeviceName(value)
		} else {
			errors = append(errors, "invalid haptics device name value")
		}
	}

	if channels, ok := config["channels"]; ok {
		if value, ok := channels.(float64); ok {
			changed = changed || int(value) != w.config.GetAudioHapticsChannels()
			w.config.SetAudioHapticsChannels(int(value))
		} else {
			errors = append(errors, "invalid haptics channels value")
		}
	}

	if sampleRate, ok := config["sampleRate"]; ok {
		if value, ok := sampleRate.(float64); ok {
			changed = changed || int(value) != w.config.GetAudioHapticsSampleRate()
			w.config.SetAudioHapticsSampleRate(int(value))
		} else {
			errors = append(errors, "invalid haptics sample rate value")
		}
	}

	if latencyMs, ok := config["latencyMs"]; ok {
		if value, ok := latencyMs.(float64); ok {
			changed = changed || int(value) != w.config.GetAudioHapticsLatencyMs()
			w.config.SetAudioHapticsLatencyMs(int(value))
		} else {
			errors = append(errors, "invalid haptics latency value")
		}
	}

	// Restart the live haptic stream only when an output value actually changed,
	// so unrelated settings saves don't glitch playback.
	if changed && w.onHapticsOutputChanged != nil {
		w.onHapticsOutputChanged()
	}

	return errors
}

// applyPitRadioAudioConfig applies updates to the local pit-radio audio device
// configuration. The incoming map mirrors the "audio" object of the pitRadio
// section of the config JSON.
func (w *WebUI) applyPitRadioAudioConfig(config map[string]any) []string {
	var errors []string

	if device, ok := config["device"]; ok {
		if value, ok := device.(string); ok {
			w.config.SetAudioPitRadioDevice(value)
		} else {
			errors = append(errors, "invalid pit-radio device value")
		}
	}

	if deviceName, ok := config["deviceName"]; ok {
		if value, ok := deviceName.(string); ok {
			w.config.SetAudioPitRadioDeviceName(value)
		} else {
			errors = append(errors, "invalid pit-radio device name value")
		}
	}

	if sampleRate, ok := config["sampleRate"]; ok {
		if value, ok := sampleRate.(float64); ok {
			w.config.SetAudioPitRadioSampleRate(int(value))
		} else {
			errors = append(errors, "invalid pit-radio sample rate value")
		}
	}

	return errors
}

// handleAudioDevices enumerates the output devices for a backend. The backend
// may be given as a "backend" query parameter; otherwise the configured backend
// is used.
func (w *WebUI) handleAudioDevices(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	backendName := request.URL.Query().Get("backend")
	if backendName == "" {
		backendName = w.config.GetAudioBackend()
	}

	backend, err := audio.New(backendName, w.log)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":            "error",
			"message":           err.Error(),
			"availableBackends": audio.AvailableBackends(),
		})

		return
	}

	defer func() { _ = backend.Close() }()

	devices, err := backend.ListDevices()
	if err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": err.Error(),
		})

		return
	}

	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"status":            "success",
		"backend":           backendName,
		"availableBackends": audio.AvailableBackends(),
		"devices":           devices,
	})
}

// handleAudioTest plays a short test tone on a device/channel so the user can
// verify wiring. The beep backend drives a single global device shared with the
// haptic output, so test tones require the malgo or portaudio backend.
func (w *WebUI) handleAudioTest(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	var reqData struct {
		Backend    string  `json:"backend"`
		Device     string  `json:"device"`
		Channel    int     `json:"channel"`
		Channels   int     `json:"channels"`
		SampleRate int     `json:"sampleRate"`
		Frequency  float64 `json:"frequency"`
	}

	reqData.Channel = -1

	if err := json.NewDecoder(request.Body).Decode(&reqData); err != nil {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Invalid request body",
		})

		return
	}

	backendName := reqData.Backend
	if backendName == "" {
		backendName = w.config.GetAudioBackend()
	}

	if backendName == audio.BackendBeep {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Test tones require the malgo or portaudio backend (beep shares one device with haptics)",
		})

		return
	}

	if reqData.Channels <= 0 {
		reqData.Channels = w.config.GetAudioHapticsChannels()
	}

	if reqData.SampleRate <= 0 {
		reqData.SampleRate = w.config.GetAudioHapticsSampleRate()
	}

	backend, err := audio.New(backendName, w.log)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": err.Error(),
		})

		return
	}

	defer func() { _ = backend.Close() }()

	cfg := audio.SinkConfig{
		DeviceID:   reqData.Device,
		Channels:   reqData.Channels,
		SampleRate: reqData.SampleRate,
	}

	if err := audio.PlayTestTone(backend, cfg, reqData.Channel, reqData.Frequency, testToneDuration); err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": err.Error(),
		})

		return
	}

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Test tone played",
	})
}
