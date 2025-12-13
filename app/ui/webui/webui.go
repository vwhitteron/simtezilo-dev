// Package webui implements a simple web server to serve a web-based user interface
package webui

import (
	"embed"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
)

// WebUI defines the web user interface.
type WebUI struct {
	log                zerolog.Logger
	port               int
	webSocketClients   int
	telemetryChartFeed chan map[string]float32
	config             *config.Config
	upgrader           websocket.Upgrader
	shutdownChan       chan exitcode.ExitCode
	setupModeEnabled   bool
}

type Config struct {
	Log                zerolog.Logger
	Port               int
	TelemetryChartFeed chan map[string]float32
	Config             *config.Config
	ShutdownChan       chan exitcode.ExitCode
	SetupModeEnabled   bool
}

// New creates a new instance of the WebUI.
func New(config Config) *WebUI {
	return &WebUI{
		log:                log.With().Str("component", "web ui").Logger(),
		port:               config.Port,
		webSocketClients:   0,
		telemetryChartFeed: config.TelemetryChartFeed,
		config:             config.Config,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		shutdownChan:     config.ShutdownChan,
		setupModeEnabled: config.SetupModeEnabled,
	}
}

// Start sets up handlers and starts the web server.
func (w *WebUI) Start() {
	http.HandleFunc("/", w.htmlRouterHandlerFunc())
	http.HandleFunc("/css/", w.cssHandlerFunc())
	http.HandleFunc("/images/", w.imagesHandlerFunc())
	http.HandleFunc("/js/", w.sciChartJSHandlerFunc())
	http.HandleFunc("/ws", w.handleWebSocketConnection)
	http.HandleFunc("/api/config", w.handleConfigAPI)
	http.HandleFunc("/api/config/save", w.handleConfigSave)
	http.HandleFunc("/api/config/reset", w.handleConfigReset)

	if w.setupModeEnabled {
		http.HandleFunc("/api/mode/setup", w.handleSetupMode)
	}

	w.log.Info().Int("port", w.port).Msg("Starting Web UI server")

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", w.port),
		ReadHeaderTimeout: 3 * time.Second,
	}

	err := server.ListenAndServe()
	if err != nil {
		w.log.Error().Err(err).Msg("error starting web server")

		return
	}

	w.log.Info().Int("port", w.port).Msg("Web UI started")
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
			"dataDir":      w.config.GetAppDataDir(),
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

	// Return success response
	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Configuration updated successfully",
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
			w.config.SetTelemetrySource(sourceStr)
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

// handleConfigSave saves the current configuration to the file with backup.
func (w *WebUI) handleConfigSave(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	w.log.Info().Msg("configuration save requested")

	// Save configuration to file with backup
	backupPath, err := w.config.SaveConfigToFile()
	if err != nil {
		w.log.Error().Err(err).Msg("failed to save configuration")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"error": "Failed to save configuration: " + err.Error(),
		})

		return
	}

	w.log.Info().Str("backup", backupPath).Msg("configuration saved successfully")

	// Return success response with backup information
	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":     "success",
		"message":    "Configuration saved",
		"backupPath": backupPath,
	})
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
