package webui

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/exitcode"
	"github.com/vwhitteron/simtezilo-dev/app/updater"
)

type updateHandlerOptions struct {
	log          zerolog.Logger
	config       *appconfig.Config
	updater      *updater.Updater
	buildVersion string
	shutdownChan chan exitcode.Code
}

type updateHandler struct {
	log          zerolog.Logger
	config       *appconfig.Config
	updater      *updater.Updater
	buildVersion string
	shutdownChan chan exitcode.Code
}

func newUpdateHandler(opts updateHandlerOptions) *updateHandler {
	return &updateHandler{
		log:          opts.log,
		config:       opts.config,
		updater:      opts.updater,
		buildVersion: opts.buildVersion,
		shutdownChan: opts.shutdownChan,
	}
}

// UploadMetadata represents metadata embedded in a custom update archive.
type UploadMetadata struct {
	Version     string    `json:"version"`
	ReleaseDate time.Time `json:"releaseDate"`
	Changelog   []string  `json:"changelog"`
	Platform    string    `json:"platform"`
}

// extractMetadataFromArchive attempts to extract manifest.json from an uploaded archive.
func (h *updateHandler) extractMetadataFromArchive(file io.ReadSeeker, filename string) (*UploadMetadata, error) {
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("failed to seek file: %w", err)
	}

	if strings.HasSuffix(filename, ".zip") {
		return h.extractMetadataFromZip(file)
	} else if strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz") {
		return h.extractMetadataFromTarGz(file)
	}

	return nil, fmt.Errorf("unsupported archive format: %s", filename)
}

// extractMetadataFromZip extracts metadata from a ZIP archive.
func (h *updateHandler) extractMetadataFromZip(file io.ReadSeeker) (*UploadMetadata, error) {
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to get file size: %w", err)
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("failed to seek file: %w", err)
	}

	zipReader, err := zip.NewReader(file.(io.ReaderAt), size) //nolint:forcetypeassert // unnecessary
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}

	for _, f := range zipReader.File {
		if f.Name == "manifest.json" || strings.HasSuffix(f.Name, "/manifest.json") {
			manifest, openErr := f.Open()
			if openErr != nil {
				return nil, fmt.Errorf("failed to open metadata file: %w", openErr)
			}
			defer manifest.Close()

			var metadata UploadMetadata

			decodeErr := json.NewDecoder(manifest).Decode(&metadata)
			if decodeErr != nil {
				return nil, fmt.Errorf("failed to decode metadata: %w", decodeErr)
			}

			return &metadata, nil
		}
	}

	return nil, errors.New("metadata file not found in archive")
}

// extractMetadataFromTarGz extracts metadata from a tar.gz archive.
func (h *updateHandler) extractMetadataFromTarGz(file io.ReadSeeker) (*UploadMetadata, error) {
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to open gzip: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, tarErr := tarReader.Next()
		if tarErr == io.EOF {
			break
		}

		if tarErr != nil {
			return nil, fmt.Errorf("failed to read tar: %w", tarErr)
		}

		if header.Name == "manifest.json" || strings.HasSuffix(header.Name, "/manifest.json") {
			var metadata UploadMetadata

			decodeErr := json.NewDecoder(tarReader).Decode(&metadata)
			if decodeErr != nil {
				return nil, fmt.Errorf("failed to decode metadata: %w", decodeErr)
			}

			return &metadata, nil
		}
	}

	return nil, errors.New("metadata file not found in archive")
}

// handleUpdatesStatus returns the current update status.
func (h *updateHandler) handleUpdatesStatus(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	responseData := map[string]any{
		"enabled":              h.config.GetAppUpdateAutoCheck(),
		"autoInstall":          h.config.GetAppUpdateAutoInstall(),
		"checkIntervalMinutes": h.config.GetAppUpdateCheckIntervalMinutes(),
		"currentVersion":       h.buildVersion,
		"channel":              h.config.GetAppUpdateChannel(),
		"status":               "disabled",
		"rollbackAvailable":    false,
		"rollbackVersion":      "",
	}

	if h.updater != nil {
		h.populateUpdaterStatus(responseData)
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	err := json.NewEncoder(response).Encode(responseData)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode updates status response")
		http.Error(response, "error encoding updates status", http.StatusInternalServerError)

		return
	}

	statusStr, ok := responseData["status"].(string)
	if !ok {
		statusStr = "unknown"
	}

	h.log.Debug().Str("status", statusStr).Msg("served updates status")
}

