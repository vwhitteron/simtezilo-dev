// Package webui implements a simple web server to serve a web-based user interface
package webui

import (
	"embed"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// WebUI defines the web user interface.
type WebUI struct {
	log                zerolog.Logger
	port               int
	webSocketClients   int
	telemetryChartFeed chan map[string]float32
	upgrader           websocket.Upgrader
}

// New creates a new instance of the WebUI.
func New(log zerolog.Logger, port int, telemetryChartFeed chan map[string]float32) *WebUI {
	return &WebUI{
		log:                log.With().Str("component", "web ui").Logger(),
		port:               port,
		webSocketClients:   0,
		telemetryChartFeed: telemetryChartFeed,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
	}
}

// Start sets up handlers and starts the web server.
func (w *WebUI) Start() {
	http.HandleFunc("/", w.htmlRouterHandlerFunc())
	http.HandleFunc("/css/", w.cssHandlerFunc())
	http.HandleFunc("/images/", w.imagesHandlerFunc())
	http.HandleFunc("/js/", w.sciChartJSHandlerFunc())
	http.HandleFunc("/ws", w.handleWebSocketConnection)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", w.port),
		ReadHeaderTimeout: 3 * time.Second,
	}

	err := server.ListenAndServe()
	if err != nil {
		w.log.Error().Err(err).Msg("error starting web server")

		return
	}

	w.log.Info().Msg("Web UI started on port 8080\r\n")
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
