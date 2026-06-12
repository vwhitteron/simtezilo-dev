package webui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// WSMessage is the typed envelope for all WebSocket messages.
type WSMessage struct {
	Type      string `json:"type"`      // Message type
	Timestamp int64  `json:"timestamp"` // Unix timestamp in milliseconds
	Data      any    `json:"data"`      // Message data
}

// wsMessage is an internal message sent to a client's write goroutine.
type wsMessage struct {
	msgType     string // Message type for subscription filtering
	data        []byte // Pre-encoded message data
	isPing      bool   // True if this is a ping control message
	isInitState bool   // True if this is initial state (bypasses subscription check)
}

// wsClient represents a connected websocket client with its own write channel.
// Each client has a dedicated goroutine that handles all writes, eliminating
// the need for mutexes and following Go's "share memory by communicating" idiom.
type wsClient struct {
	conn          *websocket.Conn
	send          chan wsMessage  // Channel for outgoing messages
	subscriptions map[string]bool // What data types this client wants
	subMu         sync.RWMutex    // Protects subscriptions map
	done          chan struct{}   // Signals client shutdown
	closeOnce     sync.Once       // Ensures cleanup happens once
	log           zerolog.Logger  // Logger for this client
}

// newWSClient creates a new websocket client with default subscriptions.
func newWSClient(conn *websocket.Conn, log zerolog.Logger) *wsClient {
	return &wsClient{
		conn: conn,
		send: make(chan wsMessage, 64), // Buffered to handle bursts
		subscriptions: map[string]bool{
			"vehicle":     true,
			"gameState":   true,
			"circuit":     true,
			"race":        true,
			"logStats":    true,
			"calibration": true,
			"telemetry":   false, // Telemetry off by default
			"screen":      false, // Hardware screen mirror off by default
		},
		done: make(chan struct{}),
		log:  log,
	}
}

// Send queues a message to be sent to the client.
// Returns false if the client is closed or the send buffer is full.
func (c *wsClient) Send(msgType string, data []byte, isInitState bool) bool {
	select {
	case <-c.done:
		return false
	case c.send <- wsMessage{msgType: msgType, data: data, isInitState: isInitState}:
		return true
	default:
		c.log.Debug().Str("msgType", msgType).Msg("dropping message, client buffer full")

		return false
	}
}

// UpdateSubscriptions updates what message types this client receives.
func (c *wsClient) UpdateSubscriptions(subs map[string]bool) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	maps.Copy(c.subscriptions, subs)
}

// Close gracefully shuts down the client.
func (c *wsClient) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

// IsClosed returns true if the client has been closed.
func (c *wsClient) IsClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// writePump handles all writes to the websocket connection.
// It runs in its own goroutine and is the only code that writes to the connection.
func (c *wsClient) writePump() {
	pingTicker := time.NewTicker(5 * time.Second)

	defer func() {
		pingTicker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			return

		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})

				return
			}

			if msg.isPing {
				_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))

				err := c.conn.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					c.log.Debug().Err(err).Msg("failed to send ping")

					return
				}

				continue
			}

			// Check subscription (unless it's initial state)
			if !msg.isInitState {
				c.subMu.RLock()
				subscribed := c.subscriptions[msg.msgType]
				c.subMu.RUnlock()

				if !subscribed {
					continue
				}
			}

			_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))

			err := c.conn.WriteMessage(websocket.TextMessage, msg.data)
			if err != nil {
				c.log.Debug().Err(err).Msg("failed to send message")

				return
			}

		case <-pingTicker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))

			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			if err != nil {
				c.log.Debug().Err(err).Msg("failed to send ping")

				return
			}
		}
	}
}

// broadcasterOptions are the constructor parameters for Broadcaster.
type broadcasterOptions struct {
	log             zerolog.Logger
	telemetryFeed   chan map[string]float32
	vehicleInfoFeed chan map[string]any
	circuitInfoFeed chan map[string]string
	raceInfoFeed    chan map[string]any
	gameStateFeed   chan string
	logStatsFeed    chan map[string]any
	screenFrameFeed chan *image.RGBA
}

