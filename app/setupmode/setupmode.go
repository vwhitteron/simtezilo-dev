package setupmode

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/rs/zerolog"
	"github.com/skip2/go-qrcode"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/sprites"
)

type SetupMode struct {
	log      zerolog.Logger
	config   *config.Config
	command  string
	done     chan<- exitcode.Code
	shutdown chan struct{}
	lcd      *display.ST7789LCD
}

type Options struct {
	Config  *config.Config
	Done    chan<- exitcode.Code
	Logger  *zerolog.Logger
	Display *display.ST7789LCD
}

func New(opts Options) *SetupMode {
	// Create a local channel to signal when we should exit
	shutdown := make(chan struct{})

	return &SetupMode{
		config:   opts.Config,
		done:     opts.Done,
		command:  filepath.Join(opts.Config.GetAppBaseDir(), "bin", "setup"),
		log:      opts.Logger.With().Str("component", "setupmode").Logger(),
		shutdown: shutdown,
		lcd:      opts.Display,
	}
}

// GetHTTPHandler returns the HTTP handler for setup mode.
func (s *SetupMode) GetHTTPHandler() http.Handler {
	// Create a new ServeMux with all setup mode routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/css/", func(w http.ResponseWriter, r *http.Request) {
		handleStaticFiles(w, r, &s.log)
	})
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		handleStaticFiles(w, r, &s.log)
	})
	mux.HandleFunc("/js/", func(w http.ResponseWriter, r *http.Request) {
		handleStaticFiles(w, r, &s.log)
	})
	mux.HandleFunc("/api/i18n", func(w http.ResponseWriter, r *http.Request) {
		handleAPIGetI18n(w, r, &s.log)
	})
	mux.HandleFunc("/api/languages", func(w http.ResponseWriter, r *http.Request) {
		handleAPIGetLanguages(w, r, &s.log)
	})
	mux.HandleFunc("/api/networks", func(w http.ResponseWriter, r *http.Request) {
		s.handleAPIGetNetworks(w, r)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		s.handleAPISetupStatus(w, r)
	})
	mux.HandleFunc("/api/config/save", func(w http.ResponseWriter, r *http.Request) {
		s.handleAPIConfigSave(w, r)
	})
	mux.HandleFunc("/api/mode/run", func(w http.ResponseWriter, r *http.Request) {
		s.handleModeRun(w, r)
	})

	s.log.Debug().Msg("Setup mode handler configured")

	return mux
}

// Run starts the setup wizard for configuring WiFi network.
func (s *SetupMode) Run() {
	// Check setup status first
	s.log.Info().Msg("Checking setup status")

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 10*time.Second)
	status := s.Status(statusCtx)

	statusCancel()

	// If setup mode is not present, initialize it
	if !status.SetupModePresent {
		s.log.Info().Msg("Setup mode not present, running setup init")

		initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer initCancel()

		_, err := s.runSetupCommand(initCtx, "init", nil)
		if err != nil {
			s.log.Error().Err(err).Msg("Failed to initialize setup mode")
			s.showErrorSprite()

			s.done <- exitcode.GeneralErr

			close(s.shutdown)

			return
		}

		s.log.Info().Msg("Setup mode initialized successfully")
	}

	// Ensure setup mode is active
	s.log.Info().Msg("Activating setup mode connection")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := s.runSetupCommand(ctx, "mode-setup", nil)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to activate setup mode, attempting to continue")
	} else {
		s.log.Info().Msg("Setup mode connection activated")
	}

	// Handle keyboard input
	go func() {
		_ = keyboard.Listen(func(key keys.Key) (stop bool, err error) {
			switch key.Code { //nolint:exhaustive
			case keys.CtrlC, keys.Escape:
				s.log.Info().Msg("Escape key pressed, shutting down")

				s.done <- exitcode.GeneralErr

				close(s.shutdown)

				return true, nil
			}

			return false, nil
		})
	}()

	code, err := s.generateQRcode()
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to generate QR code")

		s.showErrorSprite()

		s.done <- exitcode.GeneralErr

		close(s.shutdown)
	}

	// display the qrcode image on the lcd
	canvas := gui.ImageToRGBA(code)
	content := &display.Content{
		Canvas: canvas,
	}

	err = s.lcd.Write(content)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to write to display")
	} else {
		s.lcd.Wakeup()
	}

	// Wait for shutdown signal
	// This will be triggered by either successful configuration (handleSave)
	// or keyboard interrupt, or signal from main
	<-s.shutdown
}

//go:embed html/index.html
var indexHTML string

func handleRoot(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(writer, indexHTML)
}

//go:embed static/*
var staticFiles embed.FS

