package webui

import (
	_ "embed"
	"encoding/json"
	"net/http"

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
	http.HandleFunc("/telemetry", w.telemetryHandlerFunc())
	http.HandleFunc("/js/scichart.js", w.sciChartJSHandlerFunc())
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

//go:embed js/scichart.js
var scichartJS []byte

func (w *WebUI) sciChartJSHandlerFunc() func(w http.ResponseWriter, r *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Write(scichartJS)
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
	for data := range w.telemetryChartFeed {
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
			w.log.Error().Err(err).Msg("failed to send data")
			continue
		}
	}
}