// populateUpdaterStatus fills in the live updater fields of the status response map.
func (h *updateHandler) populateUpdaterStatus(responseData map[string]any) {
	status := h.updater.Status()
	availableUpdate := h.updater.AvailableUpdate()
	lastCheck := h.updater.Checker().LastCheck()
	lastError := h.updater.Checker().LastError()
	currentChannel := h.config.GetAppUpdateChannel()

	responseData["status"] = status.String()
	responseData["rollbackAvailable"] = h.updater.RollbackAvailable()
	responseData["rollbackVersion"] = h.updater.RollbackVersion()

	downloadReady := status == updater.UpdateStatusReadyToInstall
	if downloadReady && availableUpdate != nil {
		downloadReady = availableUpdate.Channel == currentChannel
	}

	responseData["downloadReady"] = downloadReady

	if status == updater.UpdateStatusDownloading {
		responseData["download_progress"] = h.updater.Checker().DownloadProgress()
	}

	if !lastCheck.IsZero() {
		responseData["lastChecked"] = lastCheck.Format(time.RFC3339)
	}

	if lastError != nil {
		responseData["error"] = lastError.Error()
	}

	if availableUpdate != nil {
		responseData["availableUpdate"] = map[string]any{
			"version":     availableUpdate.AvailableVersion,
			"releaseDate": availableUpdate.ReleaseDate,
			"changelog":   availableUpdate.Changelog,
			"downloadURL": availableUpdate.DownloadURL,
			"size":        availableUpdate.DownloadSize,
			"channel":     availableUpdate.Channel,
		}
	}
}

// handleUpdatesCheck triggers an immediate update check.
func (h *updateHandler) handleUpdatesCheck(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	responseData := map[string]any{
		"updateAvailable": false,
		"currentVersion":  h.buildVersion,
		"downloadReady":   false,
	}

	if h.updater == nil {
		h.log.Debug().Msg("update check requested but updater not available")
	} else {
		updateInfo, err := h.updater.CheckNow() //nolint:contextcheck // CheckNow manages its own timeout context
		if err != nil {
			h.log.Error().Err(err).Msg("failed to check for updates")
			response.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(response).Encode(map[string]string{"error": err.Error()}) //nolint:errchkjson // simple encoding

			return
		}

		status := h.updater.Status()
		responseData["updateAvailable"] = updateInfo != nil
		responseData["downloadReady"] = status == updater.UpdateStatusReadyToInstall

		if updateInfo != nil {
			responseData["availableUpdate"] = map[string]any{
				"version":     updateInfo.AvailableVersion,
				"releaseDate": updateInfo.ReleaseDate,
				"changelog":   updateInfo.Changelog,
				"downloadURL": updateInfo.DownloadURL,
				"size":        updateInfo.DownloadSize,
			}
		}
	}

	response.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(response).Encode(responseData)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode update check response")
		http.Error(response, "error encoding update check response", http.StatusInternalServerError)

		return
	}

	updateAvailable, ok := responseData["updateAvailable"].(bool)
	if !ok {
		updateAvailable = false
	}

	h.log.Info().
		Bool("updateAvailable", updateAvailable).
		Msg("manual update check completed")
}

