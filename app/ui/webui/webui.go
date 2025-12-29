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

// WebUI defines the web user interface.
type WebUI struct {
	log                zerolog.Logger
	port               int
	webSocketClients   int
	telemetryChartFeed chan map[string]float32
	vehicleInfoFeed    chan map[string]any
	currentVehicleInfo map[string]any
	vehicleInfoMutex   sync.RWMutex
	vehicleClients     []*websocket.Conn
	vehicleClientsChan chan *websocket.Conn
	vehicleUnsubChan   chan *websocket.Conn
	vehicleSessions    map[string]*websocket.Conn // Track sessions to prevent duplicates
	vehicleSessionsMux sync.Mutex
	gameStateFeed      chan string
	currentGameState   string
	gameStateMutex     sync.RWMutex
	circuitInfoFeed    chan map[string]string
	currentCircuitInfo map[string]string
	circuitInfoMutex   sync.RWMutex
	circuitClients     []*websocket.Conn
	circuitClientsChan chan *websocket.Conn
	circuitUnsubChan   chan *websocket.Conn
	raceInfoFeed       chan map[string]any
	currentRaceInfo    map[string]any
	raceInfoMutex      sync.RWMutex
	raceClients        []*websocket.Conn
	raceClientsChan    chan *websocket.Conn
	raceUnsubChan      chan *websocket.Conn
	config             *appconfig.Config
	upgrader           websocket.Upgrader
	shutdownChan       chan exitcode.Code
	setupModeEnabled   bool
	logStore           *logstore.Store
	buildVersion       string
	buildTime          string
	buildPlatform      string
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
		vehicleClients:     make([]*websocket.Conn, 0),
		vehicleClientsChan: make(chan *websocket.Conn, 10),
		vehicleUnsubChan:   make(chan *websocket.Conn, 10),
		vehicleSessions:    make(map[string]*websocket.Conn),
		gameStateFeed:      config.GameStateFeed,
		currentGameState:   "unknown",
		circuitInfoFeed:    config.CircuitInfoFeed,
		currentCircuitInfo: make(map[string]string),
		circuitClients:     make([]*websocket.Conn, 0),
		circuitClientsChan: make(chan *websocket.Conn, 10),
		circuitUnsubChan:   make(chan *websocket.Conn, 10),
		raceInfoFeed:       config.RaceInfoFeed,
		currentRaceInfo:    make(map[string]any),
		raceClients:        make([]*websocket.Conn, 0),
		raceClientsChan:    make(chan *websocket.Conn, 10),
		raceUnsubChan:      make(chan *websocket.Conn, 10),
		config:             config.Config,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		shutdownChan:     config.ShutdownChan,
		setupModeEnabled: config.SetupModeAvailable,
		logStore:         config.LogStore,
		buildVersion:     config.BuildVersion,
		buildTime:        config.BuildTime,
		buildPlatform:    config.BuildPlatform,
	}

	// Start vehicle info broadcaster
	go webUI.vehicleInfoBroadcaster()

	// Start circuit info broadcaster
	go webUI.circuitInfoBroadcaster()

	// Start race info broadcaster
	go webUI.raceInfoBroadcaster()

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
	mux.HandleFunc("/ws/vehicle", w.handleVehicleWebSocketConnection)
	mux.HandleFunc("/ws/circuit", w.handleCircuitWebSocketConnection)
	mux.HandleFunc("/ws/race", w.handleRaceWebSocketConnection)
	mux.HandleFunc("/api/config", w.handleConfigAPI)
	mux.HandleFunc("/api/config/status", w.handleConfigStatus)
	mux.HandleFunc("/api/config/reset", w.handleConfigReset)
	mux.HandleFunc("/api/config/export", w.handleConfigExport)
	mux.HandleFunc("/api/config/import", w.handleConfigImport)
	mux.HandleFunc("/api/i18n", w.handleI18nAPI)
	mux.HandleFunc("/api/languages", w.handleLanguagesAPI)
	mux.HandleFunc("/api/logs", w.handleLogsAPI)
	mux.HandleFunc("/api/system/cache-clear", w.handleCacheClear)
	mux.HandleFunc("/api/system/cache-size", w.handleCacheSize)
	mux.HandleFunc("/api/system/info", w.handleSystemInfo)
	mux.HandleFunc("/api/restart", w.handleRestart)

	if w.setupModeEnabled {
		mux.HandleFunc("/api/mode/setup", w.handleSetupMode)
		mux.HandleFunc("/api/factory-reset", w.handleFactoryReset)
	}

	w.log.Debug().Msg("Web UI handler configured")

	return mux
}

