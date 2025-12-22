// Package webui implements a simple web server to serve a web-based user interface
package webui

import (
	"embed"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/logstore"
)

// WebUI defines the web user interface.
type WebUI struct {
	log                zerolog.Logger
	port               int
	webSocketClients   int
	telemetryChartFeed chan map[string]float32
	config             *config.Config
	upgrader           websocket.Upgrader
	shutdownChan       chan exitcode.Code
	setupModeEnabled   bool
	logStore           *logstore.Store
}

type Config struct {
	Log                zerolog.Logger
	Port               int
	TelemetryChartFeed chan map[string]float32
	Config             *config.Config
	ShutdownChan       chan exitcode.Code
	SetupModeAvailable bool
	LogStore           *logstore.Store
}

// New creates a new instance of the WebUI.
func New(config Config) *WebUI {
	return &WebUI{
		log:                config.Log.With().Str("component", "web ui").Logger(),
		port:               config.Port,
		webSocketClients:   0,
		telemetryChartFeed: config.TelemetryChartFeed,
		config:             config.Config,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		shutdownChan:     config.ShutdownChan,
		setupModeEnabled: config.SetupModeAvailable, logStore: config.LogStore,
	}
}

// GetHTTPHandler returns the HTTP handler for the web UI.
func (w *WebUI) GetHTTPHandler() http.Handler {
	// Create a new ServeMux with all routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", w.htmlRouterHandlerFunc())
	mux.HandleFunc("/css/", w.cssHandlerFunc())
	mux.HandleFunc("/images/", w.imagesHandlerFunc())
	mux.HandleFunc("/js/", w.sciChartJSHandlerFunc())
	mux.HandleFunc("/ws", w.handleWebSocketConnection)
	mux.HandleFunc("/api/config", w.handleConfigAPI)
	mux.HandleFunc("/api/config/reset", w.handleConfigReset)
	mux.HandleFunc("/api/i18n", w.handleI18nAPI)
	mux.HandleFunc("/api/languages", w.handleLanguagesAPI)
	mux.HandleFunc("/api/logs", w.handleLogsAPI)

	if w.setupModeEnabled {
		mux.HandleFunc("/api/restart", w.handleRestart)
		mux.HandleFunc("/api/mode/setup", w.handleSetupMode)
		mux.HandleFunc("/api/factory-reset", w.handleFactoryReset)
	}

	w.log.Debug().Msg("Web UI handler configured")

	return mux
}

// HasActiveClients returns true if there are active WebSocket clients connected.
func (w *WebUI) HasActiveClients() bool {
	return w.webSocketClients > 0
}

//go:embed html/*
var htmlFiles embed.FS

// htmlRouterHandlerFunc serves HTML pages based on the request path.
func (w *WebUI) htmlRouterHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		path := request.URL.Path

		var filename string
		if path == "/" {
			filename = "index.html"
		} else {
			filename = path[1:] + ".html"
		}

		content, err := htmlFiles.ReadFile("html/" + filename)
		if err != nil {
			response.WriteHeader(http.StatusNotFound)
			w.log.Error().Err(err).Str("file", filename).Str("path", path).Msg("HTML file not found")

			return
		}

		response.Header().Set("Content-Type", "text/html; charset=utf-8")

		length, err := response.Write(content)
		if err != nil {
			w.log.Error().Err(err).Int("bytes_written", length).Str("file", filename).Str("path", path).Msg("writing HTML response")

			return
		}

		w.log.Debug().Str("file", filename).Str("path", path).Int("bytes_written", length).Msg("served HTML page")
	}
}

//go:embed static/*
var staticFiles embed.FS

// staticFileHandlerFunc serves static files with automatic content type detection.
func (w *WebUI) staticFileHandlerFunc(fileType string) func(w http.ResponseWriter, r *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		filename := "static" + request.URL.Path

		content, err := staticFiles.ReadFile(filename)
		if err != nil {
			response.WriteHeader(http.StatusNotFound)
			w.log.Error().Err(err).Str("type", fileType).Msg("Invalid file")

			return
		}

		contentType := getContentType(filename)
		response.Header().Set("Content-Type", contentType)

		length, err := response.Write(content)
		if err != nil {
			w.log.Error().Err(err).Int("bytes_written", length).Str("type", fileType).Msg("writing file")

			return
		}

		w.log.Debug().Str("file", filename).Str("mime-type", contentType).Msg("returned file")
	}
}

