package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware"
	"github.com/vwhitteron/simtezilo-dev/app/logstore"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
	"github.com/vwhitteron/simtezilo-dev/app/setupmode"
)

type systemHandlerOptions struct {
	log             zerolog.Logger
	config          *appconfig.Config
	setupMode       *setupmode.SetupMode
	shutdownChan    chan exitcode.Code
	logStore        *logstore.Store
	sendHIDInput    func(key string) bool
	buildVersion    string
	buildCommitHash string
	buildTime       string
	buildPlatform   string
}

type systemHandler struct {
	log             zerolog.Logger
	config          *appconfig.Config
	setupMode       *setupmode.SetupMode
	shutdownChan    chan exitcode.Code
	logStore        *logstore.Store
	sendHIDInput    func(key string) bool
	buildVersion    string
	buildCommitHash string
	buildTime       string
	buildPlatform   string
}

func newSystemHandler(opts systemHandlerOptions) *systemHandler {
	return &systemHandler{
		log:             opts.log,
		config:          opts.config,
		setupMode:       opts.setupMode,
		shutdownChan:    opts.shutdownChan,
		logStore:        opts.logStore,
		sendHIDInput:    opts.sendHIDInput,
		buildVersion:    opts.buildVersion,
		buildCommitHash: opts.buildCommitHash,
		buildTime:       opts.buildTime,
		buildPlatform:   opts.buildPlatform,
	}
}

// handleSystemInfo handles GET requests for system information including build info and hardware platform.
func (h *systemHandler) handleSystemInfo(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	platform := hardware.Platform().String()

	setupModeAvailable := false
	sshEnabled := false

	if h.setupMode != nil {
		setupModeAvailable = h.setupMode.IsAvailable()

		if setupModeAvailable {
			ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
			defer cancel()

			status := h.setupMode.Status(ctx)
			sshEnabled = status.SSHEnabled
			h.log.Debug().
				Bool("available", status.Available).
				Bool("sshEnabled", status.SSHEnabled).
				Msg("Retrieved SSH status from platform")
		}
	}

	responseData := map[string]any{
		"version":            h.buildVersion,
		"commitHash":         h.buildCommitHash,
		"buildTime":          h.buildTime,
		"buildPlatform":      h.buildPlatform,
		"hardware":           platform,
		"setupModeAvailable": setupModeAvailable,
		"sshEnabled":         sshEnabled,
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	err := json.NewEncoder(response).Encode(responseData)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode system info response")
		http.Error(response, "error encoding system info", http.StatusInternalServerError)

		return
	}

	h.log.Debug().Bool("setupModeAvailable", setupModeAvailable).Msg("served system info")
}

// handleRestart handles POST requests to restart the application.
func (h *systemHandler) handleRestart(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")
	h.log.Info().Msg("application restart requested")

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Application restarting...",
	})

	go func() {
		time.Sleep(500 * time.Millisecond)
		h.log.Info().Msg("initiating restart")

		h.shutdownChan <- exitcode.RestartApp
	}()
}

// handleCacheSize returns the size of the cache directory.
func (h *systemHandler) handleCacheSize(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	baseDir := h.config.GetAppBaseDir()
	cacheDir := filepath.Join(baseDir, "data", "cache")

	size, count, err := h.getDirSize(cacheDir)
	if err != nil {
		h.log.Error().Err(err).Str("cache_dir", cacheDir).Msg("failed to get cache size")
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
func (h *systemHandler) handleCacheClear(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	baseDir := h.config.GetAppBaseDir()
	cacheDir := filepath.Join(baseDir, "data", "cache")

	h.log.Info().Str("cache_dir", cacheDir).Msg("clearing cache directory")

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		h.log.Error().Err(err).Str("cache_dir", cacheDir).Msg("failed to read cache directory")
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"success": false,
			"message": "Failed to read cache directory",
		})

		return
	}

	deletedCount := 0
	errorCount := 0

	for _, entry := range entries {
		entryPath := filepath.Join(cacheDir, entry.Name())

		err := os.RemoveAll(entryPath)
		if err != nil {
			h.log.Error().Err(err).Str("path", entryPath).Msg("failed to delete cache entry")

			errorCount++
		} else {
			deletedCount++
		}
	}

	h.log.Info().Int("deleted", deletedCount).Int("errors", errorCount).Msg("cache clear completed")

	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"success": errorCount == 0,
		"message": fmt.Sprintf("Deleted %d items", deletedCount),
		"deleted": deletedCount,
		"errors":  errorCount,
	})
}

// getDirSize returns the total size of all files in a directory (recursive).
func (h *systemHandler) getDirSize(path string) (int64, int, error) {
	var (
		size  int64
		count int
	)

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			h.log.Warn().Err(err).Str("path", filePath).Msg("Get directory size")

			return nil
		}

		if !info.IsDir() {
			size += info.Size()
			count++
		}

		return nil
	})

	h.log.Debug().Int64("size_bytes", size).Int("count", count).Str("path", path).Msg("Get directory size")

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