// HasActiveClients returns true if there are active WebSocket clients connected.
func (w *WebUI) HasActiveClients() bool {
	return w.webSocketClients > 0 || len(w.vehicleClients) > 0 || len(w.circuitClients) > 0 || len(w.raceClients) > 0
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

	// Batch frames
	batchFrameRate := 30
	bufferSize := batchFrameRate / 60
	batchInterval := time.Duration(1000/batchFrameRate) * time.Millisecond

	batchBuffer := make([]map[string]float32, 0, bufferSize) // ~60fps * 0.25s = 15 frames

	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			if len(batchBuffer) == 0 {
				continue
			}

			// Send the batched frames
			encodedData, err := json.Marshal(batchBuffer)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode batched JSON data")

				continue
			}

			// Set write deadline
			_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))

			err = webSocket.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				failCount++

				w.log.Debug().Err(err).Msg("failed to send batched data to websocket")

				if failCount >= maxFailures {
					w.log.Error().Err(err).Str("reason", "too many failures").Msg("dropping websocket connection")

					_ = webSocket.Close()

					return
				}
			} else {
				failCount = 0
			}

			// Clear the buffer
			batchBuffer = batchBuffer[:0]
		}
	}()

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

		// Add to batch buffer instead of sending immediately
		batchBuffer = append(batchBuffer, data)
	}
}

