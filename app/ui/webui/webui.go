// Package webui implements a simple web server to serve a web-based user interface
package webui

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	appHaptics "github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/logstore"
)

// WSMessage represents a typed message envelope for the unified WebSocket.
type WSMessage struct {
	Type      string `json:"type"`      // "telemetry", "vehicle", "circuit", "race", "gameState"
	Timestamp int64  `json:"timestamp"` // Unix timestamp in milliseconds
	Data      any    `json:"data"`      // Payload data
}

// subscriptionUpdate represents a client's subscription preferences.
type subscriptionUpdate struct {
	client        *websocket.Conn
	subscriptions map[string]bool // map of data type to subscribed status
}

// WebUI defines the web user interface.
type WebUI struct {
	log                zerolog.Logger
	port               int
	webSocketClients   int
	telemetryChartFeed chan map[string]float32
	vehicleInfoFeed    chan map[string]any
	currentVehicleInfo map[string]any
	vehicleInfoMutex   sync.RWMutex
	gameStateFeed      chan string
	currentGameState   string
	gameStateMutex     sync.RWMutex
	circuitInfoFeed    chan map[string]string
	currentCircuitInfo map[string]string
	circuitInfoMutex   sync.RWMutex
	raceInfoFeed       chan map[string]any
	currentRaceInfo    map[string]any
	raceInfoMutex      sync.RWMutex
	config             *appconfig.Config
	upgrader           websocket.Upgrader
	shutdownChan       chan exitcode.Code
	setupModeEnabled   bool
	logStore           *logstore.Store
	logStatsFeed       chan map[string]any
	currentLogStats    map[string]any
	logStatsMutex      sync.RWMutex
	buildVersion       string
	buildTime          string
	buildPlatform      string
	// Unified WebSocket support
	unifiedClients      []*websocket.Conn
	unifiedClientsChan  chan *websocket.Conn
	unifiedUnsubChan    chan *websocket.Conn
	unifiedSessions     map[string]*websocket.Conn // Track sessions to prevent duplicates
	unifiedSessionsMux  sync.Mutex
	clientSubscriptions map[*websocket.Conn]map[string]bool // Track what data types each client wants
	subscriptionsMutex  sync.RWMutex
	subscriptionChan    chan subscriptionUpdate
}

type Config struct {
	Log                zerolog.Logger
	Port               int
	TelemetryChartFeed chan map[string]float32
	VehicleInfoFeed    chan map[string]any
	CircuitInfoFeed    chan map[string]string
	RaceInfoFeed       chan map[string]any
	GameStateFeed      chan string
	Config             *appconfig.Config
	ShutdownChan       chan exitcode.Code
	SetupModeAvailable bool
	LogStore           *logstore.Store
	LogStatsFeed       chan map[string]any
	BuildVersion       string
	BuildTime          string
	BuildPlatform      string
}

// New creates a new instance of the WebUI.
func New(config Config) *WebUI {
	webUI := &WebUI{
		log:                config.Log.With().Str("component", "web ui").Logger(),
		port:               config.Port,
		webSocketClients:   0,
		telemetryChartFeed: config.TelemetryChartFeed,
		vehicleInfoFeed:    config.VehicleInfoFeed,
		currentVehicleInfo: make(map[string]any),
		gameStateFeed:      config.GameStateFeed,
		currentGameState:   "unknown",
		circuitInfoFeed:    config.CircuitInfoFeed,
		currentCircuitInfo: make(map[string]string),
		raceInfoFeed:       config.RaceInfoFeed,
		currentRaceInfo:    make(map[string]any),
		config:             config.Config,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		shutdownChan:        config.ShutdownChan,
		setupModeEnabled:    config.SetupModeAvailable,
		logStore:            config.LogStore,
		logStatsFeed:        config.LogStatsFeed,
		currentLogStats:     make(map[string]any),
		buildVersion:        config.BuildVersion,
		buildTime:           config.BuildTime,
		buildPlatform:       config.BuildPlatform,
		unifiedClients:      make([]*websocket.Conn, 0),
		unifiedClientsChan:  make(chan *websocket.Conn, 10),
		unifiedUnsubChan:    make(chan *websocket.Conn, 10),
		unifiedSessions:     make(map[string]*websocket.Conn),
		clientSubscriptions: make(map[*websocket.Conn]map[string]bool),
		subscriptionChan:    make(chan subscriptionUpdate, 10),
	}

	// Start unified websocket broadcaster
	go webUI.unifiedWebSocketBroadcaster()

	return webUI
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
	mux.HandleFunc("/api/config/export", w.handleConfigExport)
	mux.HandleFunc("/api/config/import", w.handleConfigImport)
	mux.HandleFunc("/api/config/reset", w.handleConfigReset)
	mux.HandleFunc("/api/config/status", w.handleConfigStatus)
	mux.HandleFunc("/api/i18n", w.handleI18nAPI)
	mux.HandleFunc("/api/languages", w.handleLanguagesAPI)
	mux.HandleFunc("/api/logs", w.handleLogsAPI)
	mux.HandleFunc("/api/system/cache-clear", w.handleCacheClear)
	mux.HandleFunc("/api/system/cache-size", w.handleCacheSize)
	mux.HandleFunc("/api/system/info", w.handleSystemInfo)
	mux.HandleFunc("/api/system/restart", w.handleRestart)

	if w.setupModeEnabled {
		mux.HandleFunc("/api/system/factory-reset", w.handleFactoryReset)
		mux.HandleFunc("/api/mode/setup", w.handleSetupMode)
	}

	w.log.Debug().Msg("Web UI handler configured")

	return mux
}