// imagesHandlerFunc serves static image files.
func (w *WebUI) imagesHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return w.staticFileHandlerFunc("image")
}

// sciChartJSHandlerFunc serves static JavaScript files.
func (w *WebUI) sciChartJSHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return w.staticFileHandlerFunc("javascript")
}

// cssHandlerFunc serves static CSS files.
func (w *WebUI) cssHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return w.staticFileHandlerFunc("CSS")
}

// handleWebSocketConnection upgrades the HTTP connection to a WebSocket and streams telemetry data.
func (w *WebUI) handleWebSocketConnection(response http.ResponseWriter, request *http.Request) {
	webSocket, err := w.upgrader.Upgrade(response, request, nil)
	if err != nil {
		w.log.Error().Err(err).Msg("error upgrading connection")

		return
	}

	defer func() {
		err := webSocket.Close()
		if err != nil {
			w.log.Error().Err(err).Msg("closing websocket connection")
		}
	}()

	w.webSocketClients++

	defer func() {
		w.webSocketClients--
		w.log.Debug().Int("clients", w.webSocketClients).Msg("websocket connection closed")
	}()

	w.log.Debug().Int("clients", w.webSocketClients).Msg("websocket connection established")

	sid := 0
	failCount := 0

	maxFailures := 60 * 5
	for data := range w.telemetryChartFeed {
		if failCount >= maxFailures {
			w.log.Error().Err(err).Str("reason", "too many failures").Msg("dropping websocket connection")

			break
		}

		if sid != 0 {
			diff := int(data["seq"]) - sid
			if diff == 0 {
				continue
			}
		}

		sid = int(data["seq"])

		encodedData, err := json.Marshal(data)
		if err != nil {
			w.log.Error().Err(err).Msg("failed to encode JSON data")

			continue
		}

		err = webSocket.WriteMessage(websocket.TextMessage, encodedData)
		if err != nil {
			failCount++

			w.log.Debug().Err(err).Msg("failed to send data to websocket")

			continue
		}

		failCount = 0
	}
}

// getContentType returns the appropriate MIME type based on file extension using the standard library.
func getContentType(filename string) string {
	ext := filepath.Ext(filename)
	contentType := mime.TypeByExtension(ext)

	if contentType == "" {
		return "application/octet-stream"
	}

	return contentType
}

// handleConfigAPI handles GET and POST requests for configuration management.
func (w *WebUI) handleConfigAPI(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	switch request.Method {
	case http.MethodGet:
		w.handleGetConfig(response, request)
	case http.MethodPost:
		w.handleSetConfig(response, request)
	default:
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding
	}
}

