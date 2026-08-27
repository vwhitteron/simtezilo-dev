package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed html/align.html
var pageFiles embed.FS

// staticFiles holds the css the page needs. The assets are copies of the app's
// own files, so the tool runs from any directory without the app beside it.
//
//go:embed static
var staticFiles embed.FS

var (
	errBadName = errors.New("invalid file name")
	errBadDir  = errors.New("invalid directory")
)

// videoExts lists the containers the alignment UI will open.
//
//nolint:gochecknoglobals // a fixed lookup table with no state
var videoExts = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
}

// replayExts lists the telemetry replay extensions.
//
//nolint:gochecknoglobals // a fixed lookup table with no state
var replayExts = map[string]bool{".gtz": true, ".gtr": true}

// server holds the alignment UI state.
type server struct {
	opts options
	job  *jobRunner

	mu       sync.Mutex
	probes   map[string]cachedProbe
	previews map[string]cachedPreview

	// The settings dialog rewrites these, so guard them separately.
	dirMu     sync.RWMutex
	videoDir  string
	replayDir string
	outputDir string
}

// dirs returns the directories the UI currently reads and writes.
func (s *server) dirs() (string, string, string) {
	s.dirMu.RLock()
	defer s.dirMu.RUnlock()

	return s.videoDir, s.replayDir, s.outputDir
}

// setDirs replaces the directories after they are checked.
func (s *server) setDirs(video string, replay string, output string) {
	s.dirMu.Lock()
	defer s.dirMu.Unlock()

	s.videoDir, s.replayDir, s.outputDir = video, replay, output
}

// cachedProbe remembers a probe result until the file changes.
type cachedProbe struct {
	stamp time.Time
	size  int64
	info  videoInfo
}

// cachedPreview remembers a decoded replay until the file changes.
type cachedPreview struct {
	stamp   time.Time
	size    int64
	preview telemetryPreview
}

// serve runs the alignment web UI until the process is stopped.
func serve(opts options) error {
	if opts.outputDir == "" {
		opts.outputDir = opts.replayDir
	}

	srv := &server{
		opts:      opts,
		job:       newJobRunner(),
		probes:    make(map[string]cachedProbe),
		previews:  make(map[string]cachedPreview),
		videoDir:  opts.videoDir,
		replayDir: opts.replayDir,
		outputDir: opts.outputDir,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", srv.handlePage)
	mux.HandleFunc("GET /css/{file}", srv.handleStatic)
	mux.HandleFunc("GET /js/{file}", srv.handleStatic)
	mux.HandleFunc("GET /api/sources", srv.handleSources)
	mux.HandleFunc("GET /api/probe", srv.handleProbe)
	mux.HandleFunc("GET /api/telemetry", srv.handleTelemetry)
	mux.HandleFunc("GET /api/video", srv.handleVideo)
	mux.HandleFunc("GET /api/settings", srv.handleGetSettings)
	mux.HandleFunc("POST /api/settings", srv.handleSetSettings)
	mux.HandleFunc("POST /api/export", srv.handleExport)
	mux.HandleFunc("GET /api/export/progress", srv.handleProgress)

	// Leave the write timeout unset. A large video body outlives any deadline.
	httpServer := &http.Server{
		Addr:              opts.serve,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Fprintf(os.Stderr, "alignment UI on http://localhost%s\n", opts.serve)
	fmt.Fprintf(os.Stderr, "videos: %s\nreplays: %s\noutput: %s\n",
		opts.videoDir, opts.replayDir, opts.outputDir)

	return httpServer.ListenAndServe()
}

// writeJSON sends a value as a JSON response body.
func writeJSON(response http.ResponseWriter, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")

	err := json.NewEncoder(response).Encode(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write response: %v\n", err)
	}
}

// writeError sends a JSON error body with a status code.
func writeError(response http.ResponseWriter, status int, err error) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)

	encodeErr := json.NewEncoder(response).Encode(map[string]string{"error": err.Error()})
	if encodeErr != nil {
		fmt.Fprintf(os.Stderr, "write error response: %v\n", encodeErr)
	}
}