// Broadcaster owns all WebSocket client state and drives the message fan-out loop.
// It is the sole owner of the current-state caches used to replay state to new clients.
type Broadcaster struct {
	log zerolog.Logger

	// Feed channels from the app layer
	telemetryChartFeed chan map[string]float32
	vehicleInfoFeed    chan map[string]any
	circuitInfoFeed    chan map[string]string
	raceInfoFeed       chan map[string]any
	gameStateFeed      chan string
	logStatsFeed       chan map[string]any
	screenFrameFeed    chan *image.RGBA

	// Cached current state — replayed to new connections
	currentVehicleInfo map[string]any
	vehicleInfoMutex   sync.RWMutex
	currentGameState   string
	gameStateMutex     sync.RWMutex
	currentScreenFrame []byte // most recent broadcast screen frame
	screenFrameMutex   sync.RWMutex
	currentCircuitInfo map[string]string
	circuitInfoMutex   sync.RWMutex
	currentRaceInfo    map[string]any
	raceInfoMutex      sync.RWMutex
	currentLogStats    map[string]any
	logStatsMutex      sync.RWMutex

	// WebSocket client registry
	upgrader           websocket.Upgrader
	webSocketClients   int // legacy counter, always 0
	unifiedClients     []*wsClient
	unifiedClientsMux  sync.RWMutex
	unifiedClientsChan chan *wsClient
	unifiedUnsubChan   chan *wsClient
	unifiedSessions    map[string]*wsClient // session ID → client
	unifiedSessionsMux sync.Mutex

	// Lifecycle
	done      chan struct{}
	closeOnce sync.Once
}

func newBroadcaster(opts broadcasterOptions) *Broadcaster {
	return &Broadcaster{
		log:                opts.log,
		telemetryChartFeed: opts.telemetryFeed,
		vehicleInfoFeed:    opts.vehicleInfoFeed,
		circuitInfoFeed:    opts.circuitInfoFeed,
		raceInfoFeed:       opts.raceInfoFeed,
		gameStateFeed:      opts.gameStateFeed,
		logStatsFeed:       opts.logStatsFeed,
		screenFrameFeed:    opts.screenFrameFeed,
		currentVehicleInfo: make(map[string]any),
		currentGameState:   "unknown",
		currentCircuitInfo: make(map[string]string),
		currentRaceInfo:    make(map[string]any),
		currentLogStats:    make(map[string]any),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		unifiedClients:     make([]*wsClient, 0),
		unifiedClientsChan: make(chan *wsClient, 10),
		unifiedUnsubChan:   make(chan *wsClient, 10),
		unifiedSessions:    make(map[string]*wsClient),
		done:               make(chan struct{}),
	}
}

// HasActiveClients returns true if there are any live WebSocket clients.
func (b *Broadcaster) HasActiveClients() bool {
	b.unifiedClientsMux.RLock()
	hasUnified := len(b.unifiedClients) > 0
	b.unifiedClientsMux.RUnlock()

	return hasUnified || b.webSocketClients > 0
}

// Close signals the broadcaster to shut down and closes all clients.
func (b *Broadcaster) Close() {
	b.closeOnce.Do(func() {
		b.log.Info().Msg("Closing WebUI")
		close(b.done)

		b.unifiedClientsMux.Lock()

		for _, client := range b.unifiedClients {
			client.Close()
		}

		b.unifiedClients = nil
		b.unifiedClientsMux.Unlock()

		b.log.Debug().Msg("WebUI closed")
	})
}

// broadcast sends a message to all connected clients, pruning closed ones.
func (b *Broadcaster) broadcast(data []byte, msgType string) {
	b.unifiedClientsMux.Lock()
	defer b.unifiedClientsMux.Unlock()

	active := make([]*wsClient, 0, len(b.unifiedClients))

	for _, client := range b.unifiedClients {
		if client.IsClosed() {
			continue
		}

		if client.Send(msgType, data, false) {
			active = append(active, client)
		} else {
			b.log.Debug().Str("msgType", msgType).Msg("client unavailable, removing")
		}
	}

	b.unifiedClients = active
}