func handleStaticFiles(writer http.ResponseWriter, request *http.Request, logger *zerolog.Logger) {
	filename := "static" + request.URL.Path

	content, err := staticFiles.ReadFile(filename)
	if err != nil {
		writer.WriteHeader(http.StatusNotFound)
		logger.Error().Err(err).Str("file", filename).Msg("Static file not found")

		return
	}

	contentType := getContentType(filename)
	writer.Header().Set("Content-Type", contentType)
	writer.Header().Set("Cache-Control", "public, max-age=31536000")

	length, err := writer.Write(content)
	if err != nil {
		logger.Error().Err(err).Int("bytes_written", length).Msg("Error writing static file")

		return
	}

	logger.Debug().Str("file", filename).Str("mime-type", contentType).Msg("Served static file")
}

func handleAPIGetLanguages(writer http.ResponseWriter, _ *http.Request, logger *zerolog.Logger) {
	// Create i18n instance to get available languages
	langCode := "en"

	i18nInstance, err := i18n.New(&langCode, *logger)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create i18n instance")
		http.Error(writer, fmt.Sprintf("Error fetching languages: %v", err), http.StatusInternalServerError)

		return
	}

	languagesMap := i18nInstance.Languages()

	// Build response as array of language objects
	type languageInfo struct {
		Code string `json:"code"` //nolint:tagliatelle
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
		logger.Error().Err(err).Msg("Failed to marshal languages")
		http.Error(writer, fmt.Sprintf("Error encoding languages: %v", err), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	length, err := writer.Write(data)
	if err != nil {
		logger.Error().Err(err).Int("bytes_written", length).Msg("Error writing languages response")

		return
	}

	logger.Debug().Int("count", len(languages)).Msg("Served languages list")
}

func handleAPIGetI18n(writer http.ResponseWriter, request *http.Request, logger *zerolog.Logger) {
	// Get language from query parameter, default to English
	lang := request.URL.Query().Get("lang")
	if lang == "" {
		lang = "en"
	}

	// Create i18n instance to get translations from languagedb
	i18nInstance, err := i18n.New(&lang, *logger)
	if err != nil {
		logger.Error().Err(err).Str("lang", lang).Msg("Failed to create i18n instance")
		http.Error(writer, fmt.Sprintf("Error loading language: %v", err), http.StatusInternalServerError)

		return
	}

	// Get all translations with the "setupmode." prefix
	translations := i18nInstance.GetStringsWithPrefix("setupmode.")

	data, err := json.Marshal(translations)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to marshal translations")
		http.Error(writer, fmt.Sprintf("Error encoding translations: %v", err), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	length, err := writer.Write(data)
	if err != nil {
		logger.Error().Err(err).Int("bytes_written", length).Msg("Error writing i18n response")

		return
	}

	logger.Debug().Str("language", lang).Msg("Served i18n translations")
}

func (s *SetupMode) handleAPIGetNetworks(writer http.ResponseWriter, request *http.Request) {
	networks, err := s.getAvailableNetworks(request.Context())
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to get available networks")
		http.Error(writer, fmt.Sprintf("Error fetching networks: %v", err), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, networks)
}

func (s *SetupMode) handleAPISetupStatus(writer http.ResponseWriter, request *http.Request) {
	status := s.Status(request.Context())
	if !status.Available {
		s.log.Error().Msg("Failed to get setup status")
		http.Error(writer, "Failed to get setup status", http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(writer).Encode(status)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to encode status response")
		http.Error(writer, "Failed to encode response", http.StatusInternalServerError)
	}
}

func (s *SetupMode) handleAPIConfigSave(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	ctx := request.Context()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := s.saveNetworkConfiguration(
		ctx,
		request.FormValue("ssid"),
		request.FormValue("password"),
		request.FormValue("security"),
		request.FormValue("method"),
		request.FormValue("ipAddress"),
		request.FormValue("prefix"),
		request.FormValue("gateway"),
		request.FormValue("dns"),
	)
	if err != nil {
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"success":false,"error":"Failed to save configuration: %v"}`, err)

		s.log.Error().Err(err).Msg("Save configuration failed")

		return
	}

	s.log.Info().Msg("Configuration saved successfully, switching to run mode")

	_, err = s.runSetupCommand(ctx, "mode-run", nil)
	if err != nil {
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"success":false,"error":"Failed to enter run mode: %v"}`, err)

		s.log.Error().Err(err).Msg("Failed to enter run mode")

		return
	}

	_, err = s.runSetupCommand(ctx, "disable", nil)
	if err != nil {
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"success":false,"error":"Failed to disable setup mode: %v"}`, err)

		s.log.Error().Err(err).Msg("Failed to disable setup mode flag")

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, `{"success":true,"message":"Configuration saved successfully"}`)

	// Explicitly flush the response to ensure it reaches the client
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}

	s.log.Info().Int("exitCode", int(exitcode.Success)).Msg("Network configuration completed successfully, sending exit code")

	// Do not send to done channel for normal config save; handled by return flow

	close(s.shutdown)
}

func (s *SetupMode) handleModeRun(writer http.ResponseWriter, request *http.Request) {
	s.log.Info().Msg("User cancelled setup, returning to run mode without saving")

	ctx := request.Context()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := s.runSetupCommand(ctx, "mode-run", nil)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to enter run mode")
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"success":false,"error":"Failed to enter run mode: %v"}`, err)

		return
	}

	_, err = s.runSetupCommand(ctx, "disable", nil)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to disable setup mode flag")
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"success":false,"error":"Failed to disable setup mode: %v"}`, err)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, `{"success":true,"message":"Returning to run mode"}`)

	// Explicitly flush the response to ensure it reaches the client
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}

	s.log.Info().Int("exitCode", int(exitcode.Success)).Msg("Setup cancelled by user, sending success exit code")

	// Do not send to done channel for normal return to run mode; handled by return flow

	close(s.shutdown)
}

// WIFI:S:<SSID>;T:<AUTH>;P:<PASSWORD>;H:<true|false|blank>;;
// S (SSID): *required* The network name (SSID) of the Wi-Fi network.
// T (authentication type): The network encryption type (WPA, WPA2, WPA3, or WEP). Leave empty for open networks with no password.
// P (password): The network password. This field is ignored if the network does not have authentication.
// H (hidden network): *optional* Set to "true" if the SSID is not broadcast.
func (s *SetupMode) generateQRcode() (image.Image, error) {
	ssid, psk, security, err := s.getNetworkAccessDetails()
	if err != nil {
		return image.Black, fmt.Errorf("get network access details: %w", err)
	}

	networkAuth := strings.ToUpper(security)
	networkHidden := "false"

	networkDef := "WIFI:S:" + ssid + ";T:" + networkAuth + ";P:" + psk + ";H:" + networkHidden + ";"

	qrCode, err := qrcode.New(networkDef, qrcode.Medium)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to generate QR code")

		return image.Black, fmt.Errorf("generate QR code: %w", err)
	}

	qrCode.BackgroundColor = image.Black
	qrCode.ForegroundColor = color.Gray{Y: 180}

	return qrCode.Image(240), nil
}

func (s *SetupMode) getAvailableNetworks(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	response, err := s.runSetupCommand(ctx, "scan", nil)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to scan WiFi networks")

		return "", err
	}

	// Marshal the networks array to return as JSON string
	data, err := json.Marshal(response.Networks)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to marshal networks")

		return "", fmt.Errorf("failed to marshal networks: %w", err)
	}

	return string(data), nil
}

func (s *SetupMode) getNetworkAccessDetails() (string, string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	response, err := s.runSetupCommand(ctx, "access", nil)
	if err != nil {
		return "", "", "", fmt.Errorf("run setup access command: %w", err)
	}

	if response.WiFi == nil {
		return "", "", "", errors.New("no wifi details in response")
	}

	return strings.TrimSpace(response.WiFi.SSID), strings.TrimSpace(response.WiFi.PSK), strings.TrimSpace(response.WiFi.Security), nil
}

func (s *SetupMode) saveNetworkConfiguration(ctx context.Context, ssid, password, security, method, ipAddress, prefix, gateway, dns string) error {
	// Prepare JSON payload for setup binary
	config := []map[string]string{
		{
			"ssid":     ssid,
			"psk":      password,
			"security": security,
			"method":   method,
			"ip":       ipAddress,
			"prefix":   prefix,
			"gateway":  gateway,
			"dns":      dns,
		},
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Call setup binary with provision command
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, err = s.runSetupCommand(ctx, "provision", configJSON)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to provision network")

		return err
	}

	s.log.Info().Msg("Network provisioned successfully")

	return nil
}

func getContentType(filename string) string {
	ext := filepath.Ext(filename)
	contentType := mime.TypeByExtension(ext)

	if contentType == "" {
		return "application/octet-stream"
	}

	return contentType
}

// showErrorSprite displays the error sprite on the LCD before exiting.
func (s *SetupMode) showErrorSprite() {
	if s.lcd == nil {
		s.log.Warn().Msg("LCD not initialized, cannot display error sprite")

		return
	}

	// Load sprite set
	spriteSet, err := sprites.NewSpriteSet()
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to load sprite set for error display")

		return
	}

	// Get error sprite
	errorSprite := spriteSet.GetSprite(sprites.ErrorSprite)
	canvas := gui.ImageToRGBA(errorSprite)

	// Display error sprite
	content := &display.Content{
		Canvas: canvas,
	}

	err = s.lcd.Write(content)
	if err != nil {
		s.log.Error().Err(err).Msg("Failed to write error sprite to display")

		return
	}

	s.lcd.Wakeup()

	// Give time for the user to see the error sprite
	time.Sleep(3 * time.Second)
}
