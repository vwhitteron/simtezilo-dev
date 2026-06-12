// Package webui implements a simple web server to serve a web-based user interface
package webui

import (
	"embed"
	"image"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/logstore"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
	"github.com/vwhitteron/simtezilo-dev/app/ui/webui/webcommon"
	"github.com/vwhitteron/simtezilo-dev/app/updater"
)

// Options holds constructor parameters for WebUI.
// (Renamed from Config to avoid the self-referential Config.Config field collision.)
type Options struct {
	Log                zerolog.Logger
	Port               int
	TelemetryChartFeed chan map[string]float32
	VehicleInfoFeed    chan map[string]any
	CircuitInfoFeed    chan map[string]string
	RaceInfoFeed       chan map[string]any
	GameStateFeed      chan string
	LogStatsFeed       chan map[string]any
	ScreenFrameFeed    chan *image.RGBA
	SendHIDInput       func(key string) bool
	Config             *appconfig.Config
	Calibrator         *calibrator.ToneGenerator
	ShutdownChan       chan exitcode.Code
	SetupMode          *setupmode.SetupMode
	LogStore           *logstore.Store
	BuildVersion       string
	BuildCommitHash    string
	BuildTime          string
	BuildPlatform      string
	Updater            *updater.Updater
}

// WebUI composes a Broadcaster with focused sub-handlers for config, system, and update APIs.
// Static file serving and HTTP routing live here; all other logic is in the sub-handlers.
type WebUI struct {
	log         zerolog.Logger
	port        int
	config      *appconfig.Config // dev-tools gate for htmlRouterHandlerFunc
	broadcaster *Broadcaster
	cfgHandler  *configHandler
	sysHandler  *systemHandler
	updHandler  *updateHandler
}

// New creates a new WebUI instance and starts the WebSocket broadcaster.
func New(opts Options) *WebUI {
	log := opts.Log.With().Str("component", "web ui").Logger()

	broadcaster := newBroadcaster(broadcasterOptions{
		log:             log,
		telemetryFeed:   opts.TelemetryChartFeed,
		vehicleInfoFeed: opts.VehicleInfoFeed,
		circuitInfoFeed: opts.CircuitInfoFeed,
		raceInfoFeed:    opts.RaceInfoFeed,
		gameStateFeed:   opts.GameStateFeed,
		logStatsFeed:    opts.LogStatsFeed,
		screenFrameFeed: opts.ScreenFrameFeed,
	})

	webUI := &WebUI{
		log:         log,
		port:        opts.Port,
		config:      opts.Config,
		broadcaster: broadcaster,
		cfgHandler:  newConfigHandler(log, opts.Config, opts.Calibrator, opts.Updater, broadcaster),
		sysHandler: newSystemHandler(systemHandlerOptions{
			log:             log,
			config:          opts.Config,
			setupMode:       opts.SetupMode,
			shutdownChan:    opts.ShutdownChan,
			logStore:        opts.LogStore,
			sendHIDInput:    opts.SendHIDInput,
			buildVersion:    opts.BuildVersion,
			buildCommitHash: opts.BuildCommitHash,
			buildTime:       opts.BuildTime,
			buildPlatform:   opts.BuildPlatform,
		}),
		updHandler: newUpdateHandler(updateHandlerOptions{
			log:          log,
			config:       opts.Config,
			updater:      opts.Updater,
			buildVersion: opts.BuildVersion,
			shutdownChan: opts.ShutdownChan,
		}),
	}

	go broadcaster.run()

	return webUI
}