// safeName rejects a name that could escape its directory.
func safeName(name string, allowed map[string]bool) (string, error) {
	if name == "" || name != filepath.Base(name) ||
		strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%w: %q", errBadName, name)
	}

	if !allowed[strings.ToLower(filepath.Ext(name))] {
		return "", fmt.Errorf("%w: unsupported extension %q", errBadName, filepath.Ext(name))
	}

	return name, nil
}

// listFiles returns the sorted names in a directory with a wanted extension.
func listFiles(dir string, allowed map[string]bool) []string {
	names := []string{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		if allowed[strings.ToLower(filepath.Ext(entry.Name()))] {
			names = append(names, entry.Name())
		}
	}

	sort.Strings(names)

	return names
}

// videoExtSet reports the video extensions as a plain membership set.
func videoExtSet() map[string]bool {
	set := make(map[string]bool, len(videoExts))
	for ext := range videoExts {
		set[ext] = true
	}

	return set
}

// handlePage serves the alignment page.
func (s *server) handlePage(response http.ResponseWriter, _ *http.Request) {
	body, err := pageFiles.ReadFile("html/align.html")
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)

		return
	}

	// Write the body directly. ServeContent adds range and conditional handling,
	// which lets a browser keep showing an edited page until a forced reload.
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	noStore(response)

	_, err = response.Write(body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "write page: %v\n", err)
	}
}

// noStore tells every cache layer to keep nothing. The page changes each edit.
func noStore(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	response.Header().Set("Pragma", "no-cache")
	response.Header().Set("Expires", "0")
}

// staticTypes lists the asset extensions the tool will serve.
//
//nolint:gochecknoglobals // a fixed lookup table with no state
var staticTypes = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
}

// handleStatic serves the embedded css and js so both UIs look the same.
func (s *server) handleStatic(response http.ResponseWriter, request *http.Request) {
	name := request.PathValue("file")

	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		writeError(response, http.StatusBadRequest, fmt.Errorf("%w: %q", errBadName, name))

		return
	}

	mime, ok := staticTypes[strings.ToLower(filepath.Ext(name))]
	if !ok {
		writeError(response, http.StatusBadRequest,
			fmt.Errorf("%w: unsupported asset %q", errBadName, name))

		return
	}

	kind := strings.Trim(path.Dir(request.URL.Path), "/")

	body, err := staticFiles.ReadFile(path.Join("static", kind, name))
	if err != nil {
		writeError(response, http.StatusNotFound,
			fmt.Errorf("%w: %s not found", errBadName, name))

		return
	}

	response.Header().Set("Content-Type", mime)
	response.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(response, request, name, time.Time{}, bytes.NewReader(body))
}

// handleSources lists the videos and replays the UI can open.
func (s *server) handleSources(response http.ResponseWriter, _ *http.Request) {
	videoDir, replayDir, outputDir := s.dirs()

	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		absOut = outputDir
	}

	writeJSON(response, map[string]any{
		"videos":    listFiles(videoDir, videoExtSet()),
		"replays":   listFiles(replayDir, replayExts),
		"outputDir": absOut,
	})
}

// probeCached returns a probe result, reusing it while the file is unchanged.
func (s *server) probeCached(ctx context.Context, path string) (videoInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return videoInfo{}, fmt.Errorf("stat video: %w", err)
	}

	s.mu.Lock()
	entry, ok := s.probes[path]
	s.mu.Unlock()

	if ok && entry.stamp.Equal(stat.ModTime()) && entry.size == stat.Size() {
		return entry.info, nil
	}

	info, err := probeSource(ctx, path)
	if err != nil {
		return info, err
	}

	s.mu.Lock()
	s.probes[path] = cachedProbe{stamp: stat.ModTime(), size: stat.Size(), info: info}
	s.mu.Unlock()

	return info, nil
}