// handleUpdatesDownload downloads an available update.
func (h *updateHandler) handleUpdatesDownload(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	if h.updater == nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Updater not available"}) //nolint:errchkjson // simple encoding

		return
	}

	availableUpdate := h.updater.AvailableUpdate()
	if availableUpdate == nil {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "No update available"}) //nolint:errchkjson // simple encoding

		return
	}

	h.log.Info().
		Str("version", availableUpdate.AvailableVersion).
		Msg("starting update download")

	err := h.updater.DownloadAndPrepare(request.Context(), func(progress updater.DownloadProgress) {
		h.updater.Checker().SetDownloadProgress(progress.Percent)

		h.log.Debug().
			Int64("downloaded", progress.DownloadedBytes).
			Int64("total", progress.TotalBytes).
			Float64("percent", progress.Percent).
			Msg("download progress")
	})
	if err != nil {
		h.log.Error().Err(err).Msg("failed to download update")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": err.Error()}) //nolint:errchkjson // simple encoding

		return
	}

	response.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(response).Encode(map[string]any{
		"success": true,
		"version": availableUpdate.AvailableVersion,
		"message": "Update downloaded and prepared for installation",
	})
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode download response")
		http.Error(response, "error encoding download response", http.StatusInternalServerError)

		return
	}

	h.log.Info().
		Str("version", availableUpdate.AvailableVersion).
		Msg("update downloaded and staged for installation")
}

// handleUpdatesUpload handles custom update file uploads.
func (h *updateHandler) handleUpdatesUpload(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	if h.updater == nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Updater not available"}) //nolint:errchkjson // simple encoding

		return
	}

	file, header, err := h.parseUploadForm(request, response)
	if err != nil {
		return
	}

	defer file.Close()

	h.log.Info().Str("filename", header.Filename).Int64("size", header.Size).Msg("Receiving custom update upload")

	metadata := h.tryExtractMetadata(file, header.Filename)

	destPath, prefixedFilename, err := h.saveUploadedFile(file, header.Filename, response)
	if err != nil {
		return
	}

	version, changelog, releaseDate := resolveUploadMetadata(metadata, header.Filename)

	h.updater.Checker().SetStatus(updater.UpdateStatusReadyToInstall)
	h.updater.Checker().SetAvailableUpdate(&updater.UpdateInfo{
		CurrentVersion:   h.buildVersion,
		AvailableVersion: version,
		Channel:          "custom",
		Changelog:        changelog,
		DownloadURL:      "",
		DownloadSize:     header.Size,
		SHA256:           "",
		ReleaseDate:      releaseDate,
	})

	err = h.updater.PrepareInstall(destPath)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to prepare custom update for installation")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to prepare update for installation"}) //nolint:errchkjson // simple encoding

		return
	}

	h.log.Info().Str("filename", prefixedFilename).Str("version", version).Str("path", destPath).Msg("Custom update uploaded and ready to install")

	response.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(response).Encode(map[string]any{
		"success":  true,
		"filename": prefixedFilename,
		"size":     header.Size,
		"version":  version,
		"message":  "Update uploaded and ready to install",
	})
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode upload response")
		http.Error(response, "error encoding upload response", http.StatusInternalServerError)

		return
	}
}

// parseUploadForm parses the multipart upload and returns the file and its header.
// On error it writes the appropriate HTTP error and returns a non-nil error.
func (h *updateHandler) parseUploadForm(request *http.Request, response http.ResponseWriter) (multipart.File, *multipart.FileHeader, error) {
	err := request.ParseMultipartForm(500 << 20)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to parse multipart form")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to parse upload"}) //nolint:errchkjson // simple encoding

		return nil, nil, err
	}

	file, header, err := request.FormFile("file")
	if err != nil {
		h.log.Error().Err(err).Msg("failed to get uploaded file")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "No file uploaded"}) //nolint:errchkjson // simple encoding

		return nil, nil, err
	}

	return file, header, nil
}

// tryExtractMetadata attempts to extract metadata from the uploaded archive.
// It logs a warning and returns nil if extraction fails.
func (h *updateHandler) tryExtractMetadata(file multipart.File, filename string) *UploadMetadata {
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		return nil
	}

	metadata, err := h.extractMetadataFromArchive(seeker, filename)
	if err != nil {
		h.log.Warn().Err(err).Msg("failed to extract metadata from archive, using defaults")

		return nil
	}

	h.log.Info().Str("version", metadata.Version).Str("platform", metadata.Platform).Msg("Extracted metadata from archive")

	_, _ = seeker.Seek(0, io.SeekStart)

	return metadata
}