// handleVehicleWebSocketConnection upgrades the HTTP connection to a WebSocket and streams vehicle info updates.
func (w *WebUI) handleVehicleWebSocketConnection(response http.ResponseWriter, request *http.Request) {
	// Get session ID from query parameter
	sessionID := request.URL.Query().Get("session")

	// Close any existing connection for this session
	if sessionID != "" {
		w.vehicleSessionsMux.Lock()

		if oldConn, exists := w.vehicleSessions[sessionID]; exists {
			w.log.Debug().Str("session", sessionID).Msg("closing old connection for session")

			_ = oldConn.Close()
			// Remove from clients list immediately
			w.vehicleUnsubChan <- oldConn
		}

		w.vehicleSessionsMux.Unlock()
	}

	webSocket, err := w.upgrader.Upgrade(response, request, nil)
	if err != nil {
		w.log.Error().Err(err).Msg("error upgrading vehicle websocket connection")

		return
	}

	w.log.Debug().Str("session", sessionID).Msg("vehicle websocket connection established")

	// Track this session
	if sessionID != "" {
		w.vehicleSessionsMux.Lock()
		w.vehicleSessions[sessionID] = webSocket
		w.vehicleSessionsMux.Unlock()
	}

	// Subscribe this client
	w.vehicleClientsChan <- webSocket

	defer func() {
		// Remove from session map
		if sessionID != "" {
			w.vehicleSessionsMux.Lock()
			delete(w.vehicleSessions, sessionID)
			w.vehicleSessionsMux.Unlock()
		}

		// Unsubscribe on disconnect
		w.vehicleUnsubChan <- webSocket

		err := webSocket.Close()
		if err != nil {
			w.log.Error().Err(err).Msg("closing vehicle websocket connection")
		}
	}()

	// Send current vehicle info immediately (with mutex protection)
	w.vehicleInfoMutex.RLock()

	if len(w.currentVehicleInfo) > 0 {
		encodedData, err := json.Marshal(w.currentVehicleInfo)
		w.vehicleInfoMutex.RUnlock()

		if err == nil {
			err = webSocket.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				w.log.Debug().Err(err).Msg("failed to send initial vehicle info")
			} else {
				w.log.Debug().Msg("sent initial vehicle info to new client")
			}
		}
	} else {
		w.vehicleInfoMutex.RUnlock()
	}

	// Send current game state immediately
	w.gameStateMutex.RLock()
	gameState := w.currentGameState
	w.gameStateMutex.RUnlock()

	if gameState != "" {
		gameStateInfo := map[string]any{
			"gamestate": gameState,
		}

		encodedData, err := json.Marshal(gameStateInfo)
		if err == nil {
			err = webSocket.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				w.log.Debug().Err(err).Msg("failed to send initial game state")
			} else {
				w.log.Debug().Msg("sent initial game state to new client")
			}
		}
	}

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
		w.log.Debug().Int("code", code).Str("text", text).Msg("client sent close frame")

		return nil
	})

	// Send pings every 5 seconds to detect dead connections
	pingTicker := time.NewTicker(5 * time.Second)
	defer pingTicker.Stop()

	done := make(chan struct{})

	// Goroutine to handle pings
	go func() {
		for {
			select {
			case <-pingTicker.C:
				_ = webSocket.SetWriteDeadline(time.Now().Add(3 * time.Second))

				err := webSocket.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					close(done)

					return
				}
			case <-done:
				return
			}
		}
	}()

	for {
		_, _, err := webSocket.ReadMessage()
		if err != nil {
			w.log.Debug().Err(err).Msg("vehicle websocket read error, closing connection")
			close(done)

			break
		}
		// Reset read deadline on any message
		_ = webSocket.SetReadDeadline(time.Now().Add(10 * time.Second))
	}
}

// vehicleInfoBroadcaster manages vehicle info broadcasting to all connected clients.
func (w *WebUI) vehicleInfoBroadcaster() {
	for {
		select {
		case client := <-w.vehicleClientsChan:
			// Add new client
			w.vehicleClients = append(w.vehicleClients, client)
			w.log.Debug().Int("vehicle_clients", len(w.vehicleClients)).Msg("vehicle client subscribed")

		case client := <-w.vehicleUnsubChan:
			// Remove client
			for i, c := range w.vehicleClients {
				if c == client {
					w.vehicleClients = append(w.vehicleClients[:i], w.vehicleClients[i+1:]...)

					break
				}
			}

			w.log.Debug().Int("vehicle_clients", len(w.vehicleClients)).Msg("vehicle client unsubscribed")

		case vehicleInfo := <-w.vehicleInfoFeed:
			// Store current state with mutex protection
			w.vehicleInfoMutex.Lock()
			w.currentVehicleInfo = vehicleInfo
			w.vehicleInfoMutex.Unlock()

			// Broadcast to all connected clients
			encodedData, err := json.Marshal(vehicleInfo)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode vehicle info JSON")

				continue
			}

			// Send to all clients, remove failed ones
			activeClients := make([]*websocket.Conn, 0, len(w.vehicleClients))
			for _, client := range w.vehicleClients {
				// Set reasonable write deadline for high-latency connections (3 seconds)
				_ = client.SetWriteDeadline(time.Now().Add(3 * time.Second))

				err := client.WriteMessage(websocket.TextMessage, encodedData)
				if err != nil {
					w.log.Debug().Err(err).Msg("failed to send vehicle info to client, removing")

					_ = client.Close()
				} else {
					activeClients = append(activeClients, client)
				}
			}

			w.vehicleClients = activeClients

			if len(vehicleInfo) > 0 {
				manufacturer, _ := vehicleInfo["manufacturer"].(string)
				model, _ := vehicleInfo["model"].(string)
				carID, _ := vehicleInfo["carID"].(uint32)

				w.log.Debug().
					Str("manufacturer", manufacturer).
					Str("model", model).
					Uint32("carID", carID).
					Int("clients", len(w.vehicleClients)).
					Msg("broadcast vehicle info")
			}

		case gameState := <-w.gameStateFeed:
			// Store current state with mutex protection
			w.gameStateMutex.Lock()
			w.currentGameState = gameState
			w.gameStateMutex.Unlock()

			// Create a message with just the game state
			gameStateInfo := map[string]any{
				"gamestate": gameState,
			}

			// Broadcast to all connected vehicle clients (game state goes through vehicle websocket)
			encodedData, err := json.Marshal(gameStateInfo)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode game state JSON")

				continue
			}

			// Send to all vehicle clients, remove failed ones
			activeClients := make([]*websocket.Conn, 0, len(w.vehicleClients))
			for _, client := range w.vehicleClients {
				// Set reasonable write deadline for high-latency connections (3 seconds)
				_ = client.SetWriteDeadline(time.Now().Add(3 * time.Second))

				err := client.WriteMessage(websocket.TextMessage, encodedData)
				if err != nil {
					w.log.Debug().Err(err).Msg("failed to send game state to client, removing")

					_ = client.Close()
				} else {
					activeClients = append(activeClients, client)
				}
			}

			w.vehicleClients = activeClients

			w.log.Debug().
				Str("gameState", gameState).
				Int("clients", len(w.vehicleClients)).
				Msg("broadcast game state")
		}
	}
}