// handleGetConfig returns the current configuration as JSON.
func (w *WebUI) handleGetConfig(response http.ResponseWriter, _ *http.Request) {
	configData := map[string]any{
		"app": map[string]any{
			"language":     *w.config.GetAppLanguage(),
			"accent":       w.config.GetAppAccent(),
			"logLevel":     w.config.GetAppLogLevel(),
			"baseDir":      w.config.GetAppBaseDir(),
			"replayMode":   w.config.GetAppReplayMode(),
			"webUIEnabled": w.config.GetAppWebUIEnabled(),
			"webUIPort":    w.config.GetAppWebUIPort(),
		},
		"discord": map[string]any{
			"enabled":        w.config.GetDiscordEnabled(),
			"token":          w.config.GetDiscordToken(),
			"guildID":        w.config.GetDiscordGuildID(),
			"channelID":      w.config.GetDiscordChannelID(),
			"voiceChannelID": w.config.GetDiscordVoiceChannelID(),
		},
		"fuel": map[string]any{
			"monitoringEnabled":       w.config.GetFuelMonitoringEnabled(),
			"preWarnNotifyLaps":       w.config.GetFuelPreWarnNotifyLaps(),
			"strategyNotifyLaps":      w.config.GetFuelStrategyNotifyLaps(),
			"rangeSafetyMarginLaps":   w.config.GetFuelRangeSafetyMarginLaps(),
			"rangeSafetyMarginMeters": w.config.GetFuelRangeSafetyMarginMeters(),
		},
		"hardware": map[string]any{
			"model":              w.config.GetHardwareModel(),
			"displayOrientation": w.config.GetDisplayOrientation(),
		},
		"haptics": map[string]any{
			"dynamicTransmissionFeedback":  w.config.DynamicTransmissionFeedbackEnabled(),
			"dynamicTransmissionCurve":     w.config.GetTransmissionCurve(),
			"dynamicTransmissionGforceMax": w.config.GetTransmissionGforceMax(),
			"jerkCurve":                    w.config.GetJerkCurve(),
			"jerkMax":                      w.config.GetJerkMax(),
			"snapCurve":                    w.config.GetSnapCurve(),
			"snapMax":                      w.config.GetSnapMax(),
			"pulseMaxAmplitude":            w.config.GetPulseMaxAmplitude(),
			"pulseMaxFrequencyHz":          w.config.GetMaxHz(),
			"pulseMinFrequencyHz":          w.config.GetMinHz(),
		},
		"pitRadio": map[string]any{
			"enabled":               w.config.PitRadioEnabled(),
			"messageSendIntervalMs": w.config.GetMessageSendIntervalMs(),
		},
		"synthesizer": map[string]any{
			"internalSampleRateHz":      w.config.GetInternalSampleRateHz(),
			"outputSampleRateHz":        w.config.GetOutputSampleRateHz(),
			"outputFile":                w.config.GetOutputFile(),
			"masterGain":                w.config.GetMasterGain(),
			"chassisGain":               w.config.GetChassisGain(),
			"transmissionGain":          w.config.GetTransmissionGain(),
			"transmissionGainMinRace":   w.config.GetTransmissionGainMinRace(),
			"transmissionGainMinStreet": w.config.GetTransmissionGainMinStreet(),
			"engineGain":                w.config.GetEngineGain(),
			"gainIncrement":             w.config.GetGainIncrement(),
		},
		"telemetry": map[string]any{
			"source": w.config.GetTelemetrySource(),
		},
		"tyres": map[string]any{
			"monitoringEnabled":          w.config.GetTyreMonitoringEnabled(),
			"temperatureOptimalCelsius":  w.config.GetTyreTemperatureOptimalCelsius(),
			"temperatureOperatingWindow": w.config.GetTyreTemperatureOperatingWindow(),
			"temperatureMarginCelsius":   w.config.GetTyreTemperatureMarginCelsius(),
		},
	}

	err := json.NewEncoder(response).Encode(configData)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to encode config JSON")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to encode configuration"}) //nolint:errchkjson // simple encoding
	}
}

// handleSetConfig updates the configuration from JSON data.
func (w *WebUI) handleSetConfig(response http.ResponseWriter, request *http.Request) {
	var configData map[string]any

	err := json.NewDecoder(request.Body).Decode(&configData)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to decode config JSON")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Invalid JSON data"}) //nolint:errchkjson // simple encoding

		return
	}

	// Apply configuration changes using setter methods
	errors := w.applyConfigChanges(configData)

	if len(errors) > 0 {
		w.log.Error().Strs("errors", errors).Msg("failed to apply some configuration changes")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":  "partial_success",
			"message": "Some configuration changes failed",
			"errors":  errors,
		})

		return
	}

	w.log.Info().Interface("config", configData).Msg("configuration updated successfully")

	// Save configuration to file
	err = w.config.SaveConfigToFile()
	if err != nil {
		w.log.Error().Err(err).Msg("failed to save configuration to file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Configuration updated but failed to save: " + err.Error(),
		})

		return
	}

	w.log.Info().Msg("configuration saved to file")

	// Return success response
	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Configuration updated and saved successfully",
	})
}