// HasActiveClients returns true if there are active WebSocket clients connected.
func (w *WebUI) HasActiveClients() bool {
	return len(w.unifiedClients) > 0 || w.webSocketClients > 0
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

// handleWebSocketConnection upgrades the HTTP connection to a unified WebSocket
// that can handle multiple message types: telemetry, vehicle, circuit, race, and gameState.
func (w *WebUI) handleWebSocketConnection(response http.ResponseWriter, request *http.Request) {
	// Get session ID from query parameter
	sessionID := request.URL.Query().Get("session")

	// Close any existing connection for this session
	if sessionID != "" {
		w.unifiedSessionsMux.Lock()

		if oldConn, exists := w.unifiedSessions[sessionID]; exists {
			w.log.Debug().Str("session", sessionID).Msg("closing old unified connection for session")

			_ = oldConn.Close()
			// Remove from clients list immediately
			w.unifiedUnsubChan <- oldConn
		}

		w.unifiedSessionsMux.Unlock()
	}

	webSocket, err := w.upgrader.Upgrade(response, request, nil)
	if err != nil {
		w.log.Error().Err(err).Msg("error upgrading unified websocket connection")

		return
	}

	w.log.Debug().Str("session", sessionID).Msg("unified websocket connection established")

	// Track this session
	if sessionID != "" {
		w.unifiedSessionsMux.Lock()
		w.unifiedSessions[sessionID] = webSocket
		w.unifiedSessionsMux.Unlock()
	}

	// Subscribe this client
	w.unifiedClientsChan <- webSocket

	defer func() {
		// Remove from session map
		if sessionID != "" {
			w.unifiedSessionsMux.Lock()
			delete(w.unifiedSessions, sessionID)
			w.unifiedSessionsMux.Unlock()
		}

		// Unsubscribe on disconnect
		w.unifiedUnsubChan <- webSocket

		err := webSocket.Close()
		if err != nil {
			w.log.Debug().Err(err).Msg("closing unified websocket connection")
		}
	}()

	// Send current vehicle info immediately (with mutex protection)
	w.vehicleInfoMutex.RLock()

	if len(w.currentVehicleInfo) > 0 {
		msg := WSMessage{
			Type:      "vehicle",
			Timestamp: time.Now().UnixMilli(),
			Data:      w.currentVehicleInfo,
		}

		encodedData, err := json.Marshal(msg)
		if err == nil {
			_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = webSocket.WriteMessage(websocket.TextMessage, encodedData)
		}
	}

	w.vehicleInfoMutex.RUnlock()

	// Send current game state immediately
	w.gameStateMutex.RLock()
	gameState := w.currentGameState
	w.gameStateMutex.RUnlock()

	if gameState != "" {
		msg := WSMessage{
			Type:      "gameState",
			Timestamp: time.Now().UnixMilli(),
			Data:      map[string]any{"gamestate": gameState},
		}

		encodedData, err := json.Marshal(msg)
		if err == nil {
			_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = webSocket.WriteMessage(websocket.TextMessage, encodedData)
		}
	}

	// Send current circuit info immediately
	w.circuitInfoMutex.RLock()

	if len(w.currentCircuitInfo) > 0 {
		msg := WSMessage{
			Type:      "circuit",
			Timestamp: time.Now().UnixMilli(),
			Data:      w.currentCircuitInfo,
		}

		encodedData, err := json.Marshal(msg)
		if err == nil {
			_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = webSocket.WriteMessage(websocket.TextMessage, encodedData)
		}
	}

	w.circuitInfoMutex.RUnlock()

	// Send current race info immediately
	w.raceInfoMutex.RLock()

	if len(w.currentRaceInfo) > 0 {
		msg := WSMessage{
			Type:      "race",
			Timestamp: time.Now().UnixMilli(),
			Data:      w.currentRaceInfo,
		}

		encodedData, err := json.Marshal(msg)
		if err == nil {
			_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = webSocket.WriteMessage(websocket.TextMessage, encodedData)
		}
	}

	w.raceInfoMutex.RUnlock()

	// Send current log stats immediately
	w.logStatsMutex.RLock()

	if len(w.currentLogStats) > 0 {
		msg := WSMessage{
			Type:      "logStats",
			Timestamp: time.Now().UnixMilli(),
			Data:      w.currentLogStats,
		}

		encodedData, err := json.Marshal(msg)
		if err == nil {
			_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = webSocket.WriteMessage(websocket.TextMessage, encodedData)
		}
	}

	w.logStatsMutex.RUnlock()

	// Keep connection alive - read messages (if any) to detect disconnects
	// Set read deadline - if no pong received in 10 seconds, connection is dead
	_ = webSocket.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Handle pong messages
	webSocket.SetPongHandler(func(string) error {
		_ = webSocket.SetReadDeadline(time.Now().Add(10 * time.Second))

		return nil
	})

	// Handle close messages from client
	webSocket.SetCloseHandler(func(code int, text string) error {
		w.log.Debug().Int("code", code).Str("text", text).Msg("unified websocket close message received")

		return nil
	})

	// Send pings every 5 seconds to detect dead connections
	pingTicker := time.NewTicker(5 * time.Second)
	defer pingTicker.Stop()

	done := make(chan struct{})

	// Goroutine to handle pings
	go func() {
		defer close(done)

		for {
			select {
			case <-pingTicker.C:
				_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))

				err := webSocket.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					w.log.Debug().Err(err).Msg("failed to send ping on unified websocket")

					return
				}
			}
		}
	}()

	// Handle incoming messages (subscription updates)
	for {
		_, message, err := webSocket.ReadMessage()
		if err != nil {
			w.log.Debug().Err(err).Msg("unified websocket read error, closing connection")

			break
		}

		// Reset read deadline on any message
		_ = webSocket.SetReadDeadline(time.Now().Add(10 * time.Second))

		// Try to parse as subscription message
		var subMsg struct {
			Type          string          `json:"type"`
			Subscriptions map[string]bool `json:"subscriptions"`
		}

		if err := json.Unmarshal(message, &subMsg); err == nil && subMsg.Type == "subscribe" {
			// Send subscription update to broadcaster
			w.subscriptionChan <- subscriptionUpdate{
				client:        webSocket,
				subscriptions: subMsg.Subscriptions,
			}

			w.log.Debug().
				Interface("subscriptions", subMsg.Subscriptions).
				Msg("client updated subscriptions")
		}
	}
}