// handleCircuitWebSocketConnection upgrades the HTTP connection to a WebSocket and streams circuit info updates.
func (w *WebUI) handleCircuitWebSocketConnection(response http.ResponseWriter, request *http.Request) {
	webSocket, err := w.upgrader.Upgrade(response, request, nil)
	if err != nil {
		w.log.Error().Err(err).Msg("error upgrading circuit websocket connection")

		return
	}

	w.log.Debug().Msg("circuit websocket connection established")

	// Subscribe this client
	w.circuitClientsChan <- webSocket

	defer func() {
		// Unsubscribe on disconnect
		w.circuitUnsubChan <- webSocket

		err := webSocket.Close()
		if err != nil {
			w.log.Error().Err(err).Msg("closing circuit websocket connection")
		}
	}()

	// Send current circuit info immediately (with mutex protection)
	w.circuitInfoMutex.RLock()

	if len(w.currentCircuitInfo) > 0 {
		encodedData, err := json.Marshal(w.currentCircuitInfo)
		w.circuitInfoMutex.RUnlock()

		if err == nil {
			err = webSocket.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				w.log.Debug().Err(err).Msg("failed to send initial circuit info")
			} else {
				w.log.Debug().Msg("sent initial circuit info to new client")
			}
		}
	} else {
		w.circuitInfoMutex.RUnlock()
	}

	// Keep connection alive - read messages (if any) to detect disconnects
	for {
		_, _, err := webSocket.ReadMessage()
		if err != nil {
			w.log.Debug().Err(err).Msg("circuit websocket read error, closing connection")

			break
		}
	}
}