// handleProbe reports the format and timing of one source video.
func (s *server) handleProbe(response http.ResponseWriter, request *http.Request) {
	name, err := safeName(request.URL.Query().Get("video"), videoExtSet())
	if err != nil {
		writeError(response, http.StatusBadRequest, err)

		return
	}

	info, err := s.probeCached(request.Context(), filepath.Join(s.videoRoot(), name))
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)

		return
	}

	writeJSON(response, map[string]any{
		"name":          name,
		"frames":        info.frames,
		"rateNum":       info.rateNum,
		"rateDen":       info.rateDen,
		"duration":      info.duration,
		"width":         info.width,
		"height":        info.displayHeight(),
		"cropBottom":    info.cropBottom,
		"pixFmt":        info.pixFmt,
		"colorTransfer": info.colorTransfer,
		"colorSpace":    info.colorSpace,
		"videoCodec":    info.videoCodec,
		"audioCodec":    info.audioCodec,
		"container":     info.container,
		"needsConvert":  info.needsConvert(),
		"canToneMap":    hasFilter(request.Context(), "zscale"),
		"mimeType":      videoExts[strings.ToLower(filepath.Ext(name))],
	})
}

// handleTelemetry decodes a replay into the per-frame preview arrays.
func (s *server) handleTelemetry(response http.ResponseWriter, request *http.Request) {
	name, err := safeName(request.URL.Query().Get("replay"), replayExts)
	if err != nil {
		writeError(response, http.StatusBadRequest, err)

		return
	}

	path := filepath.Join(s.replayRoot(), name)

	stat, err := os.Stat(path)
	if err != nil {
		writeError(response, http.StatusNotFound, err)

		return
	}

	s.mu.Lock()
	entry, ok := s.previews[path]
	s.mu.Unlock()

	if ok && entry.stamp.Equal(stat.ModTime()) && entry.size == stat.Size() {
		writeJSON(response, entry.preview)

		return
	}

	preview, err := buildPreview(request.Context(), path)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)

		return
	}

	s.mu.Lock()
	s.previews[path] = cachedPreview{stamp: stat.ModTime(), size: stat.Size(), preview: preview}
	s.mu.Unlock()

	writeJSON(response, preview)
}

// handleVideo serves a source video with range support.
func (s *server) handleVideo(response http.ResponseWriter, request *http.Request) {
	name, err := safeName(request.URL.Query().Get("name"), videoExtSet())
	if err != nil {
		writeError(response, http.StatusBadRequest, err)

		return
	}

	path := filepath.Join(s.videoRoot(), name)

	file, err := os.Open(path)
	if err != nil {
		writeError(response, http.StatusNotFound, err)

		return
	}

	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		writeError(response, http.StatusInternalServerError, err)

		return
	}

	response.Header().Set("Content-Type", videoExts[strings.ToLower(filepath.Ext(name))])

	http.ServeContent(response, request, name, stat.ModTime(), file)
}

// videoRoot returns the directory videos are read from.
func (s *server) videoRoot() string {
	video, _, _ := s.dirs()

	return video
}

// replayRoot returns the directory replays are read from.
func (s *server) replayRoot() string {
	_, replay, _ := s.dirs()

	return replay
}

// outputRoot returns the directory a relative output lands in.
func (s *server) outputRoot() string {
	_, _, output := s.dirs()

	return output
}

// settingsRequest carries the three directories the settings dialog edits.
type settingsRequest struct {
	VideoDir  string `json:"videoDir"`
	ReplayDir string `json:"replayDir"`
	OutputDir string `json:"outputDir"`
}

// handleGetSettings reports the directories in use.
func (s *server) handleGetSettings(response http.ResponseWriter, _ *http.Request) {
	video, replay, output := s.dirs()

	writeJSON(response, settingsRequest{
		VideoDir:  absOrSelf(video),
		ReplayDir: absOrSelf(replay),
		OutputDir: absOrSelf(output),
	})
}

// absOrSelf resolves a path, keeping the original when that fails.
func absOrSelf(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return abs
}

// handleSetSettings checks and applies new directories.
func (s *server) handleSetSettings(response http.ResponseWriter, request *http.Request) {
	var req settingsRequest

	err := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<16)).Decode(&req)
	if err != nil {
		writeError(response, http.StatusBadRequest, err)

		return
	}

	fields := []struct {
		label string
		value *string
	}{
		{"video directory", &req.VideoDir},
		{"replay directory", &req.ReplayDir},
		{"output directory", &req.OutputDir},
	}

	for _, field := range fields {
		err = checkDir(field.label, *field.value)
		if err != nil {
			writeError(response, http.StatusBadRequest, err)

			return
		}

		*field.value = absOrSelf(*field.value)
	}

	s.setDirs(req.VideoDir, req.ReplayDir, req.OutputDir)

	// A new directory may hold different files, so drop the caches.
	s.mu.Lock()
	s.probes = make(map[string]cachedProbe)
	s.previews = make(map[string]cachedPreview)
	s.mu.Unlock()

	writeJSON(response, req)
}