// handleSetupMode handles POST requests to activate setup mode.
func (h *systemHandler) handleSetupMode(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	h.log.Info().Msg("setup mode requested")

	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()

	_, err := h.setupMode.PlatformAction(ctx, platform.SetupEnable, nil)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to enable setup mode")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Failed to enable setup mode: " + err.Error(),
		})

		return
	}

	h.log.Info().Msg("setup mode enabled")

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Setup mode activated. Application will shut down.",
	})

	go func() {
		time.Sleep(500 * time.Millisecond)
		h.log.Info().Msg("initiating shutdown for setup mode")

		h.shutdownChan <- exitcode.SetupMode
	}()
}

// handleSSHEnable handles POST requests to enable SSH.
func (h *systemHandler) handleSSHEnable(response http.ResponseWriter, request *http.Request) {
	h.manageSSHEnablement(platform.SSHEnable, response, request)
}

// handleSSHDisable handles POST requests to disable SSH.
func (h *systemHandler) handleSSHDisable(response http.ResponseWriter, request *http.Request) {
	h.manageSSHEnablement(platform.SSHDisable, response, request)
}

func (h *systemHandler) manageSSHEnablement(action platform.Command, response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	var actionStr string

	switch action { //nolint:exhaustive // only interested in enable/disable cases
	case platform.SSHEnable:
		actionStr = "enable"
	case platform.SSHDisable:
		actionStr = "disable"
	default:
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status": "error",
			"error":  "Invalid SSH action",
		})

		return
	}

	response.Header().Set("Content-Type", "application/json")

	h.log.Info().Msgf("SSH %s requested", actionStr)

	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()

	_, err := h.setupMode.PlatformAction(ctx, action, nil)
	if err != nil {
		h.log.Error().Err(err).Msgf("failed to %s SSH", actionStr)
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status": "error",
			"error":  "Failed to " + actionStr + " SSH: " + err.Error(),
		})

		return
	}

	h.log.Info().Msgf("SSH %sd successfully", actionStr)

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status": "success",
	})
}

// handleSSHProvision handles POST requests to provision an SSH public key.
func (h *systemHandler) handleSSHProvision(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	publicKey, err := io.ReadAll(request.Body)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to read SSH public key from request")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status": "error",
			"error":  "Failed to read SSH public key",
		})

		return
	}

	h.log.Info().Msg("SSH key provision requested")

	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()

	_, err = h.setupMode.PlatformAction(ctx, platform.SSHProvision, publicKey)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to provision SSH key")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status": "error",
			"error":  "Failed to provision SSH key: " + err.Error(),
		})

		return
	}

	h.log.Info().Msg("SSH key provisioned successfully")

	_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
		"status": "success",
	})
}

// handleFactoryReset handles POST requests to perform a factory reset.
func (h *systemHandler) handleFactoryReset(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	h.log.Warn().Msg("factory reset requested - all settings and network configurations will be deleted")

	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()

	_, err := h.setupMode.PlatformAction(ctx, platform.Reset, nil)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to perform factory reset")

		return
	}

	h.log.Info().Msg("factory reset completed successfully")

	h.log.Info().Msg("initiating shutdown for setup mode after factory reset")

	h.shutdownChan <- exitcode.SetupMode
}

// handleI18nAPI handles GET requests for i18n translations.
func (h *systemHandler) handleI18nAPI(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	lang := request.URL.Query().Get("lang")
	if lang == "" {
		lang = *h.config.GetAppLanguage()
	}

	i18n := h.config.GetI18n()
	if i18n == nil {
		h.log.Error().Msg("i18n instance not available")
		http.Error(response, "i18n not configured", http.StatusInternalServerError)

		return
	}

	translations := i18n.GetStringsWithPrefixForLanguage(lang, "runmode.")

	data, err := json.Marshal(translations)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to marshal translations")
		http.Error(response, "error encoding translations", http.StatusInternalServerError)

		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	length, err := response.Write(data)
	if err != nil {
		h.log.Error().Err(err).Int("bytes_written", length).Msg("error writing i18n response")

		return
	}

	h.log.Debug().Str("language", lang).Msg("served i18n translations")
}