// unifiedWebSocketBroadcaster manages the unified websocket, handling all message types.
func (w *WebUI) unifiedWebSocketBroadcaster() {
	// Batch configuration for telemetry
	batchFrameRate := 30
	bufferSize := batchFrameRate / 60
	batchInterval := time.Duration(1000/batchFrameRate) * time.Millisecond
	batchBuffer := make([]map[string]float32, 0, bufferSize)

	// Track sequence ID for telemetry deduplication
	sid := 0

	// Ticker for batched telemetry sends
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	for {
		select {
		case client := <-w.unifiedClientsChan:
			// Add new client with default subscriptions (all enabled except telemetry)
			w.unifiedClients = append(w.unifiedClients, client)

			w.subscriptionsMutex.Lock()
			w.clientSubscriptions[client] = map[string]bool{
				"vehicle":   true,
				"gameState": true,
				"circuit":   true,
				"race":      true,
				"logStats":  true,
				"telemetry": false, // Telemetry off by default
			}
			w.subscriptionsMutex.Unlock()

			w.log.Debug().Int("unified_clients", len(w.unifiedClients)).Msg("unified client subscribed")

		case client := <-w.unifiedUnsubChan:
			// Remove client
			for i, c := range w.unifiedClients {
				if c == client {
					w.unifiedClients = append(w.unifiedClients[:i], w.unifiedClients[i+1:]...)

					break
				}
			}

			// Remove subscriptions
			w.subscriptionsMutex.Lock()
			delete(w.clientSubscriptions, client)
			w.subscriptionsMutex.Unlock()

			w.log.Debug().Int("unified_clients", len(w.unifiedClients)).Msg("unified client unsubscribed")

		case subUpdate := <-w.subscriptionChan:
			// Update client subscriptions
			w.subscriptionsMutex.Lock()

			if _, exists := w.clientSubscriptions[subUpdate.client]; exists {
				for dataType, subscribed := range subUpdate.subscriptions {
					w.clientSubscriptions[subUpdate.client][dataType] = subscribed
				}
			}

			w.subscriptionsMutex.Unlock()

		case data := <-w.telemetryChartFeed:
			// Handle telemetry data - add to batch buffer
			if sid != 0 {
				diff := int(data["seq"]) - sid
				if diff == 0 {
					continue // Skip duplicate
				}
			}

			sid = int(data["seq"])
			batchBuffer = append(batchBuffer, data)

		case <-ticker.C:
			// Send batched telemetry data
			if len(batchBuffer) == 0 {
				continue
			}

			msg := WSMessage{
				Type:      "telemetry",
				Timestamp: time.Now().UnixMilli(),
				Data:      batchBuffer,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode batched telemetry JSON")

				batchBuffer = batchBuffer[:0]

				continue
			}

			// Broadcast to all unified clients
			w.broadcastToUnifiedClients(encodedData, "telemetry")

			// Clear buffer
			batchBuffer = batchBuffer[:0]

		case vehicleInfo := <-w.vehicleInfoFeed:
			// Store current state with mutex protection
			w.vehicleInfoMutex.Lock()
			w.currentVehicleInfo = vehicleInfo
			w.vehicleInfoMutex.Unlock()

			// Broadcast vehicle info
			msg := WSMessage{
				Type:      "vehicle",
				Timestamp: time.Now().UnixMilli(),
				Data:      vehicleInfo,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode vehicle info JSON")

				continue
			}

			w.broadcastToUnifiedClients(encodedData, "vehicle")

			if len(vehicleInfo) > 0 {
				manufacturer, _ := vehicleInfo["manufacturer"].(string)
				model, _ := vehicleInfo["model"].(string)
				carID, _ := vehicleInfo["carID"].(uint32)

				w.log.Debug().
					Str("manufacturer", manufacturer).
					Str("model", model).
					Uint32("carID", carID).
					Int("clients", len(w.unifiedClients)).
					Msg("broadcast vehicle info to unified clients")
			}

		case gameState := <-w.gameStateFeed:
			// Store current state with mutex protection
			w.gameStateMutex.Lock()
			w.currentGameState = gameState
			w.gameStateMutex.Unlock()

			// Broadcast game state
			msg := WSMessage{
				Type:      "gameState",
				Timestamp: time.Now().UnixMilli(),
				Data:      map[string]any{"gamestate": gameState},
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode game state JSON")

				continue
			}

			w.broadcastToUnifiedClients(encodedData, "gameState")

			w.log.Debug().
				Str("gameState", gameState).
				Int("clients", len(w.unifiedClients)).
				Msg("broadcast game state to unified clients")

		case circuitInfo := <-w.circuitInfoFeed:
			// Store current state with mutex protection
			w.circuitInfoMutex.Lock()
			w.currentCircuitInfo = circuitInfo
			w.circuitInfoMutex.Unlock()

			// Broadcast circuit info
			msg := WSMessage{
				Type:      "circuit",
				Timestamp: time.Now().UnixMilli(),
				Data:      circuitInfo,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode circuit info JSON")

				continue
			}

			w.broadcastToUnifiedClients(encodedData, "circuit")

			if len(circuitInfo) > 0 {
				name := circuitInfo["name"]
				length := circuitInfo["length"]

				w.log.Debug().
					Str("circuit", name).
					Str("length", length).
					Int("clients", len(w.unifiedClients)).
					Msg("broadcast circuit info to unified clients")
			}

		case raceInfo := <-w.raceInfoFeed:
			// Store current state with mutex protection
			w.raceInfoMutex.Lock()
			w.currentRaceInfo = raceInfo
			w.raceInfoMutex.Unlock()

			// Broadcast race info
			msg := WSMessage{
				Type:      "race",
				Timestamp: time.Now().UnixMilli(),
				Data:      raceInfo,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode race info JSON")

				continue
			}

			w.broadcastToUnifiedClients(encodedData, "race")

			if len(raceInfo) > 0 {
				lap, _ := raceInfo["lap"].(int)
				totalLaps, _ := raceInfo["totalLaps"].(int)

				w.log.Debug().
					Int("lap", lap).
					Int("totalLaps", totalLaps).
					Int("clients", len(w.unifiedClients)).
					Msg("broadcast race info to unified clients")
			}

		case logStats := <-w.logStatsFeed:
			// Store current state with mutex protection
			w.logStatsMutex.Lock()
			w.currentLogStats = logStats
			w.logStatsMutex.Unlock()

			// Broadcast log stats
			msg := WSMessage{
				Type:      "logStats",
				Timestamp: time.Now().UnixMilli(),
				Data:      logStats,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode log stats JSON")

				continue
			}

			w.broadcastToUnifiedClients(encodedData, "logStats")

			totalCount, _ := logStats["totalCount"].(int)
			w.log.Debug().
				Int("totalCount", totalCount).
				Int("clients", len(w.unifiedClients)).
				Msg("broadcast log stats to unified clients")
		}
	}
}

// broadcastToUnifiedClients sends a message to subscribed unified websocket clients.
// messageType specifies what type of data is being sent (e.g., "telemetry", "vehicle", etc.)
func (w *WebUI) broadcastToUnifiedClients(encodedData []byte, messageType string) {
	activeClients := make([]*websocket.Conn, 0, len(w.unifiedClients))

	w.subscriptionsMutex.RLock()
	defer w.subscriptionsMutex.RUnlock()

	for _, client := range w.unifiedClients {
		// Check if client is subscribed to this message type
		if subs, exists := w.clientSubscriptions[client]; exists && !subs[messageType] {
			// Client not subscribed to this type, skip
			activeClients = append(activeClients, client)

			continue
		}

		// Set reasonable write deadline for high-latency connections (3 seconds)
		_ = client.SetWriteDeadline(time.Now().Add(3 * time.Second))

		err := client.WriteMessage(websocket.TextMessage, encodedData)
		if err != nil {
			w.log.Debug().Err(err).Msg("failed to send message to unified client, removing")

			_ = client.Close()
		} else {
			activeClients = append(activeClients, client)
		}
	}

	w.unifiedClients = activeClients
}

// raceInfoBroadcaster was removed - race data is now sent through unified WebSocket broadcaster.
// Race info broadcasts are handled in unifiedWebSocketBroadcaster via the raceInfoFeed channel.

