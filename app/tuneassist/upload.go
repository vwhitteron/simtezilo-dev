package tuneassist

import (
	"compress/flate"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// maxUploadBody caps the gzip body of an upload. The inflated size is capped
// separately by maxInflatedReplay.
const maxUploadBody = 256 << 20

// uploadPrefix and uploadSuffix frame an upload's cache file name around its ID.
const (
	uploadPrefix = "upload-"
	uploadSuffix = ".gtr"
)

// uploadIDPattern matches an upload ID: the SHA-256 of the inflated bytes, as 64
// lowercase hex characters. Matching it also keeps a request from naming a path.
var uploadIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// errBadUpload reports an upload body that is not a non-empty gzip stream.
var errBadUpload = errors.New("upload is not a non-empty gzip stream")

// errUploadNotFound reports an upload ID with no file in the cache.
var errUploadNotFound = errors.New("unknown upload")

// HandleUpload stores a gzip-compressed .gtr packet stream in the cache and returns
// its content ID, so /data and /audio can read it with upload=<id>. The ID is the
// SHA-256 of the inflated bytes, so uploading the same recording twice yields one file.
func (s *Service) HandleUpload(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	dir := s.cacheDir()
	if dir == "" {
		http.Error(response, errNoCacheDir.Error(), http.StatusServiceUnavailable)

		return
	}

	// The slot is held across the write, so the sweep cannot delete the file between
	// the write and the first read.
	release, err := s.acquire(request.Context())
	if err != nil {
		s.log.Debug().Err(err).Msg("acquiring heavy job slot")

		return
	}
	defer release()

	body := http.MaxBytesReader(response, request.Body, maxUploadBody)
	defer body.Close()

	name, err := storeUpload(dir, body)
	if err != nil {
		status := uploadErrorStatus(err)
		if status == http.StatusInternalServerError {
			s.log.Error().Err(err).Msg("storing upload")
		}

		http.Error(response, err.Error(), status)

		return
	}

	s.scheduleSweep(s.cacheTTL)

	response.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(response).Encode(map[string]string{"id": name})
	if err != nil {
		s.log.Error().Err(err).Str("upload", name).Msg("encoding upload id")
	}
}

// uploadErrorStatus maps a storeUpload failure to its HTTP status.
func uploadErrorStatus(err error) int {
	var tooLarge *http.MaxBytesError

	switch {
	case errors.As(err, &tooLarge), errors.Is(err, errReplayTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, errBadUpload):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// isGzipFault reports whether err means the bytes were not a valid gzip stream, as
// opposed to a read or disk failure.
func isGzipFault(err error) bool {
	var corrupt flate.CorruptInputError

	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, gzip.ErrHeader) ||
		errors.Is(err, gzip.ErrChecksum) ||
		errors.As(err, &corrupt)
}

// storeUpload inflates a gzip stream into the cache directory and returns the ID of
// the stored file. The inflated bytes are hashed as they are written, then the
// temporary file is renamed to upload-<id>.gtr. If that file already exists, its
// modification time is refreshed and the temporary file is discarded.
//
// The temporary name contains ".gtr.", so the cache sweep removes one left behind by
// an interrupted write.
func storeUpload(dir string, body io.Reader) (string, error) {
	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	reader, err := gzip.NewReader(body)
	if err != nil {
		return "", classifyUploadError("reading gzip header", err)
	}

	defer reader.Close()

	temp, err := os.CreateTemp(dir, uploadPrefix+"*"+uploadSuffix+".part")
	if err != nil {
		return "", fmt.Errorf("creating upload file: %w", err)
	}

	defer os.Remove(temp.Name())

	hash := sha256.New()

	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(reader, maxInflatedReplay+1))
	if err != nil {
		temp.Close()

		return "", classifyUploadError("inflating upload", err)
	}

	err = temp.Close()
	if err != nil {
		return "", fmt.Errorf("closing upload file: %w", err)
	}

	if written > maxInflatedReplay {
		return "", errReplayTooLarge
	}

	if written == 0 {
		return "", fmt.Errorf("%w: no packets", errBadUpload)
	}

	return publishUpload(dir, temp.Name(), hex.EncodeToString(hash.Sum(nil)))
}

// publishUpload renames the finished temporary file to upload-<digest>.gtr. If that
// file already exists, it is refreshed and the temporary file is left to be removed.
func publishUpload(dir, tempPath, digest string) (string, error) {
	path := uploadPath(dir, digest)

	_, err := os.Stat(path)
	if err == nil {
		return digest, refreshUpload(path)
	}

	if !os.IsNotExist(err) {
		return "", fmt.Errorf("stating upload: %w", err)
	}

	err = os.Rename(tempPath, path)
	if err != nil {
		return "", fmt.Errorf("publishing upload: %w", err)
	}

	return digest, nil
}

// classifyUploadError wraps a read failure, marking it errBadUpload when the bytes
// were not valid gzip. A body-size error passes through, so it keeps its 413.
func classifyUploadError(action string, err error) error {
	var tooLarge *http.MaxBytesError

	if !errors.As(err, &tooLarge) && isGzipFault(err) {
		return fmt.Errorf("%w: %s: %w", errBadUpload, action, err)
	}

	return fmt.Errorf("%s: %w", action, err)
}

// uploadPath returns where the upload with the given ID is cached.
func uploadPath(dir, digest string) string {
	return filepath.Join(dir, uploadPrefix+digest+uploadSuffix)
}

// refreshUpload sets an upload's modification time to now, because the cache sweep
// expires copies by idle time.
func refreshUpload(path string) error {
	now := time.Now()

	err := os.Chtimes(path, now, now)
	if err != nil {
		return fmt.Errorf("refreshing upload: %w", err)
	}

	return nil
}

// resolveRequestSource returns a file:// URL for the replay or upload a request
// names, with video metadata when the replay is a video. An upload carries none, as
// the web UI holds the video itself. Exactly one of the replay and upload query
// parameters must be present. On failure it returns the HTTP status to answer with.
//
// Callers hold the heavy-job slot, so the sweep cannot remove an upload between the
// existence check here and the read.
func (s *Service) resolveRequestSource(request *http.Request) (source string, video *videoMeta, status int, err error) {
	query := request.URL.Query()
	replay, upload := query.Get("replay"), query.Get("upload")

	if (replay == "") == (upload == "") {
		return "", nil, http.StatusBadRequest, errors.New("exactly one of replay and upload is required")
	}

	if upload != "" {
		return s.resolveUpload(upload)
	}

	dir := s.replayDir()

	status, valid := validateReplayName(replay, s.listReplays(dir))
	if !valid {
		return "", nil, status, errors.New("invalid or unknown replay filename")
	}

	source, video, err = s.resolveSource(dir, replay)
	if err != nil {
		return "", nil, http.StatusInternalServerError, err
	}

	return source, video, http.StatusOK, nil
}

// resolveUpload returns a file:// URL for a stored upload and refreshes its idle time.
func (s *Service) resolveUpload(uploadKey string) (source string, video *videoMeta, status int, err error) {
	if !uploadIDPattern.MatchString(uploadKey) {
		return "", nil, http.StatusBadRequest, errors.New("invalid upload id")
	}

	dir := s.cacheDir()
	if dir == "" {
		return "", nil, http.StatusNotFound, errUploadNotFound
	}

	path := uploadPath(dir, uploadKey)

	err = refreshUpload(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, http.StatusNotFound, errUploadNotFound
	}

	if err != nil {
		return "", nil, http.StatusInternalServerError, err
	}

	s.scheduleSweep(s.cacheTTL)

	return "file://" + filepath.ToSlash(path), nil, http.StatusOK, nil
}

// requestSourceName returns the replay name or upload ID a request names, for log
// fields.
func requestSourceName(request *http.Request) string {
	query := request.URL.Query()

	if upload := query.Get("upload"); upload != "" {
		return upload
	}

	return query.Get("replay")
}