// handleLanguagesAPI handles GET requests for available languages.
func (h *systemHandler) handleLanguagesAPI(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	i18n := h.config.GetI18n()
	if i18n == nil {
		h.log.Error().Msg("i18n instance not available")
		http.Error(response, "i18n not configured", http.StatusInternalServerError)

		return
	}

	languagesMap := i18n.Languages()

	type languageInfo struct {
		Code           string `json:"code"`
		Name           string `json:"name"`
		DefaultCountry string `json:"defaultCountry"`
	}

	languages := make([]languageInfo, 0, len(languagesMap))
	for code, metadata := range languagesMap {
		languages = append(languages, languageInfo{
			Code:           code,
			Name:           metadata.Name,
			DefaultCountry: metadata.DefaultCountry,
		})
	}

	data, err := json.Marshal(languages)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to marshal languages")
		http.Error(response, "error encoding languages", http.StatusInternalServerError)

		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "public, max-age=3600")

	length, err := response.Write(data)
	if err != nil {
		h.log.Error().Err(err).Int("bytes_written", length).Msg("error writing languages response")

		return
	}

	h.log.Debug().Msg("served available languages")
}

// handleLogsAPI returns log entries from the in-memory store.
func (h *systemHandler) handleLogsAPI(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	if h.logStore == nil {
		h.log.Warn().Msg("log store not initialized")
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Log store not available"}) //nolint:errchkjson // simple encoding

		return
	}

	queryParams := request.URL.Query()
	page := parsePageParam(queryParams.Get("page"))
	pageSize := parsePageSizeParam(queryParams.Get("pageSize"))
	levelFilters := parseLevelFilters(queryParams.Get("levels"))

	allLogs := h.logStore.GetAll()
	filteredLogs := filterLogsByLevel(allLogs, levelFilters)

	totalCount := len(filteredLogs)
	totalPages := max((totalCount+pageSize-1)/pageSize, 1)

	if page > totalPages {
		page = totalPages
	}

	offset := (page - 1) * pageSize
	endIdx := min(offset+pageSize, totalCount)

	var logs []logstore.LogEntry
	if offset < totalCount {
		logs = filteredLogs[offset:endIdx]
	} else {
		logs = []logstore.LogEntry{}
	}

	stats := h.logStore.GetStats()

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
		h.log.Error().Err(err).Msg("failed to encode logs response")
		http.Error(response, "error encoding logs", http.StatusInternalServerError)

		return
	}

	h.log.Debug().Int("log_count", len(logs)).Int("page", page).Msg("served logs")
}

// parsePageParam parses the "page" query parameter, defaulting to 1.
func parsePageParam(raw string) int {
	if raw == "" {
		return 1
	}

	var page int

	n, err := fmt.Sscanf(raw, "%d", &page)
	if err != nil || n != 1 || page < 1 {
		return 1
	}

	return page
}

// parsePageSizeParam parses the "pageSize" query parameter, clamped to [1, 1000], defaulting to 100.
func parsePageSizeParam(raw string) int {
	if raw == "" {
		return 100
	}

	var pageSize int

	n, err := fmt.Sscanf(raw, "%d", &pageSize)
	if err != nil || n != 1 || pageSize < 1 {
		return 100
	}

	if pageSize > 1000 {
		return 1000
	}

	return pageSize
}

// parseLevelFilters parses a comma-separated "levels" query parameter into a set.
// Returns nil if the parameter is empty (meaning no filter).
func parseLevelFilters(raw string) map[string]bool {
	if raw == "" {
		return nil
	}

	filters := make(map[string]bool)

	for level := range strings.SplitSeq(raw, ",") {
		trimmed := strings.TrimSpace(level)
		if trimmed != "" {
			filters[trimmed] = true
		}
	}

	return filters
}

// filterLogsByLevel returns only the log entries that match the level filter set.
// If levelFilters is nil or empty, all entries are returned.
func filterLogsByLevel(allLogs []logstore.LogEntry, levelFilters map[string]bool) []logstore.LogEntry {
	if len(levelFilters) == 0 {
		return allLogs
	}

	filtered := make([]logstore.LogEntry, 0, len(allLogs))

	for _, entry := range allLogs {
		if level, ok := entry["level"].(string); ok && levelFilters[level] {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// handleHardwareInput injects a HID button press from the hardware dev view.
func (h *systemHandler) handleHardwareInput(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if !h.config.GetDevToolsEnabled() {
		response.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "developer tools not enabled"}) //nolint:errchkjson // simple encoding

		return
	}

	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	var body struct {
		Key string `json:"key"`
	}

	err := json.NewDecoder(request.Body).Decode(&body)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Invalid JSON data"}) //nolint:errchkjson // simple encoding

		return
	}

	if h.sendHIDInput == nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "HID input unavailable"}) //nolint:errchkjson // simple encoding

		return
	}

	if !h.sendHIDInput(body.Key) {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "unknown or dropped key: " + body.Key}) //nolint:errchkjson // simple encoding

		return
	}

	_ = json.NewEncoder(response).Encode(map[string]any{"status": "ok", "key": body.Key}) //nolint:errchkjson // simple encoding
}