// GetHTTPHandler returns the HTTP handler for the web UI.
func (w *WebUI) GetHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// Static content
	mux.HandleFunc("/", w.htmlRouterHandlerFunc())
	mux.HandleFunc("/css/", w.cssHandlerFunc())
	mux.HandleFunc("/images/", w.imagesHandlerFunc())
	mux.HandleFunc("/js/", w.sciChartJSHandlerFunc())

	// WebSocket
	mux.HandleFunc("/ws", w.broadcaster.handleWebSocketConnection)

	// Config API
	mux.HandleFunc("/api/calibration/sweep", w.cfgHandler.handleCalibrationSweep)
	mux.HandleFunc("/api/config", w.cfgHandler.handleConfigAPI)
	mux.HandleFunc("/api/config/export", w.cfgHandler.handleConfigExport)
	mux.HandleFunc("/api/config/import", w.cfgHandler.handleConfigImport)
	mux.HandleFunc("/api/config/reset", w.cfgHandler.handleConfigReset)
	mux.HandleFunc("/api/config/status", w.cfgHandler.handleConfigStatus)

	// System API
	mux.HandleFunc("/api/hardware/input", w.sysHandler.handleHardwareInput)
	mux.HandleFunc("/api/i18n", w.sysHandler.handleI18nAPI)
	mux.HandleFunc("/api/languages", w.sysHandler.handleLanguagesAPI)
	mux.HandleFunc("/api/logs", w.sysHandler.handleLogsAPI)
	mux.HandleFunc("/api/system/cache-clear", w.sysHandler.handleCacheClear)
	mux.HandleFunc("/api/system/cache-size", w.sysHandler.handleCacheSize)
	mux.HandleFunc("/api/system/info", w.sysHandler.handleSystemInfo)
	mux.HandleFunc("/api/system/restart", w.sysHandler.handleRestart)

	// Updates API
	mux.HandleFunc("/api/updates/status", w.updHandler.handleUpdatesStatus)
	mux.HandleFunc("/api/updates/check", w.updHandler.handleUpdatesCheck)
	mux.HandleFunc("/api/updates/download", w.updHandler.handleUpdatesDownload)
	mux.HandleFunc("/api/updates/upload", w.updHandler.handleUpdatesUpload)
	mux.HandleFunc("/api/updates/install", w.updHandler.handleUpdatesInstall)
	mux.HandleFunc("/api/updates/rollback", w.updHandler.handleUpdatesRollback)

	if w.sysHandler.setupMode != nil && w.sysHandler.setupMode.IsAvailable() {
		mux.HandleFunc("/api/system/factory-reset", w.sysHandler.handleFactoryReset)
		mux.HandleFunc("/api/system/ssh/enable", w.sysHandler.handleSSHEnable)
		mux.HandleFunc("/api/system/ssh/disable", w.sysHandler.handleSSHDisable)
		mux.HandleFunc("/api/system/ssh/provision", w.sysHandler.handleSSHProvision)
		mux.HandleFunc("/api/mode/setup", w.sysHandler.handleSetupMode)
	}

	w.log.Debug().Msg("Web UI handler configured")

	return w.corsMiddleware(mux)
}

// HasActiveClients returns true if there are active WebSocket clients connected.
func (w *WebUI) HasActiveClients() bool {
	return w.broadcaster.HasActiveClients()
}

// Close gracefully shuts down the WebUI.
func (w *WebUI) Close() {
	w.broadcaster.Close()
}

//go:embed html/*
var htmlFiles embed.FS

// htmlRouterHandlerFunc serves HTML pages based on the request path.
func (w *WebUI) htmlRouterHandlerFunc() func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		path := request.URL.Path

		if strings.HasPrefix(path, "/api/") {
			response.WriteHeader(http.StatusNotFound)
			w.log.Debug().Str("path", path).Msg("API endpoint not found")

			return
		}

		var filename string
		if path == "/" {
			filename = "index.html"
		} else {
			filename = path[1:] + ".html"
		}

		if (filename == "dev.html" || filename == "hardware.html") && !w.config.GetDevToolsEnabled() {
			response.WriteHeader(http.StatusForbidden)
			w.log.Debug().Str("path", path).Msg("access to developer page denied - dev tools not enabled")

			return
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

// corsMiddleware adds CORS headers to all responses.
// TODO: figure out if this is needed, and if so perhaps make it more restrictive via config.
func (w *WebUI) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Access-Control-Allow-Origin", "*")
		response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusOK)

			return
		}

		next.ServeHTTP(response, request)
	})
}

//go:embed static/*
var staticFiles embed.FS

// staticFileHandlerFunc serves static files with automatic content type detection.
// It first tries to load from webui-specific static files, then falls back to shared files.
func (w *WebUI) staticFileHandlerFunc(fileType string) func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, request *http.Request) {
		filename := "static" + request.URL.Path

		content, err := staticFiles.ReadFile(filename)
		if err != nil {
			content, err = webcommon.StaticFiles.ReadFile(filename)
			if err != nil {
				response.WriteHeader(http.StatusNotFound)
				w.log.Error().Err(err).Str("type", fileType).Msg("Invalid file")

				return
			}
		}

		contentType := getContentType(filename)
		response.Header().Set("Content-Type", contentType)

		length, err := response.Write(content)
		if err != nil {
			w.log.Error().Err(err).Int("bytes_written", length).Str("type", fileType).Msg("writing file")

			return
		}

		w.log.Debug().Str("file", filename).Str("mime-type", contentType).Msg("static file served")
	}
}

func (w *WebUI) imagesHandlerFunc() func(http.ResponseWriter, *http.Request) {
	return w.staticFileHandlerFunc("image")
}

func (w *WebUI) sciChartJSHandlerFunc() func(http.ResponseWriter, *http.Request) {
	return w.staticFileHandlerFunc("javascript")
}

func (w *WebUI) cssHandlerFunc() func(http.ResponseWriter, *http.Request) {
	return w.staticFileHandlerFunc("CSS")
}

// getContentType returns the appropriate MIME type based on file extension.
func getContentType(filename string) string {
	ext := filepath.Ext(filename)
	contentType := mime.TypeByExtension(ext)

	if contentType == "" {
		return "application/octet-stream"
	}

	return contentType
}