// handleRaceWebSocketConnection handles WebSocket connections for race info updates.
func (w *WebUI) handleRaceWebSocketConnection(response http.ResponseWriter, request *http.Request) {
	webSocket, err := w.upgrader.Upgrade(response, request, nil)
	if err != nil {
		w.log.Error().Err(err).Msg("error upgrading race websocket connection")

		return
	}

	w.log.Debug().Msg("race websocket connection established")

	// Subscribe this client
	w.raceClientsChan <- webSocket

	defer func() {
		// Unsubscribe on disconnect
		w.raceUnsubChan <- webSocket

		err := webSocket.Close()
		if err != nil {
			w.log.Error().Err(err).Msg("closing race websocket connection")
		}
	}()

	// Send current race info immediately (with mutex protection)
	w.raceInfoMutex.RLock()

	if len(w.currentRaceInfo) > 0 {
		encodedData, err := json.Marshal(w.currentRaceInfo)
		w.raceInfoMutex.RUnlock()

		if err == nil {
			err = webSocket.WriteMessage(websocket.TextMessage, encodedData)
			if err != nil {
				w.log.Debug().Err(err).Msg("failed to send initial race info")
			} else {
				w.log.Debug().Msg("sent initial race info to new client")
			}
		}
	} else {
		w.raceInfoMutex.RUnlock()
	}

	// Keep connection alive - read messages (if any) to detect disconnects
	for {
		_, _, err := webSocket.ReadMessage()
		if err != nil {
			w.log.Debug().Err(err).Msg("race websocket read error, closing connection")

			break
		}
	}
}

// circuitInfoBroadcaster manages circuit info broadcasting to all connected clients.
func (w *WebUI) circuitInfoBroadcaster() {
	for {
		select {
		case client := <-w.circuitClientsChan:
			// Add new client
			w.circuitClients = append(w.circuitClients, client)
			w.log.Debug().Int("circuit_clients", len(w.circuitClients)).Msg("circuit client subscribed")

		case client := <-w.circuitUnsubChan:
			// Remove client
			for i, c := range w.circuitClients {
				if c == client {
					w.circuitClients = append(w.circuitClients[:i], w.circuitClients[i+1:]...)

					break
				}
			}

			w.log.Debug().Int("circuit_clients", len(w.circuitClients)).Msg("circuit client unsubscribed")

		case circuitInfo := <-w.circuitInfoFeed:
			w.log.Debug().
				Interface("circuitInfo", circuitInfo).
				Int("num_clients", len(w.circuitClients)).
				Msg("circuitInfoBroadcaster: received circuit info")

			// Store current state with mutex protection
			w.circuitInfoMutex.Lock()
			w.currentCircuitInfo = circuitInfo
			w.circuitInfoMutex.Unlock()

			// Broadcast to all connected clients
			encodedData, err := json.Marshal(circuitInfo)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode circuit info JSON")

				continue
			}

			w.log.Debug().
				Str("json_data", string(encodedData)).
				Msg("circuitInfoBroadcaster: marshaled JSON")

			// Send to all clients, remove failed ones
			activeClients := make([]*websocket.Conn, 0, len(w.circuitClients))
			for _, client := range w.circuitClients {
				// Set reasonable write deadline for high-latency connections (3 seconds)
				_ = client.SetWriteDeadline(time.Now().Add(3 * time.Second))

				err := client.WriteMessage(websocket.TextMessage, encodedData)
				if err != nil {
					w.log.Debug().Err(err).Msg("failed to send circuit info to client, removing")

					_ = client.Close()
				} else {
					activeClients = append(activeClients, client)
				}
			}

			w.circuitClients = activeClients

			if len(circuitInfo) > 0 {
				w.log.Debug().
					Str("circuit", circuitInfo["name"]).
					Str("variation", circuitInfo["variation"]).
					Int("clients", len(w.circuitClients)).
					Msg("broadcast circuit info")
			}
		}
	}
}