// checkDir rejects a path that is missing or is not a directory.
func checkDir(label string, path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: the %s is empty", errBadDir, label)
	}

	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: the %s %q cannot be read", errBadDir, label, path)
	}

	if !stat.IsDir() {
		return fmt.Errorf("%w: the %s %q is not a directory", errBadDir, label, path)
	}

	return nil
}

// handleExport starts one export job.
func (s *server) handleExport(response http.ResponseWriter, request *http.Request) {
	var req exportRequest

	err := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<16)).Decode(&req)
	if err != nil {
		writeError(response, http.StatusBadRequest, err)

		return
	}

	err = s.validateExport(&req)
	if err != nil {
		writeError(response, http.StatusBadRequest, err)

		return
	}

	if !s.job.start() {
		writeError(response, http.StatusConflict, errJobRunning)

		return
	}

	// Snapshot the directories so a settings change mid job cannot move them.
	jobOpts := s.opts
	jobOpts.videoDir, jobOpts.replayDir, jobOpts.outputDir = s.dirs()

	// The job outlives the response, so keep the values but drop the deadline.
	go runExport(context.WithoutCancel(request.Context()), jobOpts, req, s.job)

	writeJSON(response, map[string]string{"job": "1"})
}

// validateExport checks the submitted names and fills in the defaults.
func (s *server) validateExport(req *exportRequest) error {
	video, err := safeName(req.Video, videoExtSet())
	if err != nil {
		return err
	}

	replay, err := safeName(req.Replay, replayExts)
	if err != nil {
		return err
	}

	req.Video = video
	req.Replay = replay

	if req.MapMode != mapRealtime {
		req.MapMode = mapSequential
	}

	if req.Colour != colourHDR {
		req.Colour = colourSDR
	}

	if req.Output == "" {
		return fmt.Errorf("%w: no output path", errBadName)
	}

	if !filepath.IsAbs(req.Output) {
		req.Output = filepath.Join(s.outputRoot(), req.Output)
	}

	if strings.ToLower(filepath.Ext(req.Output)) != ".mp4" {
		return fmt.Errorf("%w: the output must be an .mp4 file", errBadName)
	}

	return nil
}

// handleProgress streams the running job's events to the page.
func (s *server) handleProgress(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, errJobRunning)

		return
	}

	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Connection", "keep-alive")
	response.WriteHeader(http.StatusOK)

	events, backlog := s.job.subscribe()

	sent := false

	for _, event := range backlog {
		writeEvent(response, event)

		sent = sent || event.Name == eventDone || event.Name == eventError
	}

	flusher.Flush()

	if events == nil {
		return
	}

	s.streamEvents(request, response, flusher, events, backlog, sent)
}

// streamEvents forwards live events until the job or the client stops.
func (s *server) streamEvents(request *http.Request, response http.ResponseWriter,
	flusher http.Flusher, events chan sseEvent, backlog []sseEvent, sent bool,
) {
	for {
		select {
		case <-request.Context().Done():
			s.job.unsubscribe(events)

			return
		case event, open := <-events:
			if !open {
				// A dropped terminal event would leave the page waiting.
				if !sent {
					s.replayTerminal(response, flusher, backlog)
				}

				return
			}

			writeEvent(response, event)
			flusher.Flush()

			sent = sent || event.Name == eventDone || event.Name == eventError
		}
	}
}

// replayTerminal sends the job's final event to a listener that missed it.
func (s *server) replayTerminal(response http.ResponseWriter, flusher http.Flusher,
	_ []sseEvent,
) {
	_, history := s.job.subscribe()

	for _, event := range history {
		if event.Name == eventDone || event.Name == eventError {
			writeEvent(response, event)
			flusher.Flush()

			return
		}
	}
}

// writeEvent formats one named server-sent event.
func writeEvent(response http.ResponseWriter, event sseEvent) {
	fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event.Name, event.Data)
}
