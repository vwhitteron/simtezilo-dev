package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

// testToneDuration is how long a device/channel test tone plays.
const testToneDuration = 1500 * time.Millisecond

// applyHapticsOutputConfig applies updates to the haptic output stream
// configuration. The incoming map mirrors the "output" object of the haptics
// section of the config JSON.
func (h *configHandler) applyHapticsOutputConfig(config map[string]any) []string {
	// Detect a genuine output change before applying, so unrelated settings saves
	// don't glitch playback by restarting the live stream.
	changed := h.hapticsOutputChanged(config)

	var errors []string

	errors = appendErr(errors, applyField(config, "device", "invalid haptics device value", h.config.SetAudioHapticsDevice))
	// The name is metadata for device resolution; a device change is saved
	// alongside it and already triggers the restart, so it isn't change-tracked.
	errors = appendErr(errors, applyField(config, "deviceName", "invalid haptics device name value", h.config.SetAudioHapticsDeviceName))
	errors = appendErr(errors, applyField(config, "channels", "invalid haptics channels value", func(f float64) {
		h.config.SetAudioHapticsChannels(int(f))
	}))
	errors = appendErr(errors, applyField(config, "sampleRate", "invalid haptics sample rate value", func(f float64) {
		h.config.SetAudioHapticsSampleRate(int(f))
	}))
	errors = appendErr(errors, applyField(config, "latencyMs", "invalid haptics latency value", func(f float64) {
		h.config.SetAudioHapticsLatencyMs(int(f))
	}))

	if changed && h.onHapticsOutputChanged != nil {
		h.onHapticsOutputChanged()
	}

	return errors
}

// hapticsOutputChanged reports whether the patch alters any haptic output value
// that requires restarting the live stream. deviceName is metadata only and is
// deliberately excluded from the comparison.
func (h *configHandler) hapticsOutputChanged(config map[string]any) bool {
	if v, ok := config["device"].(string); ok && v != h.config.GetAudioHapticsDevice() {
		return true
	}

	if v, ok := config["channels"].(float64); ok && int(v) != h.config.GetAudioHapticsChannels() {
		return true
	}

	if v, ok := config["sampleRate"].(float64); ok && int(v) != h.config.GetAudioHapticsSampleRate() {
		return true
	}

	if v, ok := config["latencyMs"].(float64); ok && int(v) != h.config.GetAudioHapticsLatencyMs() {
		return true
	}

	return false
}

// applyPitRadioAudioConfig applies updates to the local pit-radio audio device
// configuration. The incoming map mirrors the "audio" object of the pitRadio
// section of the config JSON.
func (h *configHandler) applyPitRadioAudioConfig(config map[string]any) []string {
	var errors []string

	if device, ok := config["device"]; ok {
		if value, ok := device.(string); ok {
			h.config.SetAudioPitRadioDevice(value)
		} else {
			errors = append(errors, "invalid pit-radio device value")
		}
	}

	if deviceName, ok := config["deviceName"]; ok {
		if value, ok := deviceName.(string); ok {
			h.config.SetAudioPitRadioDeviceName(value)
		} else {
			errors = append(errors, "invalid pit-radio device name value")
		}
	}

	if sampleRate, ok := config["sampleRate"]; ok {
		if value, ok := sampleRate.(float64); ok {
			h.config.SetAudioPitRadioSampleRate(int(value))
		} else {
			errors = append(errors, "invalid pit-radio sample rate value")
		}
	}

	if volume, ok := config["volume"]; ok {
		if value, ok := volume.(float64); ok {
			h.config.SetAudioPitRadioVolume(int(value))
		} else {
			errors = append(errors, "invalid pit-radio volume value")
		}
	}

	return errors
}

// handleAudioDevices enumerates the output devices for a backend. The backend
// may be given as a "backend" query parameter; otherwise the configured backend
// is used.
func (h *configHandler) handleAudioDevices(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	backendName := request.URL.Query().Get("backend")
	if backendName == "" {
		backendName = h.config.GetAudioBackend()
	}

	backend, err := audio.New(backendName, h.log)
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

	h.enrichBluetoothNames(request.Context(), devices)

	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"status":            "success",
		"backend":           backendName,
		"availableBackends": audio.AvailableBackends(),
		"devices":           devices,
	})
}