// applyConfigChanges applies the configuration changes using appropriate setter methods.
func (w *WebUI) applyConfigChanges(configData map[string]any) []string {
	var errors []string

	// Process each section of the configuration
	for section, sectionData := range configData {
		sectionMap, ok := sectionData.(map[string]any)
		if !ok {
			errors = append(errors, "invalid data for section "+section)

			continue
		}

		switch section {
		case "app":
			errors = append(errors, w.applyAppConfig(sectionMap)...)
		case "synthesizer":
			errors = append(errors, w.applySynthesizerConfig(sectionMap)...)
		case "haptics":
			errors = append(errors, w.applyHapticsConfig(sectionMap)...)
		case "fuel":
			errors = append(errors, w.applyFuelConfig(sectionMap)...)
		case "telemetry":
			errors = append(errors, w.applyTelemetryConfig(sectionMap)...)
		// Add more sections as needed
		default:
			w.log.Debug().Str("section", section).Msg("configuration section not implemented for updates")
		}
	}

	return errors
}

// applyAppConfig applies application configuration changes.
func (w *WebUI) applyAppConfig(config map[string]any) []string {
	var errors []string

	if language, ok := config["language"]; ok {
		if langStr, ok := language.(string); ok {
			w.config.SetAppLanguage(langStr)
		} else {
			errors = append(errors, "invalid language value")
		}
	}

	if logLevel, ok := config["logLevel"]; ok {
		if levelStr, ok := logLevel.(string); ok {
			w.config.SetAppLogLevel(levelStr)
		} else {
			errors = append(errors, "invalid log level value")
		}
	}

	if baseDir, ok := config["baseDir"]; ok {
		if baseDirStr, ok := baseDir.(string); ok {
			w.config.SetAppBaseDir(baseDirStr)
		} else {
			errors = append(errors, "invalid base directory value")
		}
	}

	if vehicleDBFile, ok := config["vehicleDBFile"]; ok {
		if vehicleDBFileStr, ok := vehicleDBFile.(string); ok {
			// If a file path is provided, validate that it exists
			if vehicleDBFileStr != "" {
				if _, err := os.Stat(vehicleDBFileStr); err != nil {
					if os.IsNotExist(err) {
						errors = append(errors, "vehicle database file not found: "+vehicleDBFileStr)
					} else {
						errors = append(errors, "cannot access vehicle database file: "+err.Error())
					}

					return errors
				}
			}

			oldVehicleDBFile := w.config.GetAppVehicleDBFile()
			w.config.SetAppVehicleDBFile(vehicleDBFileStr)

			// Signal GT client restart if vehicle DB file changed
			if oldVehicleDBFile != vehicleDBFileStr {
				w.log.Info().
					Str("old_vehicle_db", oldVehicleDBFile).
					Str("new_vehicle_db", vehicleDBFileStr).
					Msg("Vehicle database file changed, signaling GT client restart")

				select {
				case w.shutdownChan <- exitcode.RestartGTClient:
					w.log.Debug().Msg("GT client restart signal sent")
				default:
					w.log.Debug().Msg("GT client restart signal already pending")
				}
			}
		} else {
			errors = append(errors, "invalid vehicle database file value")
		}
	}

	return errors
}

