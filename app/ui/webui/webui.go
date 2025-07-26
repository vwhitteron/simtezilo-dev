package webui

import (
	"embed"
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type WebUI struct {
	log                zerolog.Logger
	webSocketClients   int
	telemetryChartFeed chan map[string]float32
}

func NewWebUI(log zerolog.Logger, telemetryChartFeed chan map[string]float32) *WebUI {
	return &WebUI{
		log:                log.With().Str("component", "web ui").Logger(),
		webSocketClients:   0,
		telemetryChartFeed: telemetryChartFeed,
	}
}

func (w *WebUI) Start() {
	w.log.Info().Msg("Web UI started on port 8080\r\n")

	http.HandleFunc("/", w.rootHandlerFunc())
	http.HandleFunc("/images/", w.imagesHandlerFunc())
	http.HandleFunc("/js/", w.sciChartJSHandlerFunc())
	http.HandleFunc("/telemetry", w.telemetryHandlerFunc())
	http.HandleFunc("/ws", w.handleWebSocketConnection)

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		w.log.Error().Err(err).Msg("error starting web server")
	}
}

//go:embed html/index.html
var indexHTML []byte

func (w *WebUI) rootHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		response.Write(indexHTML)
	}
}

//go:embed html/telemetry.html
var telemetryHTML []byte

func (w *WebUI) telemetryHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		response.Write(telemetryHTML)
	}
}

//go:embed static/*
var staticFiles embed.FS

func (w *WebUI) imagesHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		filename := "static" + request.URL.Path

		content, err := staticFiles.ReadFile(filename)
		if err != nil {
			response.WriteHeader(http.StatusNotFound)
			w.log.Error().Err(err).Msg("Invalid image file")
		}

		var contentType string
		suffix := filepath.Ext(filename)
		switch suffix {
		case ".png":
			contentType = "image/png"
		case ".svg":
			contentType = "image/svg+xml"
		default:
			contentType = "image/jpeg"
		}

		response.Header().Add("Content-Type", contentType)
		response.Write(content)

		w.log.Debug().Str("file", filename).Str("mime-type", contentType).Str("suffix", suffix).Msg("returned file")
	}
}

func (w *WebUI) sciChartJSHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		filename := "static" + request.URL.Path

		content, err := staticFiles.ReadFile(filename)
		if err != nil {
			response.WriteHeader(http.StatusNotFound)
			w.log.Error().Err(err).Msg("Invalid javascript file")
		}

		contentType := "application/javascript"

		response.Header().Add("Content-Type", contentType)
		response.Write(content)

		w.log.Debug().Str("file", filename).Str("mime-type", contentType).Msg("returned file")
	}
}

func (w *WebUI) HasActiveClients() bool {
	return w.webSocketClients > 0
}

func (w *WebUI) handleWebSocketConnection(response http.ResponseWriter, request *http.Request) {
	ws, err := Upgrader.Upgrade(response, request, nil)
	if err != nil {
		w.log.Error().Err(err).Msg("error upgrading connection")
		return
	}
	defer ws.Close()

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
		err = ws.WriteMessage(websocket.TextMessage, encodedData)
		if err != nil {
			failCount++
			w.log.Debug().Err(err).Msg("failed to send data to websocket")
			continue
		}
	}
}