// handleRaceWebSocketConnection was removed - race data is now sent through unified /ws endpoint.

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
			"enableReplay": w.config.GetHapticsEnableReplay(),
			"webUIEnabled": w.config.GetAppWebUIEnabled(),
			"webUIPort":    w.config.GetAppWebUIPort(),
		},
		"discord": map[string]any{
			"token":          w.config.GetDiscordToken(),
			"guildID":        w.config.GetDiscordGuildID(),
			"channelID":      w.config.GetDiscordChannelID(),
			"voiceChannelID": w.config.GetDiscordVoiceChannelID(),
		},
		"fuel": map[string]any{
			"monitoringEnabled":       w.config.GetPitRadioFuelMonitoringEnabled(),
			"preWarnNotifyLaps":       w.config.GetPitRadioFuelPreWarnNotifyLaps(),
			"strategyNotifyLaps":      w.config.GetPitRadioFuelStrategyNotifyLaps(),
			"rangeSafetyMarginLaps":   w.config.GetPitRadioFuelRangeSafetyMarginLaps(),
			"rangeSafetyMarginMeters": w.config.GetPitRadioFuelRangeSafetyMarginMeters(),
		},
		"hardware": map[string]any{
			"model":              w.config.GetHardwareModel(),
			"displayOrientation": w.config.GetDisplayOrientation(),
		},
		"haptics": map[string]any{
			"dynamicTransmissionFeedback":  w.config.GethapticsDynamicTransFeedbackEnabled(),
			"dynamicTransmissionCurve":     w.config.GetHapticsTransmissionCurve(),
			"dynamicTransmissionGforceMax": w.config.GetHapticsTransmissionGforceMax(),
			"jerkCurve":                    w.config.GethapticsJerkCurve(),
			"jerkMax":                      w.config.GetHapticsJerkMax(),
			"snapCurve":                    w.config.GetHapticsSnapCurve(),
			"snapMax":                      w.config.GetHapticsSnapMax(),
			"pulseMaxAmplitude":            w.config.GetHapticsPulseMaxAmplitude(),
			"pulseMaxFrequencyHz":          w.config.GetHapticsPulseMaxHz(),
			"pulseMinFrequencyHz":          w.config.GetHapticsPulseMinHz(),
		},
		"pitRadio": map[string]any{
			"enabled":               w.config.PitRadioEnabled(),
			"messageSendIntervalMs": w.config.GetPitRadioMessageSendIntervalMs(),
			"notifications": map[string]any{
				"raceProgressEnabled":     w.config.GetPitRadioNotifyRaceProgressEnabled(),
				"raceProgressMinLaps":     w.config.GetPitRadioNotifyRaceProgressMinLaps(),
				"raceProgressIntervalPc":  w.config.GetPitRadioNotifyRaceProgressIntervalPc(),
				"raceLapsEnabled":         w.config.GetPitRadioNotifyRaceLapsEnabled(),
				"raceLapsIntervalLaps":    w.config.GetPitRadioNotifyRaceLapsIntervalLaps(),
				"raceLapsCountdownLaps":   w.config.GetPitRadioNotifyRaceLapsCountdownLaps(),
				"lapTimesEnabled":         w.config.GetPitRadioNotifyLapTimesEnabled(),
				"lapTimesMaxDeltaSeconds": w.config.GetPitRadioNotifyLapTimesMaxDeltaSeconds(),
			},
			"discord": map[string]any{
				"token":          w.config.GetDiscordToken(),
				"guildID":        w.config.GetDiscordGuildID(),
				"channelID":      w.config.GetDiscordChannelID(),
				"voiceChannelID": w.config.GetDiscordVoiceChannelID(),
			},
		},
		"synthesizer": map[string]any{
			"internalSampleRateHz":      w.config.GetSynthInternalSampleRateHz(),
			"outputSampleRateHz":        w.config.GetSynthOutputSampleRateHz(),
			"outputFile":                w.config.GetSynthOutputFile(),
			"masterMute":                w.config.GetSynthMasterMute(),
			"masterGain":                w.config.GetSynthMasterGain(),
			"chassisMute":               w.config.GetSynthChassisMute(),
			"chassisGain":               w.config.GetSynthChassisGain(),
			"transmissionMute":          w.config.GetSynthTransmissionMute(),
			"transmissionGain":          w.config.GetSynthTransmissionGain(),
			"transmissionGainMinRace":   w.config.GetSynthTransmissionGainMinRace(),
			"transmissionGainMinStreet": w.config.GetSynthTransmissionGainMinStreet(),
			"engineMute":                w.config.GetSynthEngineMute(),
			"engineGain":                w.config.GetSynthEngineGain(),
			"gainIncrement":             w.config.GetSynthGainIncrement(),
			"engineProfiles":            w.config.GetSynthEngineProfiles(),
			"eqEnabled":                 w.config.GetSynthEqEnabled(),
			"eq":                        w.config.GetSynthEq(),
		},
		"eqCurve": func() map[string]any {
			curve, minFreq, resolution := w.config.GetSynthEqCurve()

			return map[string]any{
				"curve":      curve,
				"minFreq":    minFreq,
				"resolution": resolution,
			}
		}(),
		"telemetry": map[string]any{
			"source": w.config.GetTelemetrySource(),
		},
		"tyres": map[string]any{
			"monitoringEnabled":          w.config.GetPitRadioTyreMonitoringEnabled(),
			"temperatureOptimalCelsius":  w.config.GetPitRadioTyreTemperatureOptimalCelsius(),
			"temperatureOperatingWindow": w.config.GetPitRadioTyreTemperatureOperatingWindow(),
			"temperatureMarginCelsius":   w.config.GetPitRadioTyreTemperatureMarginCelsius(),
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

	// Check if restart is required BEFORE applying changes
	restartRequired := w.checkRestartRequired(configData)

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

	w.log.Debug().Interface("config", configData).Msg("configuration updated successfully")

	// Save configuration to file
	err = w.config.SaveConfigToFile()
	if err != nil {
		w.log.Error().Err(err).Msg("failed to save configuration to file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Configuration updated but failed to save: " + err.Error(),
		})

		return
	}

	w.log.Debug().Msg("configuration saved to file")

	// Return success response with updated config including EQ curve
	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"status":          "success",
		"message":         "Configuration updated and saved successfully",
		"restartRequired": restartRequired,
		"config": map[string]any{
			"eqCurve": func() map[string]any {
				curve, minFreq, resolution := w.config.GetSynthEqCurve()

				return map[string]any{
					"curve":      curve,
					"minFreq":    minFreq,
					"resolution": resolution,
				}
			}(),
		},
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
		case "hardware":
			errors = append(errors, w.applyHardwareConfig(sectionMap)...)
		case "telemetry":
			errors = append(errors, w.applyTelemetryConfig(sectionMap)...)
		case "discord":
			errors = append(errors, w.applyDiscordConfig(sectionMap)...)
		case "pitRadio":
			errors = append(errors, w.applyPitRadioConfig(sectionMap)...)
		case "tyres":
			errors = append(errors, w.applyTyresConfig(sectionMap)...)
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

	if accent, ok := config["accent"]; ok {
		if accentStr, ok := accent.(string); ok {
			w.config.SetAppAccent(accentStr)
		} else {
			errors = append(errors, "invalid accent value")
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
				_, err := os.Stat(vehicleDBFileStr)
				if err != nil {
					if os.IsNotExist(err) {
						errors = append(errors, "vehicle database file not found: "+vehicleDBFileStr)
					} else {
						errors = append(errors, "cannot access vehicle database file: "+err.Error())
					}

					return errors
				}
			}

			w.config.SetAppVehicleDBFile(vehicleDBFileStr)
		} else {
			errors = append(errors, "invalid vehicle database file value")
		}
	}

	if enableReplay, ok := config["enableReplay"]; ok {
		if replayBool, ok := enableReplay.(bool); ok {
			w.config.SetHapticsEnableReplay(replayBool)
		} else {
			errors = append(errors, "invalid replay mode value")
		}
	}

	return errors
}