// applySynthesizerConfig applies synthesizer configuration changes.
func (w *WebUI) applySynthesizerConfig(config map[string]any) []string {
	var errors []string

	if masterGain, ok := config["masterGain"]; ok {
		if gainFloat, ok := masterGain.(float64); ok {
			w.config.SetMasterGain(gainFloat)
		} else {
			errors = append(errors, "invalid master gain value")
		}
	}

	if chassisGain, ok := config["chassisGain"]; ok {
		if gainFloat, ok := chassisGain.(float64); ok {
			w.config.SetChassisGain(gainFloat)
		} else {
			errors = append(errors, "invalid chassis gain value")
		}
	}

	if transmissionGain, ok := config["transmissionGain"]; ok {
		if gainFloat, ok := transmissionGain.(float64); ok {
			w.config.SetTransmissionGain(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain value")
		}
	}

	if engineGain, ok := config["engineGain"]; ok {
		if gainFloat, ok := engineGain.(float64); ok {
			w.config.SetEngineGain(gainFloat)
		} else {
			errors = append(errors, "invalid engine gain value")
		}
	}

	return errors
}

// applyHapticsConfig applies haptics configuration changes.
//
//nolint:cyclop // functio is easy to understand
func (w *WebUI) applyHapticsConfig(config map[string]any) []string {
	var errors []string

	// Helper for float64 conversion and error append
	parseFloat := func(val any, key string) (float64, bool) {
		f, ok := val.(float64)
		if !ok {
			errors = append(errors, "invalid "+key+" value")
		}

		return f, ok
	}

	if jerkCurve, ok := config["jerkCurve"]; ok {
		if curveFloat, ok := parseFloat(jerkCurve, "jerk curve"); ok {
			w.config.SetJerkCurve(int(curveFloat * 1000.0))
		}
	}

	if jerkMax, ok := config["jerkMax"]; ok {
		if maxFloat, ok := parseFloat(jerkMax, "jerk max"); ok {
			w.config.SetJerkMax(int(maxFloat))
		}
	}

	if snapCurve, ok := config["snapCurve"]; ok {
		if curveFloat, ok := parseFloat(snapCurve, "snap curve"); ok {
			w.config.SetSnapCurve(int(curveFloat * 1000.0))
		}
	}

	if snapMax, ok := config["snapMax"]; ok {
		if maxFloat, ok := parseFloat(snapMax, "snap max"); ok {
			w.config.SetSnapMax(int(maxFloat))
		}
	}

	if transmissionCurve, ok := config["dynamicTransmissionCurve"]; ok {
		if curveFloat, ok := parseFloat(transmissionCurve, "transmission curve"); ok {
			w.config.SetTransmissionCurve(int(curveFloat * 1000.0))
		}
	}

	if transmissionGforceMax, ok := config["dynamicTransmissionGforceMax"]; ok {
		if gforceFloat, ok := parseFloat(transmissionGforceMax, "transmission G-force max"); ok {
			w.config.SetTransmissionGforceMax(gforceFloat)
		}
	}

	return errors
}

// applyFuelConfig applies fuel management configuration changes.
func (w *WebUI) applyFuelConfig(config map[string]any) []string {
	var errors []string

	if monitoringEnabled, ok := config["monitoringEnabled"]; ok {
		if enabledBool, ok := monitoringEnabled.(bool); ok {
			w.config.SetFuelMonitoringEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid fuel monitoring enabled value")
		}
	}

	return errors
}

// applyTelemetryConfig applies telemetry configuration changes.
func (w *WebUI) applyTelemetryConfig(config map[string]any) []string {
	var errors []string

	if source, ok := config["source"]; ok {
		if sourceStr, ok := source.(string); ok {
			// If source is a file:// path, validate that the file exists
			if strings.HasPrefix(sourceStr, "file://") {
				filePath := strings.TrimPrefix(sourceStr, "file://")
				if _, err := os.Stat(filePath); err != nil {
					if os.IsNotExist(err) {
						errors = append(errors, "telemetry replay file not found: "+filePath)
					} else {
						errors = append(errors, "cannot access telemetry replay file: "+err.Error())
					}

					return errors
				}
			}

			oldSource := w.config.GetTelemetrySource()
			w.config.SetTelemetrySource(sourceStr)

			// Signal GT client restart if source changed
			if oldSource != sourceStr {
				w.log.Info().
					Str("old_source", oldSource).
					Str("new_source", sourceStr).
					Msg("Telemetry source changed, signaling GT client restart")

				select {
				case w.shutdownChan <- exitcode.RestartGTClient:
					w.log.Debug().Msg("GT client restart signal sent")
				default:
					w.log.Debug().Msg("GT client restart signal already pending")
				}
			}
		} else {
			errors = append(errors, "invalid telemetry source value")
		}
	}

	return errors
}

// handleConfigReset resets the configuration to default values.
func (w *WebUI) handleConfigReset(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	w.log.Info().Msg("configuration reset requested")

	w.config.SetDefault()

	w.handleGetConfig(response, request)
}

// handleRestart handles POST requests to restart the application.
func (w *WebUI) handleRestart(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")
	w.log.Info().Msg("application restart requested")

	// Return success response
	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Application restarting...",
	})

	// Trigger application shutdown in a goroutine to allow the response to be sent
	go func() {
		time.Sleep(500 * time.Millisecond) // Give time for response to be sent
		w.log.Info().Msg("initiating restart")

		w.shutdownChan <- exitcode.RestartApp
	}()
}

