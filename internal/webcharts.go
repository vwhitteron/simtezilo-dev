package internal

import (
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func StartWebChartServer(core *Core) {
	if !core.webEnabled {
		core.log.Info().Msg("web server disabled")

		return
	}

	http.HandleFunc("/", createHandlerFunc(core.assetDir))
	http.HandleFunc("/js/scichart.js", createSciChartJSFunc(core.assetDir))
	http.HandleFunc("/ws", core.handleWebSocketConnection)
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		core.log.Error().Err(err).Msg("error starting web server")
		core.webEnabled = false
	}

	core.log.Info().Msg("web server started on port 8080")

	return
}

func createHandlerFunc(assetDir string) func(response http.ResponseWriter, request *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		htmlFile, err := os.Open(assetDir + "/html/index.html")
		if err != nil {
			log.Printf("error opening html file: %v", err)
			return
		}

		htmlData, err := io.ReadAll(htmlFile)
		if err != nil {
			log.Printf("error reading html file: %v", err)
			return
		}

		response.Write(htmlData)
	}
}
func createSciChartJSFunc(assetDir string) func(response http.ResponseWriter, request *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		jsFile, err := os.Open(assetDir + "/html/scichart.js")
		if err != nil {
			log.Printf("error opening js file: %v", err)
			return
		}

		jsData, err := io.ReadAll(jsFile)
		if err != nil {
			log.Printf("error reading js file: %v", err)
			return
		}

		response.Write(jsData)

	}
}