// applySynthesizerConfig applies synthesizer configuration changes.
func (w *WebUI) applySynthesizerConfig(config map[string]any) []string {
	var errors []string

	if internalSampleRate, ok := config["internalSampleRateHz"]; ok {
		if rateFloat, ok := internalSampleRate.(float64); ok {
			w.config.SetSynthInternalSampleRateHz(int(rateFloat))
		} else {
			errors = append(errors, "invalid internal sample rate value")
		}
	}

	if outputSampleRate, ok := config["outputSampleRateHz"]; ok {
		if rateFloat, ok := outputSampleRate.(float64); ok {
			w.config.SetSynthOutputSampleRateHz(int(rateFloat))
		} else {
			errors = append(errors, "invalid output sample rate value")
		}
	}

	if masterGain, ok := config["masterGain"]; ok {
		if gainFloat, ok := masterGain.(float64); ok {
			w.config.SetSynthMasterGain(gainFloat)
		} else {
			errors = append(errors, "invalid master gain value")
		}
	}

	if masterMute, ok := config["masterMute"]; ok {
		if mute, ok := masterMute.(bool); ok {
			w.config.SetSynthMasterMute(mute)
		} else {
			errors = append(errors, "invalid master gain mute value")
		}
	}

	if chassisGain, ok := config["chassisGain"]; ok {
		if gainFloat, ok := chassisGain.(float64); ok {
			w.config.SetSynthChassisGain(gainFloat)
		} else {
			errors = append(errors, "invalid chassis gain value")
		}
	}

	if chassisMute, ok := config["chassisMute"]; ok {
		if mute, ok := chassisMute.(bool); ok {
			w.config.SetSynthChassisMute(mute)
		} else {
			errors = append(errors, "invalid chassis gain mute value")
		}
	}

	if transmissionGain, ok := config["transmissionGain"]; ok {
		if gainFloat, ok := transmissionGain.(float64); ok {
			w.config.SetSynthTransmissionGain(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain value")
		}
	}

	if transmissionMute, ok := config["transmissionMute"]; ok {
		if mute, ok := transmissionMute.(bool); ok {
			w.config.SetSynthTransmissionMute(mute)
		} else {
			errors = append(errors, "invalid transmission gain mute value")
		}
	}

	if transmissionGainMinRace, ok := config["transmissionGainMinRace"]; ok {
		if gainFloat, ok := transmissionGainMinRace.(float64); ok {
			w.config.SetSynthTransmissionGainMinRace(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain min race value")
		}
	}

	if transmissionGainMinStreet, ok := config["transmissionGainMinStreet"]; ok {
		if gainFloat, ok := transmissionGainMinStreet.(float64); ok {
			w.config.SetSynthTransmissionGainMinStreet(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain min street value")
		}
	}

	if engineGain, ok := config["engineGain"]; ok {
		if gainFloat, ok := engineGain.(float64); ok {
			w.config.SetSynthEngineGain(gainFloat)
		} else {
			errors = append(errors, "invalid engine gain value")
		}
	}

	if engineMute, ok := config["engineMute"]; ok {
		if mute, ok := engineMute.(bool); ok {
			w.config.SetSynthEngineMute(mute)
		} else {
			errors = append(errors, "invalid engine gain mute value")
		}
	}

	if gainIncrement, ok := config["gainIncrement"]; ok {
		if incrementFloat, ok := gainIncrement.(float64); ok {
			w.config.SetSynthGainIncrement(incrementFloat)
		} else {
			errors = append(errors, "invalid gain increment value")
		}
	}

	// Handle engine profiles
	if engineProfiles, ok := config["engineProfiles"]; ok {
		if profilesMap, ok := engineProfiles.(map[string]any); ok {
			for name, profileData := range profilesMap {
				if profileMap, ok := profileData.(map[string]any); ok {
					profile := appHaptics.EngineProfile{}

					if pb, ok := profileMap["PrimaryBalance"].(float64); ok {
						profile.PrimaryBalance = pb
					}

					if sb, ok := profileMap["SecondaryBalance"].(float64); ok {
						profile.SecondaryBalance = sb
					}

					if g, ok := profileMap["Gain"].(float64); ok {
						profile.Gain = g
					}

					if ps, ok := profileMap["PulseScale"].(float64); ok {
						profile.PulseScale = ps
					}

					w.config.SetSynthEngineProfile(name, profile)
				}
			}
		} else {
			errors = append(errors, "invalid engine profiles format")
		}
	}

	if eqEnabled, ok := config["eqEnabled"]; ok {
		if enabled, ok := eqEnabled.(bool); ok {
			w.config.SetSynthEqEnabled(enabled)
		} else {
			errors = append(errors, "invalid EQ enabled value")
		}
	}

	// Handle EQ bands
	if eq, ok := config["eq"]; ok {
		if eqArray, ok := eq.([]any); ok {
			eqBands := make([]appconfig.EQBand, 0, len(eqArray))
			for idx, val := range eqArray {
				if bandMap, ok := val.(map[string]any); ok {
					freq, freqOk := bandMap["frequency"].(float64)
					gain, gainOk := bandMap["gain"].(float64)
					qVal, qOk := bandMap["q"].(float64)

					if !freqOk || !gainOk || !qOk {
						errors = append(errors, fmt.Sprintf("invalid EQ band %d: missing or invalid fields", idx+1))

						continue // Skip this band but continue processing others
					}

					eqBands = append(eqBands, appconfig.EQBand{
						Frequency: freq,
						Gain:      gain,
						Q:         qVal,
					})
				} else {
					errors = append(errors, fmt.Sprintf("invalid EQ band %d format", idx+1))

					continue
				}
			}

			if len(eqBands) == 8 {
				w.config.SetSynthEq(eqBands)
			} else {
				errors = append(errors, fmt.Sprintf("EQ must have exactly 8 bands, got %d", len(eqBands)))
			}
		} else {
			errors = append(errors, "invalid EQ format")
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

	// Helper for bool conversion
	parseBool := func(val any, key string) (bool, bool) {
		b, ok := val.(bool)
		if !ok {
			errors = append(errors, "invalid "+key+" value")
		}

		return b, ok
	}

	if dynamicTransmission, ok := config["dynamicTransmissionFeedback"]; ok {
		if dynamicBool, ok := parseBool(dynamicTransmission, "dynamic transmission feedback"); ok {
			w.config.SetHapticsDynamicTransFeedbackEnabled(dynamicBool)
		}
	}

	if jerkCurve, ok := config["jerkCurve"]; ok {
		if curveFloat, ok := parseFloat(jerkCurve, "jerk curve"); ok {
			w.config.SetHapticsJerkCurve(int(curveFloat * 1000.0))
		}
	}

	if jerkMax, ok := config["jerkMax"]; ok {
		if maxFloat, ok := parseFloat(jerkMax, "jerk max"); ok {
			w.config.SetHapticsJerkMax(int(maxFloat))
		}
	}

	if snapCurve, ok := config["snapCurve"]; ok {
		if curveFloat, ok := parseFloat(snapCurve, "snap curve"); ok {
			w.config.SetHapticsSnapCurve(int(curveFloat * 1000.0))
		}
	}

	if snapMax, ok := config["snapMax"]; ok {
		if maxFloat, ok := parseFloat(snapMax, "snap max"); ok {
			w.config.SetHapticsSnapMax(int(maxFloat))
		}
	}

	if transmissionCurve, ok := config["dynamicTransmissionCurve"]; ok {
		if curveFloat, ok := parseFloat(transmissionCurve, "transmission curve"); ok {
			w.config.SetHapticsTransmissionCurve(int(curveFloat * 1000.0))
		}
	}

	if transmissionGforceMax, ok := config["dynamicTransmissionGforceMax"]; ok {
		if gforceFloat, ok := parseFloat(transmissionGforceMax, "transmission G-force max"); ok {
			w.config.SetHapticsTransmissionGforceMax(gforceFloat)
		}
	}

	if pulseMaxAmplitude, ok := config["pulseMaxAmplitude"]; ok {
		if amplitudeFloat, ok := parseFloat(pulseMaxAmplitude, "pulse max amplitude"); ok {
			w.config.SetHapticsPulseMaxAmplitude(amplitudeFloat)
		}
	}

	if pulseMaxFreq, ok := config["pulseMaxFrequencyHz"]; ok {
		if freqFloat, ok := parseFloat(pulseMaxFreq, "pulse max frequency"); ok {
			w.config.SetHapticsPulseMaxFrequencyHz(freqFloat)
		}
	}

	if pulseMinFreq, ok := config["pulseMinFrequencyHz"]; ok {
		if freqFloat, ok := parseFloat(pulseMinFreq, "pulse min frequency"); ok {
			w.config.SetHapticsPulseMinFrequencyHz(freqFloat)
		}
	}

	return errors
}

// checkRestartRequired checks if any configuration changes require a restart.
func (w *WebUI) checkRestartRequired(configData map[string]any) bool {
	// Check if vehicleDBFile changed
	if appConfig, ok := configData["app"].(map[string]any); ok { //nolint:nestif // compact nesting
		if vehicleDBFile, ok := appConfig["vehicleDBFile"]; ok {
			if vehicleDBFileStr, ok := vehicleDBFile.(string); ok {
				if vehicleDBFileStr != w.config.GetAppVehicleDBFile() {
					return true
				}
			}
		}
	}

	// Check if telemetry source changed
	if telemetryConfig, ok := configData["telemetry"].(map[string]any); ok { //nolint:nestif // compact nesting
		if source, ok := telemetryConfig["source"]; ok {
			if sourceStr, ok := source.(string); ok {
				if sourceStr != w.config.GetTelemetrySource() {
					return true
				}
			}
		}
	}

	return false
}

// applyFuelConfig applies fuel management configuration changes.
func (w *WebUI) applyFuelConfig(config map[string]any) []string {
	var errors []string

	if monitoringEnabled, ok := config["monitoringEnabled"]; ok {
		if enabledBool, ok := monitoringEnabled.(bool); ok {
			w.config.SetPitRadioFuelMonitoringEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid fuel monitoring enabled value")
		}
	}

	if preWarnLaps, ok := config["preWarnNotifyLaps"]; ok {
		if lapsFloat, ok := preWarnLaps.(float64); ok {
			w.config.SetPitRadioFuelPreWarnNotifyLaps(lapsFloat)
		} else {
			errors = append(errors, "invalid pre-warn notify laps value")
		}
	}

	if strategyLaps, ok := config["strategyNotifyLaps"]; ok {
		if lapsFloat, ok := strategyLaps.(float64); ok {
			w.config.SetPitRadioFuelStrategyNotifyLaps(lapsFloat)
		} else {
			errors = append(errors, "invalid strategy notify laps value")
		}
	}

	if safetyMarginLaps, ok := config["rangeSafetyMarginLaps"]; ok {
		if marginFloat, ok := safetyMarginLaps.(float64); ok {
			w.config.SetPitRadioFuelRangeSafetyMarginLaps(marginFloat)
		} else {
			errors = append(errors, "invalid range safety margin laps value")
		}
	}

	if safetyMarginMeters, ok := config["rangeSafetyMarginMeters"]; ok {
		if marginFloat, ok := safetyMarginMeters.(float64); ok {
			w.config.SetPitRadioFuelRangeSafetyMarginMeters(marginFloat)
		} else {
			errors = append(errors, "invalid range safety margin meters value")
		}
	}

	return errors
}

// applyHardwareConfig applies hardware configuration changes.
func (w *WebUI) applyHardwareConfig(config map[string]any) []string {
	var errors []string

	if model, ok := config["model"]; ok {
		if modelStr, ok := model.(string); ok {
			w.config.SetHardwareModel(modelStr)
		} else {
			errors = append(errors, "invalid hardware model value")
		}
	}

	if orientation, ok := config["displayOrientation"]; ok {
		if orientFloat, ok := orientation.(float64); ok {
			w.config.SetDisplayOrientation(int(orientFloat))
		} else {
			errors = append(errors, "invalid display orientation value")
		}
	}

	return errors
}

// applyTelemetryConfig applies telemetry configuration changes.
func (w *WebUI) applyTelemetryConfig(config map[string]any) []string {
	var errors []string

	source, cfgOK := config["source"]
	if !cfgOK {
		return errors
	}

	sourceStr, strOK := source.(string)
	if !strOK {
		errors = append(errors, "invalid telemetry source value")

		return errors
	}

	// If source is a file:// path, validate that the file exists
	if after, cutOK := strings.CutPrefix(sourceStr, "file://"); cutOK {
		filePath := after

		_, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				errors = append(errors, "telemetry replay file not found: "+filePath)
			} else {
				errors = append(errors, "cannot access telemetry replay file: "+err.Error())
			}

			return errors
		}
	}

	w.config.SetTelemetrySource(sourceStr)

	return errors
}

// applyDiscordConfig applies Discord configuration changes.
func (w *WebUI) applyDiscordConfig(config map[string]any) []string {
	var errors []string

	if token, ok := config["token"]; ok {
		if tokenStr, ok := token.(string); ok {
			w.config.SetDiscordToken(tokenStr)
		} else {
			errors = append(errors, "invalid discord token value")
		}
	}

	if guildID, ok := config["guildID"]; ok {
		if guildIDStr, ok := guildID.(string); ok {
			w.config.SetDiscordGuildID(guildIDStr)
		} else {
			errors = append(errors, "invalid discord guild ID value")
		}
	}

	if channelID, ok := config["channelID"]; ok {
		if channelIDStr, ok := channelID.(string); ok {
			w.config.SetDiscordChannelID(channelIDStr)
		} else {
			errors = append(errors, "invalid discord channel ID value")
		}
	}

	if voiceChannelID, ok := config["voiceChannelID"]; ok {
		if voiceChannelIDStr, ok := voiceChannelID.(string); ok {
			w.config.SetDiscordVoiceChannelID(voiceChannelIDStr)
		} else {
			errors = append(errors, "invalid discord voice channel ID value")
		}
	}

	return errors
}

// applyNotificationsConfig applies notifications configuration changes.
func (w *WebUI) applyNotificationsConfig(config map[string]any) []string {
	var errors []string

	if raceProgressEnabled, ok := config["raceProgressEnabled"]; ok {
		if enabledBool, ok := raceProgressEnabled.(bool); ok {
			w.config.SetPitRadioNotifyRaceProgressEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid race progress enabled value")
		}
	}

	if raceProgressMinLaps, ok := config["raceProgressMinLaps"]; ok {
		if lapsFloat, ok := raceProgressMinLaps.(float64); ok {
			w.config.SetPitRadioNotifyRaceProgressMinLaps(int(lapsFloat))
		} else {
			errors = append(errors, "invalid race progress min laps value")
		}
	}

	if raceProgressIntervalPc, ok := config["raceProgressIntervalPc"]; ok {
		if intervalFloat, ok := raceProgressIntervalPc.(float64); ok {
			w.config.SetPitRadioNotifyRaceProgressIntervalPc(int(intervalFloat))
		} else {
			errors = append(errors, "invalid race progress interval percentage value")
		}
	}

	if raceLapsEnabled, ok := config["raceLapsEnabled"]; ok {
		if enabledBool, ok := raceLapsEnabled.(bool); ok {
			w.config.SetPitRadioNotifyRaceLapsEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid race laps enabled value")
		}
	}

	if raceLapsIntervalLaps, ok := config["raceLapsIntervalLaps"]; ok {
		if intervalFloat, ok := raceLapsIntervalLaps.(float64); ok {
			w.config.SetPitRadioNotifyRaceLapsIntervalLaps(int(intervalFloat))
		} else {
			errors = append(errors, "invalid race laps interval laps value")
		}
	}

	if raceLapsCountdownLaps, ok := config["raceLapsCountdownLaps"]; ok {
		if countdownFloat, ok := raceLapsCountdownLaps.(float64); ok {
			w.config.SetPitRadioNotifyRaceLapsCountdownLaps(int(countdownFloat))
		} else {
			errors = append(errors, "invalid race laps countdown laps value")
		}
	}

	if lapTimesEnabled, ok := config["lapTimesEnabled"]; ok {
		if enabledBool, ok := lapTimesEnabled.(bool); ok {
			w.config.SetPitRadioNotifyLapTimesEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid lap times enabled value")
		}
	}

	if lapTimesMaxDelta, ok := config["lapTimesMaxDeltaSeconds"]; ok {
		if deltaFloat, ok := lapTimesMaxDelta.(float64); ok {
			w.config.SetPitRadioNotifyLapTimesMaxDeltaSeconds(deltaFloat)
		} else {
			errors = append(errors, "invalid lap times max delta seconds value")
		}
	}

	return errors
}

// applyPitRadioConfig applies pit radio configuration changes.
func (w *WebUI) applyPitRadioConfig(config map[string]any) []string {
	var errors []string

	if enabled, ok := config["enabled"]; ok {
		if enabledBool, ok := enabled.(bool); ok {
			w.config.SetPitRadioEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid pit radio enabled value")
		}
	}

	if intervalMs, ok := config["messageSendIntervalMs"]; ok {
		if intervalFloat, ok := intervalMs.(float64); ok {
			w.config.SetPitRadioMessageSendIntervalMs(int(intervalFloat))
		} else {
			errors = append(errors, "invalid message send interval value")
		}
	}

	// Handle nested notifications configuration
	if notificationsConfig, ok := config["notifications"]; ok {
		if notificationsMap, ok := notificationsConfig.(map[string]any); ok {
			notificationsErrors := w.applyNotificationsConfig(notificationsMap)
			errors = append(errors, notificationsErrors...)
		} else {
			errors = append(errors, "invalid notifications configuration structure")
		}
	}

	// Handle nested Discord configuration
	if discordConfig, ok := config["discord"]; ok {
		if discordMap, ok := discordConfig.(map[string]any); ok {
			discordErrors := w.applyDiscordConfig(discordMap)
			errors = append(errors, discordErrors...)
		} else {
			errors = append(errors, "invalid discord configuration structure")
		}
	}

	return errors
}

// applyTyresConfig applies tyre management configuration changes.
func (w *WebUI) applyTyresConfig(config map[string]any) []string {
	var errors []string

	if monitoringEnabled, ok := config["monitoringEnabled"]; ok {
		if enabledBool, ok := monitoringEnabled.(bool); ok {
			w.config.SetPitRadioTyreMonitoringEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid tyre monitoring enabled value")
		}
	}

	if tempOptimal, ok := config["temperatureOptimalCelsius"]; ok {
		if tempFloat, ok := tempOptimal.(float64); ok {
			w.config.SetPitRadioTyreTemperatureOptimalCelsius(float32(tempFloat))
		} else {
			errors = append(errors, "invalid temperature optimal value")
		}
	}

	if tempWindow, ok := config["temperatureOperatingWindow"]; ok {
		if windowFloat, ok := tempWindow.(float64); ok {
			w.config.SetPitRadioTyreTemperatureOperatingWindow(float32(windowFloat))
		} else {
			errors = append(errors, "invalid temperature operating window value")
		}
	}

	if tempMargin, ok := config["temperatureMarginCelsius"]; ok {
		if marginFloat, ok := tempMargin.(float64); ok {
			w.config.SetPitRadioTyreTemperatureMarginCelsius(float32(marginFloat))
		} else {
			errors = append(errors, "invalid temperature margin value")
		}
	}

	return errors
}

// handleConfigStatus returns the configuration status including last update timestamp and restart required flag.
func (w *WebUI) handleConfigStatus(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	status := w.config.Status()

	statusData := map[string]any{
		"lastUpdate":      status.LastUpdate,
		"restartRequired": status.RestartRequired,
	}

	err := json.NewEncoder(response).Encode(statusData)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to encode config status JSON")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to encode status"}) //nolint:errchkjson // simple encoding
	}
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

	// Save the default configuration to disk
	err := w.config.SaveConfigToFile()
	if err != nil {
		w.log.Error().Err(err).Msg("failed to save default configuration to file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to save configuration"}) //nolint:errchkjson // simple encoding

		return
	}

	// Mark that a restart is required
	w.config.MarkRestartRequired()

	w.handleGetConfig(response, request)
}