// handleSetupMode handles both GET (check availability) and POST (activate setup mode) requests.
func (w *WebUI) handleSetupMode(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	switch request.Method {
	case http.MethodGet:
		// Check if the config file exists
		configFile := "/boot/firmware/simtezilo/simtezilo.conf"
		_, err := os.Stat(configFile)
		available := err == nil

		_ = json.NewEncoder(response).Encode(map[string]bool{ //nolint:errchkjson // simple encoding
			"available": available,
		})

	case http.MethodPost:
		w.log.Info().Msg("setup mode requested")

		// Execute setup binary to enable setup mode
		setupBinPath := filepath.Join(w.config.GetAppBaseDir(), "bin", "setup")
		cmd := exec.CommandContext(request.Context(), setupBinPath, "enable")

		output, err := cmd.CombinedOutput()
		if err != nil {
			w.log.Error().Err(err).Str("output", string(output)).Msg("failed to enable setup mode")
			response.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
				"status":  "error",
				"message": "Failed to enable setup mode: " + err.Error(),
			})

			return
		}

		w.log.Info().Str("output", string(output)).Msg("setup mode enabled")

		// Return success response
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "success",
			"message": "Setup mode activated. Application will shut down.",
		})

		// Trigger application shutdown in a goroutine to allow the response to be sent
		go func() {
			time.Sleep(500 * time.Millisecond) // Give time for response to be sent
			w.log.Info().Msg("initiating shutdown for setup mode")

			w.shutdownChan <- exitcode.SetupMode
		}()

	default:
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding
	}
}

// handleFactoryReset handles POST requests to perform a factory reset.
func (w *WebUI) handleFactoryReset(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	w.log.Warn().Msg("factory reset requested - all settings and network configurations will be deleted")

	// Execute setup binary with reset action to delete all connections and reinitialize
	// Note: No response is sent as the network will disconnect during the reset
	setupBinPath := filepath.Join(w.config.GetAppBaseDir(), "bin", "setup")
	cmd := exec.CommandContext(request.Context(), setupBinPath, "reset")

	output, err := cmd.CombinedOutput()
	if err != nil {
		w.log.Error().Err(err).Str("output", string(output)).Msg("failed to perform factory reset")

		return
	}

	w.log.Info().Str("output", string(output)).Msg("factory reset completed successfully")

	// Trigger application shutdown to restart in setup mode
	w.log.Info().Msg("initiating shutdown for setup mode after factory reset")

	w.shutdownChan <- exitcode.SetupMode
}

// handleI18nAPI handles GET requests for i18n translations.
func (w *WebUI) handleI18nAPI(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	// Get language from query parameter, default to config language
	lang := request.URL.Query().Get("lang")
	if lang == "" {
		lang = *w.config.GetAppLanguage()
	}

	// Get all translations with the "runmode." prefix
	i18n := w.config.GetI18n()
	if i18n == nil {
		w.log.Error().Msg("i18n instance not available")
		http.Error(response, "i18n not configured", http.StatusInternalServerError)

		return
	}

	translations := i18n.GetStringsWithPrefixForLanguage(lang, "runmode.")

	data, err := json.Marshal(translations)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to marshal translations")
		http.Error(response, "error encoding translations", http.StatusInternalServerError)

		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "public, max-age=3600")

	length, err := response.Write(data)
	if err != nil {
		w.log.Error().Err(err).Int("bytes_written", length).Msg("error writing i18n response")

		return
	}

	w.log.Debug().Str("language", lang).Msg("served i18n translations")
}