// enrichBluetoothNames replaces the MAC-derived label of each bluealsa output
// with the device's friendly Bluetooth alias, cross-referencing the platform
// helper's paired/connected list by address. Best-effort and in place: when the
// helper is unavailable or a device isn't matched, the existing DisplayName is
// left untouched. DisplayName is presentation-only, so this never affects the
// stable Name used for selection/resolution.
func (h *configHandler) enrichBluetoothNames(ctx context.Context, devices []audio.Device) {
	if h.btDevices == nil {
		return
	}

	hasBT := false

	for i := range devices {
		if devices[i].Type == audio.DeviceBluetooth {
			hasBT = true

			break
		}
	}

	if !hasBT {
		return
	}

	aliasByAddr := map[string]string{}

	// connectedAlias is the name of the first connected device, used to label the
	// snd-aloop Loopback output (which carries no MAC in its PortAudio name but is
	// the bridge to the connected speaker).
	connectedAlias := ""

	for _, dev := range h.btDevices(ctx) {
		aliasByAddr[strings.ToUpper(dev.Address)] = dev.Name

		if dev.Connected && connectedAlias == "" {
			connectedAlias = dev.Name
		}
	}

	for idx := range devices {
		if devices[idx].Type != audio.DeviceBluetooth {
			continue
		}

		// A bluealsa PCM carries its MAC; the Loopback bridge does not.
		if addr := audio.BTAddress(devices[idx].Name); addr != "" {
			if alias, ok := aliasByAddr[addr]; ok && alias != "" {
				devices[idx].DisplayName = alias
			}

			continue
		}

		if connectedAlias != "" {
			devices[idx].DisplayName = connectedAlias
		}
	}
}

// handleAudioTest plays a short test tone on a device/channel so the user can
// verify wiring. The beep backend drives a single global device shared with the
// haptic output, so test tones require the portaudio backend.
func (h *configHandler) handleAudioTest(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	var reqData struct {
		Target     string  `json:"target"`
		Backend    string  `json:"backend"`
		Device     string  `json:"device"`
		Channel    int     `json:"channel"`
		Channels   int     `json:"channels"`
		SampleRate int     `json:"sampleRate"`
		Frequency  float64 `json:"frequency"`
	}

	reqData.Channel = -1

	err := json.NewDecoder(request.Body).Decode(&reqData)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Invalid request body",
		})

		return
	}

	// The haptics identify tone plays through the live synthesizer pipeline (via
	// the calibrator) so it uses the configured per-channel + master gain and EQ,
	// rather than opening a second, gain-agnostic sink on the device.
	if reqData.Target == "haptics" {
		h.playHapticsIdentifyTone(response, reqData.Channel, reqData.Frequency)

		return
	}

	// The pit-radio test speaks a short announcement through the live pit-radio
	// output, so it exercises exactly the same TTS + device path (including the
	// Bluetooth bridge) as real notifications rather than opening a one-off sink.
	if h.sendPitRadioTest == nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Pit radio audio output is not enabled",
		})

		return
	}

	err = h.sendPitRadioTest()
	if err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": err.Error(),
		})

		return
	}

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Test announcement sent",
	})
}

// playHapticsIdentifyTone plays a short tone on a single haptic output channel by
// driving the calibrator through the live synthesizer pipeline. The tone inherits
// the channel's configured gain and EQ, so it reflects how that channel is
// actually driven. It blocks for the tone duration, then restores calibrator state.
func (h *configHandler) playHapticsIdentifyTone(response http.ResponseWriter, channel int, freq float64) {
	if h.calibrator == nil {
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Calibrator unavailable",
		})

		return
	}

	if freq <= 0 {
		freq = audio.DefaultTestToneHz
	}

	// Preserve the user's calibration frequency so an identify tone does not
	// clobber it.
	prevFreq := h.calibrator.GetFrequency()

	h.calibrator.SetTargetChannel(channel)
	h.calibrator.SetFrequency(freq)
	h.calibrator.SetEnabled(true)

	time.Sleep(testToneDuration)

	// Initiate the zero-crossing stop, give it a moment to land on zero, then
	// restore the calibrator to its prior (all-channel) state.
	h.calibrator.SetEnabled(false)
	time.Sleep(150 * time.Millisecond)
	h.calibrator.SetTargetChannel(-1)
	h.calibrator.SetFrequency(prevFreq)

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Test tone played",
	})
}