// handleConfigExport handles GET requests to export the full configuration file from disk.
func (w *WebUI) handleConfigExport(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	// Get the config file path
	configFilePath := w.config.GetConfigFilePath()

	// Read the config file from disk
	configData, err := os.ReadFile(configFilePath)
	if err != nil {
		w.log.Error().Err(err).Str("file", configFilePath).Msg("failed to read config file")
		response.WriteHeader(http.StatusInternalServerError)
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to read configuration file"}) //nolint:errchkjson // simple encoding

		return
	}

	// Set headers for file download
	response.Header().Set("Content-Type", "application/toml")
	response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"simtezilo-config-%s.conf\"",
		time.Now().Format("20060102-150405")))

	// Write the config file content
	_, err = response.Write(configData)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to write config data to response")
	}
}

// handleConfigImport handles POST requests to import and validate a configuration file.
func (w *WebUI) handleConfigImport(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	// Parse the multipart form with a size limit (10 MB)
	err := request.ParseMultipartForm(10 << 20)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to parse multipart form")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to parse form data"}) //nolint:errchkjson // simple encoding

		return
	}

	// Get the file from the form
	file, header, err := request.FormFile("config")
	if err != nil {
		w.log.Error().Err(err).Msg("failed to get file from form")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "No config file provided"}) //nolint:errchkjson // simple encoding

		return
	}
	defer file.Close()

	w.log.Info().Str("filename", header.Filename).Int64("size", header.Size).Msg("config import requested")

	// Read the file content
	fileContent, err := io.ReadAll(file)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to read uploaded file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to read uploaded file"}) //nolint:errchkjson // simple encoding

		return
	}

	// Validate the TOML content by attempting to unmarshal it
	var testConfig map[string]any

	err = toml.Unmarshal(fileContent, &testConfig)
	if err != nil {
		w.log.Error().Err(err).Msg("invalid TOML format")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": fmt.Sprintf("Invalid TOML format: %v", err)}) //nolint:errchkjson // simple encoding

		return
	}

	// Load and validate the configuration using viper and our validation logic
	cfg := viper.New()
	cfg.SetConfigType("toml")

	err = cfg.ReadConfig(bytes.NewReader(fileContent))
	if err != nil {
		w.log.Error().Err(err).Msg("failed to parse config with viper")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": fmt.Sprintf("Invalid configuration format: %v", err)}) //nolint:errchkjson // simple encoding

		return
	}

	// Create a temporary config instance to validate the structure and values
	tempConfig := &appconfig.Config{}

	err = cfg.Unmarshal(tempConfig)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to unmarshal config")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": fmt.Sprintf("Configuration structure error: %v", err)}) //nolint:errchkjson // simple encoding

		return
	}

	// Run comprehensive validation
	validationResult := w.config.ValidateConfig(fileContent)
	if !validationResult.Valid {
		w.log.Warn().Interface("errors", validationResult.Errors).Msg("config validation failed")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"error":            "Configuration validation failed",
			"validationErrors": validationResult.Errors,
		})

		return
	}

	// Create a backup of the current config file before overwriting
	backupPath, err := w.config.BackupConfigFile()
	if err != nil {
		w.log.Warn().Err(err).Msg("failed to backup current config (continuing anyway)")
	} else {
		w.log.Info().Str("backup", backupPath).Msg("created config backup")
	}

	// Write the validated config to the config file
	configFilePath := w.config.GetConfigFilePath()

	err = os.WriteFile(configFilePath, fileContent, 0o600)
	if err != nil {
		w.log.Error().Err(err).Str("file", configFilePath).Msg("failed to write config file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to write configuration file"}) //nolint:errchkjson // simple encoding

		return
	}

	w.log.Info().Str("file", configFilePath).Msg("config file imported successfully")

	// Mark that a restart is required
	w.config.MarkRestartRequired()

	// Return success response
	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Configuration imported successfully. Please restart the application for changes to take effect.",
		"backup":  backupPath,
	})
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