// sendCurrentScreenFrame replays the latest hardware screen frame to a client that
// subscribes after the screen has gone static (the render loop only writes on change).
func (b *Broadcaster) sendCurrentScreenFrame(client *wsClient) {
	b.screenFrameMutex.RLock()
	data := b.currentScreenFrame
	b.screenFrameMutex.RUnlock()

	if data != nil {
		client.Send("screen", data, true)
	}
}

// sendInitialState sends all cached state to a newly connected client.
func (b *Broadcaster) sendInitialState(client *wsClient) {
	b.vehicleInfoMutex.RLock()

	if len(b.currentVehicleInfo) > 0 {
		data, err := json.Marshal(WSMessage{
			Type:      "vehicle",
			Timestamp: time.Now().UnixMilli(),
			Data:      b.currentVehicleInfo,
		})
		if err == nil {
			client.Send("vehicle", data, true)
		}
	}

	b.vehicleInfoMutex.RUnlock()

	b.gameStateMutex.RLock()
	gameState := b.currentGameState
	b.gameStateMutex.RUnlock()

	if gameState != "" {
		data, err := json.Marshal(WSMessage{
			Type:      "gameState",
			Timestamp: time.Now().UnixMilli(),
			Data:      map[string]any{"gamestate": gameState},
		})
		if err == nil {
			client.Send("gameState", data, true)
		}
	}

	b.circuitInfoMutex.RLock()

	if len(b.currentCircuitInfo) > 0 {
		data, err := json.Marshal(WSMessage{
			Type:      "circuit",
			Timestamp: time.Now().UnixMilli(),
			Data:      b.currentCircuitInfo,
		})
		if err == nil {
			client.Send("circuit", data, true)
		}
	}

	b.circuitInfoMutex.RUnlock()

	b.raceInfoMutex.RLock()

	if len(b.currentRaceInfo) > 0 {
		data, err := json.Marshal(WSMessage{
			Type:      "race",
			Timestamp: time.Now().UnixMilli(),
			Data:      b.currentRaceInfo,
		})
		if err == nil {
			client.Send("race", data, true)
		}
	}

	b.raceInfoMutex.RUnlock()

	b.logStatsMutex.RLock()

	if len(b.currentLogStats) > 0 {
		data, err := json.Marshal(WSMessage{
			Type:      "logStats",
			Timestamp: time.Now().UnixMilli(),
			Data:      b.currentLogStats,
		})
		if err == nil {
			client.Send("logStats", data, true)
		}
	}

	b.logStatsMutex.RUnlock()
}