// handleLanguagesAPI handles GET requests for available languages.
func (w *WebUI) handleLanguagesAPI(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	i18n := w.config.GetI18n()
	if i18n == nil {
		w.log.Error().Msg("i18n instance not available")
		http.Error(response, "i18n not configured", http.StatusInternalServerError)

		return
	}

	languagesMap := i18n.Languages()

	// Build response as array of language objects
	type languageInfo struct {
		Code string `json:"code"` //nolint:tagliatelle // lowercase for interface simpicity
		Name string `json:"name"` //nolint:tagliatelle
	}

	languages := make([]languageInfo, 0, len(languagesMap))
	for code, metadata := range languagesMap {
		languages = append(languages, languageInfo{
			Code: code,
			Name: metadata.Name,
		})
	}

	data, err := json.Marshal(languages)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to marshal languages")
		http.Error(response, "error encoding languages", http.StatusInternalServerError)

		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "public, max-age=3600")

	length, err := response.Write(data)
	if err != nil {
		w.log.Error().Err(err).Int("bytes_written", length).Msg("error writing languages response")

		return
	}

	w.log.Debug().Msg("served available languages")
}

// handleLogsAPI returns log entries from the in-memory store.
func (w *WebUI) handleLogsAPI(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	if w.logStore == nil {
		w.log.Warn().Msg("log store not initialized")
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Log store not available"}) //nolint:errchkjson // simple encoding

		return
	}

	// Parse pagination parameters
	queryParams := request.URL.Query()
	page := 1
	pageSize := 100

	if pageStr := queryParams.Get("page"); pageStr != "" {
		if p, err := fmt.Sscanf(pageStr, "%d", &page); err == nil && p == 1 {
			if page < 1 {
				page = 1
			}
		}
	}

	if pageSizeStr := queryParams.Get("pageSize"); pageSizeStr != "" {
		if ps, err := fmt.Sscanf(pageSizeStr, "%d", &pageSize); err == nil && ps == 1 {
			if pageSize < 1 {
				pageSize = 100
			} else if pageSize > 1000 {
				pageSize = 1000 // max limit
			}
		}
	}

	// Parse level filters
	var levelFilters map[string]bool

	if levelsParam := queryParams.Get("levels"); levelsParam != "" {
		levels := strings.Split(levelsParam, ",")
		levelFilters = make(map[string]bool)

		for _, level := range levels {
			trimmedLevel := strings.TrimSpace(level)
			if trimmedLevel != "" {
				levelFilters[trimmedLevel] = true
			}
		}
	}

	// Get all logs and filter by level if needed
	allLogs := w.logStore.GetAll()

	var filteredLogs []logstore.LogEntry

	if len(levelFilters) > 0 {
		for _, log := range allLogs {
			if level, ok := log["level"].(string); ok && levelFilters[level] {
				filteredLogs = append(filteredLogs, log)
			}
		}
	} else {
		filteredLogs = allLogs
	}

	// Calculate pagination based on filtered results
	totalCount := len(filteredLogs)

	totalPages := (totalCount + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	// Validate page number
	if page > totalPages {
		page = totalPages
	}

	// Calculate offset and slice the filtered logs
	offset := (page - 1) * pageSize

	endIdx := offset + pageSize
	if endIdx > totalCount {
		endIdx = totalCount
	}

	var logs []logstore.LogEntry
	if offset < totalCount {
		logs = filteredLogs[offset:endIdx]
	} else {
		logs = []logstore.LogEntry{}
	}

	stats := w.logStore.GetStats()

	// Build response
	responseData := map[string]any{
		"logs":       logs,
		"stats":      stats,
		"count":      len(logs),
		"totalCount": totalCount,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	err := json.NewEncoder(response).Encode(responseData)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to encode logs response")
		http.Error(response, "error encoding logs", http.StatusInternalServerError)

		return
	}

	w.log.Debug().Int("log_count", len(logs)).Int("page", page).Msg("served logs")
}