// handleSetupMode handles POST requests to activate setup mode.
func (w *WebUI) handleSetupMode(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

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
	response.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

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

// handleSystemInfo handles GET requests for system information including build info and hardware platform.
func (w *WebUI) handleSystemInfo(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	platform := hardware.Platform()

	// Check current setup mode availability by calling setup binary
	setupModeAvailable := w.setupModeEnabled // Default to cached value
	setupBinPath := filepath.Join(w.config.GetAppBaseDir(), "bin", "setup")
	cmd := exec.CommandContext(request.Context(), setupBinPath, "status")

	output, err := cmd.CombinedOutput()
	if err != nil {
		w.log.Warn().
			Err(err).
			Str("path", setupBinPath).
			Str("output", string(output)).
			Msg("failed to check setup status, using cached value")
	} else {
		// Parse the JSON output to get the available status
		var statusResponse struct {
			Status struct {
				Available bool `json:"available"` //nolint:tagliatelle // lowercase for interface simpicity
			} `json:"status"` //nolint:tagliatelle
		}

		err := json.Unmarshal(output, &statusResponse)
		if err != nil {
			w.log.Warn().
				Err(err).
				Str("output", string(output)).
				Msg("failed to parse setup status response")
		} else {
			setupModeAvailable = statusResponse.Status.Available
			w.log.Debug().
				Bool("available", setupModeAvailable).
				Msg("successfully checked setup mode availability")
		}
	}

	responseData := map[string]any{
		"version":            w.buildVersion,
		"buildTime":          w.buildTime,
		"buildPlatform":      w.buildPlatform,
		"hardware":           platform.String(),
		"setupModeAvailable": setupModeAvailable,
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	err = json.NewEncoder(response).Encode(responseData)
	if err != nil {
		w.log.Error().Err(err).Msg("failed to encode system info response")
		http.Error(response, "error encoding system info", http.StatusInternalServerError)

		return
	}

	w.log.Debug().Bool("setupModeAvailable", setupModeAvailable).Msg("served system info")
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
		p, err := fmt.Sscanf(pageStr, "%d", &page)
		if err == nil && p == 1 {
			if page < 1 {
				page = 1
			}
		}
	}

	if pageSizeStr := queryParams.Get("pageSize"); pageSizeStr != "" {
		ps, err := fmt.Sscanf(pageSizeStr, "%d", &pageSize)
		if err == nil && ps == 1 {
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

// handleCacheSize returns the size of the cache directory.
func (w *WebUI) handleCacheSize(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	baseDir := w.config.GetAppBaseDir()
	cacheDir := filepath.Join(baseDir, "data", "cache")

	size, count, err := w.getDirSize(cacheDir)
	if err != nil {
		w.log.Error().Err(err).Str("cache_dir", cacheDir).Msg("failed to get cache size")
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"success": false,
			"message": "Failed to get cache size",
			"size":    "Error",
		})

		return
	}

	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"success": true,
		"size":    formatBytes(size),
		"bytes":   size,
		"count":   count,
	})
}

