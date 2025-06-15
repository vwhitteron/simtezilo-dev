package internal

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func StartWebChartServer(app *App) {
	if !app.webEnabled {
		app.log.Debug().Str("component", "webchart").Msg("server disabled")

		return
	}

	http.HandleFunc("/", rootHandlerFunc(app.assetDir))
	http.HandleFunc("/chart", chartHandlerFunc(app.assetDir))
	http.HandleFunc("/js/scichart.js", sciChartJSFunc(app.assetDir))
	http.HandleFunc("/ws", app.handleWebSocketConnection)

	app.log.Debug().Str("component", "webchart").Msg("starting server on port 8080")
	fmt.Printf("WebChart server started on port 8080\r\n")

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		app.log.Error().Err(err).Str("component", "webchart").Msg("error starting web server")
		app.webEnabled = false
	}
}

func rootHandlerFunc(assetDir string) func(response http.ResponseWriter, request *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		htmlFile, err := os.Open(assetDir + "/html/index.html") // TODO: use go:embed
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

func chartHandlerFunc(assetDir string) func(response http.ResponseWriter, request *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		htmlFile, err := os.Open(assetDir + "/html/chart.html") // TODO: use go:embed
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

func sciChartJSFunc(assetDir string) func(response http.ResponseWriter, request *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		jsFile, err := os.Open(assetDir + "/html/scichart.js") // TODO: use go:embed
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