// handleWebSocketConnection is the HTTP handler that upgrades connections and runs the read loop.
func (b *Broadcaster) handleWebSocketConnection(response http.ResponseWriter, request *http.Request) {
	sessionID := request.URL.Query().Get("session")

	if sessionID != "" {
		b.unifiedSessionsMux.Lock()

		if oldClient, exists := b.unifiedSessions[sessionID]; exists {
			b.log.Debug().Str("session", sessionID).Msg("closing old unified connection for session")

			oldClient.Close()
			b.unifiedUnsubChan <- oldClient
		}

		b.unifiedSessionsMux.Unlock()
	}

	conn, err := b.upgrader.Upgrade(response, request, nil)
	if err != nil {
		b.log.Error().Err(err).Msg("error upgrading unified websocket connection")

		return
	}

	client := newWSClient(conn, b.log)

	b.log.Debug().Str("session", sessionID).Msg("unified websocket connection established")

	if sessionID != "" {
		b.unifiedSessionsMux.Lock()
		b.unifiedSessions[sessionID] = client
		b.unifiedSessionsMux.Unlock()
	}

	go client.writePump()

	b.unifiedClientsChan <- client
	b.sendInitialState(client)

	defer func() {
		if sessionID != "" {
			b.unifiedSessionsMux.Lock()
			delete(b.unifiedSessions, sessionID)
			b.unifiedSessionsMux.Unlock()
		}

		b.unifiedUnsubChan <- client
		client.Close()
	}()

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		return nil
	})

	conn.SetCloseHandler(func(code int, text string) error {
		b.log.Debug().Int("code", code).Str("text", text).Msg("unified websocket close message received")

		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			b.log.Debug().Err(err).Msg("unified websocket read error, closing connection")

			break
		}

		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		var subMsg struct {
			Type          string          `json:"type"`
			Subscriptions map[string]bool `json:"subscriptions"`
		}

		if err := json.Unmarshal(message, &subMsg); err == nil && subMsg.Type == "subscribe" {
			client.UpdateSubscriptions(subMsg.Subscriptions)

			b.log.Debug().
				Interface("subscriptions", subMsg.Subscriptions).
				Msg("client updated subscriptions")

			if subMsg.Subscriptions["screen"] {
				b.sendCurrentScreenFrame(client)
			}
		}
	}
}