// handleCacheClear clears all files in the cache directory.
func (w *WebUI) handleCacheClear(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	baseDir := w.config.GetAppBaseDir()
	cacheDir := filepath.Join(baseDir, "data", "cache")

	w.log.Info().Str("cache_dir", cacheDir).Msg("clearing cache directory")

	// Read directory contents
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		w.log.Error().Err(err).Str("cache_dir", cacheDir).Msg("failed to read cache directory")
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"success": false,
			"message": "Failed to read cache directory",
		})

		return
	}

	// Delete all files and directories
	deletedCount := 0
	errorCount := 0

	for _, entry := range entries {
		entryPath := filepath.Join(cacheDir, entry.Name())

		err := os.RemoveAll(entryPath)
		if err != nil {
			w.log.Error().Err(err).Str("path", entryPath).Msg("failed to delete cache entry")

			errorCount++
		} else {
			deletedCount++
		}
	}

	w.log.Info().Int("deleted", deletedCount).Int("errors", errorCount).Msg("cache clear completed")

	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"success": errorCount == 0,
		"message": fmt.Sprintf("Deleted %d items", deletedCount),
		"deleted": deletedCount,
		"errors":  errorCount,
	})
}

// getDirSize returns the total size of all files in a directory (recursive).
func (w *WebUI) getDirSize(path string) (int64, int, error) {
	var (
		size  int64
		count int
	)

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			w.log.Warn().Err(err).Str("path", filePath).Msg("Get directory size")

			return nil // Skip files that can't be accessed
		}

		if !info.IsDir() {
			size += info.Size()
			count++
		}

		return nil
	})

	w.log.Debug().Int64("size_bytes", size).Int("count", count).Str("path", path).Msg("Get directory size")

	return size, count, err
}

// formatBytes formats bytes into a human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