// raceInfoBroadcaster listens for race info updates and broadcasts them to all connected clients.
func (w *WebUI) raceInfoBroadcaster() {
	for {
		select {
		case client := <-w.raceClientsChan:
			// Add new client
			w.raceClients = append(w.raceClients, client)
			w.log.Debug().Int("race_clients", len(w.raceClients)).Msg("race client subscribed")

		case client := <-w.raceUnsubChan:
			// Remove client
			for i, c := range w.raceClients {
				if c == client {
					w.raceClients = append(w.raceClients[:i], w.raceClients[i+1:]...)

					break
				}
			}

			w.log.Debug().Int("race_clients", len(w.raceClients)).Msg("race client unsubscribed")

		case raceInfo := <-w.raceInfoFeed:
			w.log.Debug().
				Interface("raceInfo", raceInfo).
				Int("num_clients", len(w.raceClients)).
				Msg("raceInfoBroadcaster: received race info")

			// Store current state with mutex protection
			w.raceInfoMutex.Lock()
			w.currentRaceInfo = raceInfo
			w.raceInfoMutex.Unlock()

			// Broadcast to all connected clients
			encodedData, err := json.Marshal(raceInfo)
			if err != nil {
				w.log.Error().Err(err).Msg("failed to encode race info JSON")

				continue
			}

			w.log.Debug().
				Str("json_data", string(encodedData)).
				Msg("raceInfoBroadcaster: marshaled JSON")

			// Send to all clients, remove failed ones
			activeClients := make([]*websocket.Conn, 0, len(w.raceClients))
			for _, client := range w.raceClients {
				// Set reasonable write deadline for high-latency connections (3 seconds)
				_ = client.SetWriteDeadline(time.Now().Add(3 * time.Second))

				err := client.WriteMessage(websocket.TextMessage, encodedData)
				if err != nil {
					w.log.Debug().Err(err).Msg("failed to send race info to client, removing")

					_ = client.Close()
				} else {
					activeClients = append(activeClients, client)
				}
			}

			w.raceClients = activeClients

			if len(raceInfo) > 0 {
				currentLap := ""
				position := ""

				if val, ok := raceInfo["currentlap"].(string); ok {
					currentLap = val
				}

				if val, ok := raceInfo["position"].(string); ok {
					position = val
				}

				w.log.Debug().
					Str("currentlap", currentLap).
					Str("position", position).
					Int("clients", len(w.raceClients)).
					Msg("broadcast race info")
			}
		}
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
			"discord": map[string]any{
				"token":          w.config.GetDiscordToken(),
				"guildID":        w.config.GetDiscordGuildID(),
				"channelID":      w.config.GetDiscordChannelID(),
				"voiceChannelID": w.config.GetDiscordVoiceChannelID(),
			},
		},
		"synthesizer": map[string]any{
			"internalSampleRateHz":      w.config.GetInternalSampleRateHz(),
			"outputSampleRateHz":        w.config.GetOutputSampleRateHz(),
			"outputFile":                w.config.GetOutputFile(),
			"masterGain":                w.config.GetMasterGain(),
			"masterGainMute":            w.config.GetMasterGainMute(),
			"chassisGain":               w.config.GetChassisGain(),
			"chassisGainMute":           w.config.GetChassisGainMute(),
			"transmissionGain":          w.config.GetTransmissionGain(),
			"transmissionGainMute":      w.config.GetTransmissionGainMute(),
			"transmissionGainMinRace":   w.config.GetTransmissionGainMinRace(),
			"transmissionGainMinStreet": w.config.GetTransmissionGainMinStreet(),
			"engineGain":                w.config.GetEngineGain(),
			"engineGainMute":            w.config.GetEngineGainMute(),
			"gainIncrement":             w.config.GetGainIncrement(),
			"engineProfiles":            w.config.GetEngineProfiles(),
			"eqEnabled":                 w.config.GetEqEnabled(),
			"eq":                        w.config.GetEq(),
		},
		"eqCurve": func() map[string]any {
			curve, minFreq, resolution := w.config.GetEqCurve()

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
				curve, minFreq, resolution := w.config.GetEqCurve()

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

	if replayMode, ok := config["replayMode"]; ok {
		if replayBool, ok := replayMode.(bool); ok {
			w.config.SetAppReplayMode(replayBool)
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
			w.config.SetInternalSampleRateHz(int(rateFloat))
		} else {
			errors = append(errors, "invalid internal sample rate value")
		}
	}

	if outputSampleRate, ok := config["outputSampleRateHz"]; ok {
		if rateFloat, ok := outputSampleRate.(float64); ok {
			w.config.SetOutputSampleRateHz(int(rateFloat))
		} else {
			errors = append(errors, "invalid output sample rate value")
		}
	}

	if masterGain, ok := config["masterGain"]; ok {
		if gainFloat, ok := masterGain.(float64); ok {
			w.config.SetMasterGain(gainFloat)
		} else {
			errors = append(errors, "invalid master gain value")
		}
	}

	if masterGainMute, ok := config["masterGainMute"]; ok {
		if mute, ok := masterGainMute.(bool); ok {
			w.config.SetMasterGainMute(mute)
		} else {
			errors = append(errors, "invalid master gain mute value")
		}
	}

	if chassisGain, ok := config["chassisGain"]; ok {
		if gainFloat, ok := chassisGain.(float64); ok {
			w.config.SetChassisGain(gainFloat)
		} else {
			errors = append(errors, "invalid chassis gain value")
		}
	}

	if chassisGainMute, ok := config["chassisGainMute"]; ok {
		if mute, ok := chassisGainMute.(bool); ok {
			w.config.SetChassisGainMute(mute)
		} else {
			errors = append(errors, "invalid chassis gain mute value")
		}
	}

	if transmissionGain, ok := config["transmissionGain"]; ok {
		if gainFloat, ok := transmissionGain.(float64); ok {
			w.config.SetTransmissionGain(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain value")
		}
	}

	if transmissionGainMute, ok := config["transmissionGainMute"]; ok {
		if mute, ok := transmissionGainMute.(bool); ok {
			w.config.SetTransmissionGainMute(mute)
		} else {
			errors = append(errors, "invalid transmission gain mute value")
		}
	}

	if transmissionGainMinRace, ok := config["transmissionGainMinRace"]; ok {
		if gainFloat, ok := transmissionGainMinRace.(float64); ok {
			w.config.SetTransmissionGainMinRace(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain min race value")
		}
	}

	if transmissionGainMinStreet, ok := config["transmissionGainMinStreet"]; ok {
		if gainFloat, ok := transmissionGainMinStreet.(float64); ok {
			w.config.SetTransmissionGainMinStreet(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain min street value")
		}
	}

	if engineGain, ok := config["engineGain"]; ok {
		if gainFloat, ok := engineGain.(float64); ok {
			w.config.SetEngineGain(gainFloat)
		} else {
			errors = append(errors, "invalid engine gain value")
		}
	}

	if engineGainMute, ok := config["engineGainMute"]; ok {
		if mute, ok := engineGainMute.(bool); ok {
			w.config.SetEngineGainMute(mute)
		} else {
			errors = append(errors, "invalid engine gain mute value")
		}
	}

	if gainIncrement, ok := config["gainIncrement"]; ok {
		if incrementFloat, ok := gainIncrement.(float64); ok {
			w.config.SetGainIncrement(incrementFloat)
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

					w.config.SetEngineProfile(name, profile)
				}
			}
		} else {
			errors = append(errors, "invalid engine profiles format")
		}
	}

	if eqEnabled, ok := config["eqEnabled"]; ok {
		if enabled, ok := eqEnabled.(bool); ok {
			w.config.SetEqEnabled(enabled)
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
				w.config.SetEq(eqBands)
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
			w.config.SetDynamicTransmissionFeedbackEnabled(dynamicBool)
		}
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

	if pulseMaxAmplitude, ok := config["pulseMaxAmplitude"]; ok {
		if amplitudeFloat, ok := parseFloat(pulseMaxAmplitude, "pulse max amplitude"); ok {
			w.config.SetPulseMaxAmplitude(amplitudeFloat)
		}
	}

	if pulseMaxFreq, ok := config["pulseMaxFrequencyHz"]; ok {
		if freqFloat, ok := parseFloat(pulseMaxFreq, "pulse max frequency"); ok {
			w.config.SetPulseMaxFrequencyHz(freqFloat)
		}
	}

	if pulseMinFreq, ok := config["pulseMinFrequencyHz"]; ok {
		if freqFloat, ok := parseFloat(pulseMinFreq, "pulse min frequency"); ok {
			w.config.SetPulseMinFrequencyHz(freqFloat)
		}
	}

	return errors
}

// checkRestartRequired checks if any configuration changes require a restart.
func (w *WebUI) checkRestartRequired(configData map[string]any) bool {
	// Check if vehicleDBFile changed
	if appConfig, ok := configData["app"].(map[string]any); ok {
		if vehicleDBFile, ok := appConfig["vehicleDBFile"]; ok {
			if vehicleDBFileStr, ok := vehicleDBFile.(string); ok {
				if vehicleDBFileStr != w.config.GetAppVehicleDBFile() {
					return true
				}
			}
		}
	}

	// Check if telemetry source changed
	if telemetryConfig, ok := configData["telemetry"].(map[string]any); ok {
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
			w.config.SetFuelMonitoringEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid fuel monitoring enabled value")
		}
	}

	if preWarnLaps, ok := config["preWarnNotifyLaps"]; ok {
		if lapsFloat, ok := preWarnLaps.(float64); ok {
			w.config.SetFuelPreWarnNotifyLaps(lapsFloat)
		} else {
			errors = append(errors, "invalid pre-warn notify laps value")
		}
	}

	if strategyLaps, ok := config["strategyNotifyLaps"]; ok {
		if lapsFloat, ok := strategyLaps.(float64); ok {
			w.config.SetFuelStrategyNotifyLaps(lapsFloat)
		} else {
			errors = append(errors, "invalid strategy notify laps value")
		}
	}

	if safetyMarginLaps, ok := config["rangeSafetyMarginLaps"]; ok {
		if marginFloat, ok := safetyMarginLaps.(float64); ok {
			w.config.SetFuelRangeSafetyMarginLaps(marginFloat)
		} else {
			errors = append(errors, "invalid range safety margin laps value")
		}
	}

	if safetyMarginMeters, ok := config["rangeSafetyMarginMeters"]; ok {
		if marginFloat, ok := safetyMarginMeters.(float64); ok {
			w.config.SetFuelRangeSafetyMarginMeters(marginFloat)
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

	if source, ok := config["source"]; ok {
		if sourceStr, ok := source.(string); ok {
			// If source is a file:// path, validate that the file exists
			if strings.HasPrefix(sourceStr, "file://") {
				filePath := strings.TrimPrefix(sourceStr, "file://")

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
		} else {
			errors = append(errors, "invalid telemetry source value")
		}
	}

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
			w.config.SetMessageSendIntervalMs(int(intervalFloat))
		} else {
			errors = append(errors, "invalid message send interval value")
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
			w.config.SetTyreMonitoringEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid tyre monitoring enabled value")
		}
	}

	if tempOptimal, ok := config["temperatureOptimalCelsius"]; ok {
		if tempFloat, ok := tempOptimal.(float64); ok {
			w.config.SetTyreTemperatureOptimalCelsius(float32(tempFloat))
		} else {
			errors = append(errors, "invalid temperature optimal value")
		}
	}

	if tempWindow, ok := config["temperatureOperatingWindow"]; ok {
		if windowFloat, ok := tempWindow.(float64); ok {
			w.config.SetTyreTemperatureOperatingWindow(float32(windowFloat))
		} else {
			errors = append(errors, "invalid temperature operating window value")
		}
	}

	if tempMargin, ok := config["temperatureMarginCelsius"]; ok {
		if marginFloat, ok := tempMargin.(float64); ok {
			w.config.SetTyreTemperatureMarginCelsius(float32(marginFloat))
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
				Available bool `json:"available"`
			} `json:"status"`
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