// run is the main broadcaster goroutine — drives all message fan-out.
func (b *Broadcaster) run() {
	batchFrameRate := 30
	bufferSize := batchFrameRate / 60
	batchInterval := time.Duration(1000/batchFrameRate) * time.Millisecond
	batchBuffer := make([]map[string]float32, 0, bufferSize)

	sid := 0

	var lastScreenHash uint32

	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.done:
			b.log.Debug().Msg("Broadcaster received shutdown signal")

			return

		case client := <-b.unifiedClientsChan:
			b.unifiedClientsMux.Lock()
			b.unifiedClients = append(b.unifiedClients, client)
			b.unifiedClientsMux.Unlock()

			b.log.Debug().Int("unified_clients", len(b.unifiedClients)).Msg("unified client subscribed")

		case client := <-b.unifiedUnsubChan:
			b.unifiedClientsMux.Lock()

			for i, c := range b.unifiedClients {
				if c == client {
					b.unifiedClients = append(b.unifiedClients[:i], b.unifiedClients[i+1:]...)

					break
				}
			}

			b.unifiedClientsMux.Unlock()

			b.log.Debug().Int("unified_clients", len(b.unifiedClients)).Msg("unified client unsubscribed")

		case data := <-b.telemetryChartFeed:
			if sid != 0 {
				diff := int(data["seq"]) - sid
				if diff == 0 {
					continue
				}
			}

			sid = int(data["seq"])
			batchBuffer = append(batchBuffer, data)

		case <-ticker.C:
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
				b.log.Error().Err(err).Msg("failed to encode batched telemetry JSON")

				batchBuffer = batchBuffer[:0]

				continue
			}

			b.broadcast(encodedData, "telemetry")

			batchBuffer = batchBuffer[:0]

		case canvas := <-b.screenFrameFeed:
			sum := crc32.ChecksumIEEE(canvas.Pix)
			if sum == lastScreenHash {
				continue
			}

			lastScreenHash = sum

			var buf bytes.Buffer

			if err := png.Encode(&buf, canvas); err != nil {
				b.log.Error().Err(err).Msg("failed to encode screen frame PNG")

				continue
			}

			bounds := canvas.Bounds()

			msg := WSMessage{
				Type:      "screen",
				Timestamp: time.Now().UnixMilli(),
				Data: map[string]any{
					"image":  "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
					"width":  bounds.Dx(),
					"height": bounds.Dy(),
				},
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				b.log.Error().Err(err).Msg("failed to encode screen frame JSON")

				continue
			}

			b.screenFrameMutex.Lock()
			b.currentScreenFrame = encodedData
			b.screenFrameMutex.Unlock()

			b.broadcast(encodedData, "screen")

		case vehicleInfo := <-b.vehicleInfoFeed:
			b.vehicleInfoMutex.Lock()
			b.currentVehicleInfo = vehicleInfo
			b.vehicleInfoMutex.Unlock()

			msg := WSMessage{
				Type:      "vehicle",
				Timestamp: time.Now().UnixMilli(),
				Data:      vehicleInfo,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				b.log.Error().Err(err).Msg("failed to encode vehicle info JSON")

				continue
			}

			b.broadcast(encodedData, "vehicle")

			if len(vehicleInfo) > 0 {
				manufacturer, _ := vehicleInfo["manufacturer"].(string)
				model, _ := vehicleInfo["model"].(string)
				carID, _ := vehicleInfo["carID"].(uint32)

				b.log.Debug().
					Str("manufacturer", manufacturer).
					Str("model", model).
					Uint32("carID", carID).
					Int("clients", len(b.unifiedClients)).
					Msg("broadcast vehicle info to unified clients")
			}

		case gameState := <-b.gameStateFeed:
			b.gameStateMutex.Lock()
			b.currentGameState = gameState
			b.gameStateMutex.Unlock()

			msg := WSMessage{
				Type:      "gameState",
				Timestamp: time.Now().UnixMilli(),
				Data:      map[string]any{"gamestate": gameState},
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				b.log.Error().Err(err).Msg("failed to encode game state JSON")

				continue
			}

			b.broadcast(encodedData, "gameState")

			b.log.Debug().
				Str("gameState", gameState).
				Int("clients", len(b.unifiedClients)).
				Msg("broadcast game state to unified clients")

		case circuitInfo := <-b.circuitInfoFeed:
			b.log.Debug().Interface("circuitInfo", circuitInfo).Msg("received circuit info from channel")

			b.circuitInfoMutex.Lock()
			b.currentCircuitInfo = circuitInfo
			b.circuitInfoMutex.Unlock()

			msg := WSMessage{
				Type:      "circuit",
				Timestamp: time.Now().UnixMilli(),
				Data:      circuitInfo,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				b.log.Error().Err(err).Msg("failed to encode circuit info JSON")

				continue
			}

			b.broadcast(encodedData, "circuit")

			if len(circuitInfo) > 0 {
				name := circuitInfo["name"]
				length := circuitInfo["length"]

				b.log.Debug().
					Str("circuit", name).
					Str("length", length).
					Int("clients", len(b.unifiedClients)).
					Msg("broadcast circuit info to unified clients")
			}

		case raceInfo := <-b.raceInfoFeed:
			b.raceInfoMutex.Lock()
			b.currentRaceInfo = raceInfo
			b.raceInfoMutex.Unlock()

			msg := WSMessage{
				Type:      "race",
				Timestamp: time.Now().UnixMilli(),
				Data:      raceInfo,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				b.log.Error().Err(err).Msg("failed to encode race info JSON")

				continue
			}

			b.broadcast(encodedData, "race")

			if len(raceInfo) > 0 {
				lap, _ := raceInfo["lap"].(int)
				totalLaps, _ := raceInfo["totalLaps"].(int)

				b.log.Debug().
					Int("lap", lap).
					Int("totalLaps", totalLaps).
					Int("clients", len(b.unifiedClients)).
					Msg("broadcast race info to unified clients")
			}

		case logStats := <-b.logStatsFeed:
			b.logStatsMutex.Lock()
			b.currentLogStats = logStats
			b.logStatsMutex.Unlock()

			msg := WSMessage{
				Type:      "logStats",
				Timestamp: time.Now().UnixMilli(),
				Data:      logStats,
			}

			encodedData, err := json.Marshal(msg)
			if err != nil {
				b.log.Error().Err(err).Msg("failed to encode log stats JSON")

				continue
			}

			b.broadcast(encodedData, "logStats")

			totalCount, _ := logStats["totalCount"].(int)
			b.log.Debug().
				Int("totalCount", totalCount).
				Int("clients", len(b.unifiedClients)).
				Msg("broadcast log stats to unified clients")
		}
	}
}