// saveUploadedFile writes the uploaded file to the download directory and sets its permissions.
// Returns the destination path, the prefixed filename, and any error (HTTP error written on failure).
func (h *updateHandler) saveUploadedFile(file multipart.File, filename string, response http.ResponseWriter) (string, string, error) {
	downloadDir := filepath.Join(h.config.GetAppBaseDir(), "data", "update", "downloads")

	err := os.MkdirAll(downloadDir, 0o755)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to create download directory")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to create download directory"}) //nolint:errchkjson // simple encoding

		return "", "", err
	}

	h.log.Debug().Msg("Cleaning up old uploads before saving new custom upload")
	_ = h.updater.Downloader().CleanupDownloads()

	prefixedFilename := "custom-" + filename
	destPath := filepath.Join(downloadDir, prefixedFilename)

	destFile, err := os.Create(destPath)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to create destination file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to save upload"}) //nolint:errchkjson // simple encoding

		return "", "", err
	}

	defer destFile.Close()

	_, err = io.Copy(destFile, file)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to write uploaded file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to write file"}) //nolint:errchkjson // simple encoding

		return "", "", err
	}

	err = os.Chmod(destPath, 0o755)
	if err != nil {
		h.log.Warn().Err(err).Msg("failed to set executable permissions")
	}

	return destPath, prefixedFilename, nil
}

// resolveUploadMetadata returns version, changelog, and release date from metadata or defaults.
func resolveUploadMetadata(metadata *UploadMetadata, filename string) (string, []string, time.Time) {
	if metadata != nil {
		return metadata.Version, metadata.Changelog, metadata.ReleaseDate
	}

	return "custom-" + filepath.Base(filename),
		[]string{"Custom uploaded file: " + filename},
		time.Now()
}

// handleUpdatesInstall triggers installation of a downloaded update (restarts the service).
func (h *updateHandler) handleUpdatesInstall(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	if h.updater == nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Updater not available"}) //nolint:errchkjson // simple encoding

		return
	}

	status := h.updater.Status()
	if status != updater.UpdateStatusReadyToInstall {
		response.WriteHeader(http.StatusBadRequest)
		//nolint:errchkjson // simple encoding
		_ = json.NewEncoder(response).Encode(map[string]string{
			"error":  "No update ready to install",
			"status": status.String(),
		})

		return
	}

	h.log.Info().Msg("triggering service restart to apply update")

	h.updater.SetStatus(updater.UpdateStatusInstalling)

	response.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(response).Encode(map[string]any{
		"success": true,
		"message": "Service will restart to apply update",
	})
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode install response")
	}

	go func() {
		time.Sleep(500 * time.Millisecond)

		h.shutdownChan <- exitcode.RestartApp
	}()
}

// handleUpdatesRollback handles POST /api/updates/rollback.
func (h *updateHandler) handleUpdatesRollback(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	if h.updater == nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Updater not available"}) //nolint:errchkjson // simple encoding

		return
	}

	if !h.updater.RollbackAvailable() {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "No rollback version available"}) //nolint:errchkjson // simple encoding

		return
	}

	h.log.Info().Msg("triggering rollback to previous version")

	err := h.updater.Rollback()
	if err != nil {
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": err.Error()}) //nolint:errchkjson // simple encoding

		return
	}

	response.Header().Set("Content-Type", "application/json")

	encErr := json.NewEncoder(response).Encode(map[string]any{
		"success": true,
		"message": "Service will restart with previous version",
	})
	if encErr != nil {
		h.log.Error().Err(encErr).Msg("failed to encode rollback response")
	}

	go func() {
		time.Sleep(500 * time.Millisecond)

		h.shutdownChan <- exitcode.RestartApp
	}()
}
