package tuneassist

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// Options holds constructor parameters for Service.
type Options struct {
	Log zerolog.Logger
	// ReplayDir returns the absolute path to the replay directory. It is evaluated
	// per request rather than captured once, since the app's base directory can
	// change at runtime.
	ReplayDir func() string
	// CacheDir returns the absolute path to the directory holding telemetry tracks
	// extracted from videos. Evaluated per request, for the same reason as ReplayDir.
	// Leaving it unset disables video sources rather than failing at construction.
	CacheDir func() string
}

// Service serves the tuning assistant's HTTP API.
type Service struct {
	log       zerolog.Logger
	replayDir func() string
	cacheDir  func() string

	tuningDefaults []byte
}

// New creates a Service ready to be wired into the web UI's HTTP mux.
func New(opts Options) *Service {
	cacheDir := opts.CacheDir
	if cacheDir == nil {
		cacheDir = func() string { return "" }
	}

	svc := &Service{
		log:       opts.Log,
		replayDir: opts.ReplayDir,
		cacheDir:  cacheDir,
	}

	svc.tuningDefaults = buildTuningDefaults(opts.Log)

	return svc
}

// buildTuningDefaults marshals the shipped tuning defaults once at construction.
// A marshal failure is logged and results in an empty payload rather than a fatal
// exit, since this package always runs embedded in the long-lived app process.
func buildTuningDefaults(log zerolog.Logger) []byte {
	defaults := haptics.DefaultTuning()

	// map[string]any rather than map[string]int: jerkPivotGain is a dB figure and
	// carries a fractional part.
	defaultsJSON, err := json.Marshal(map[string]any{
		"jerkCurve":     defaults.JerkCurve,
		"jerkPivot":     defaults.JerkPivot,
		"jerkPivotGain": defaults.JerkPivotGain,
		"snapCurve":     defaults.SnapCurve,
		"snapMax":       defaults.SnapMax,

		"pulseLimits": haptics.DefaultPulseLimits(),
	})
	if err != nil {
		log.Error().Err(err).Msg("marshalling tuning defaults")

		return []byte(`{}`)
	}

	return defaultsJSON
}

// errNoCacheDir reports a video source requested without a cache directory to
// extract its telemetry track into.
var errNoCacheDir = errors.New("no cache directory configured for video sources")

// validateReplayName rejects empty names, path traversal attempts, and names not
// present in the freshly-scanned replay listing.
func validateReplayName(filename string, replays []string) (status int, ok bool) {
	if filename == "" || filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\`) {
		return http.StatusBadRequest, false
	}

	if !slices.Contains(replays, filename) {
		return http.StatusNotFound, false
	}

	return 0, true
}

// HandleReplays lists the replay files currently available in the replay directory.
func (s *Service) HandleReplays(response http.ResponseWriter, _ *http.Request) {
	dir := s.replayDir()
	replays := s.listReplays(dir)

	if replays == nil {
		replays = []string{}
	}

	response.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(response).Encode(map[string][]string{"replays": replays})
	if err != nil {
		s.log.Error().Err(err).Msg("encoding replays list")
	}
}

// HandleData returns the per-lap jerk/snap/map analysis for a replay.
func (s *Service) HandleData(response http.ResponseWriter, request *http.Request) {
	dir := s.replayDir()
	replays := s.listReplays(dir)

	filename := request.URL.Query().Get("replay")

	status, valid := validateReplayName(filename, replays)
	if !valid {
		http.Error(response, "invalid or unknown replay filename", status)

		return
	}

	source, video, err := s.resolveSource(dir, filename)
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("resolving replay source")
		http.Error(response, err.Error(), http.StatusInternalServerError)

		return
	}

	replayData, err := buildLapResponse(request.Context(), source, video)
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("building replay analysis")
		http.Error(response, err.Error(), http.StatusInternalServerError)

		return
	}

	// Encoded straight to the client rather than marshalled into a buffer first, so
	// the payload is never held in full alongside the analysis it came from.
	response.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(response).Encode(replayData)
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("encoding replay analysis")
	}
}

// HandleAudio renders chassis audio for a lap section as a WAV. The render is done
// per request and only the requested section is held, so nothing accumulates across
// replay, lap, or tuning changes; the web UI caches the decoded buffer client-side.
// Query: replay, lap, from, to (per-lap frame indices; to<0 => whole lap), and the
// five tuning knobs jerkCurve/jerkPivot/jerkPivotGain/snapCurve/snapMax (0 =>
// shipped default, except jerkPivotGain which uses 1 as its not-supplied sentinel
// since 0 is a legal gain value).
func (s *Service) HandleAudio(response http.ResponseWriter, request *http.Request) {
	dir := s.replayDir()
	replays := s.listReplays(dir)

	filename := request.URL.Query().Get("replay")

	status, valid := validateReplayName(filename, replays)
	if !valid {
		http.Error(response, "invalid or unknown replay filename", status)

		return
	}

	lap := clampToInt16(parseIntParam(request, "lap", 0))
	fromFrame := parseIntParam(request, "from", 0)
	toFrame := parseIntParam(request, "to", -1)

	tuning := haptics.Tuning{
		JerkCurve: parseIntParam(request, "jerkCurve", 0),
		JerkPivot: parseIntParam(request, "jerkPivot", 0),
		// 0 dB is a legal gain value, so "not supplied" is signalled with 1,
		// which falls outside the valid -12..0 dB range and is rejected by
		// applyTuning's bounds check.
		JerkPivotGain: parseFloatParam(request, "jerkPivotGain", 1),
		SnapCurve:     parseIntParam(request, "snapCurve", 0),
		SnapMax:       parseIntParam(request, "snapMax", 0),
	}

	unfiltered := request.URL.Query().Get("raw") == "1"

	source, _, err := s.resolveSource(dir, filename)
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("resolving replay source")
		http.Error(response, err.Error(), http.StatusInternalServerError)

		return
	}

	wav, err := renderSectionWAV(request.Context(), source, tuning, unfiltered, lap, fromFrame, toFrame)
	if err != nil {
		if errors.Is(err, errNoAudio) {
			http.Error(response, "no audio for requested lap/section", http.StatusNotFound)

			return
		}

		s.log.Error().Err(err).Str("replay", filename).Msg("rendering chassis audio")
		http.Error(response, err.Error(), http.StatusInternalServerError)

		return
	}

	response.Header().Set("Content-Type", "audio/wav")
	response.Header().Set("Cache-Control", "no-store")
	_, _ = response.Write(wav)
}

// HandleTuningDefaults returns the shipped default tuning values, so the web UI can
// initialise its sliders where the live app starts.
func (s *Service) HandleTuningDefaults(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write(s.tuningDefaults)
}

// listReplays scans dir for replay sources, sorted by name. A missing or unreadable
// directory yields an empty (not error) list, since the assistant should simply show
// nothing rather than fail when no replays have been recorded yet.
//
// Videos are listed alongside plain recordings because a video carries its own
// telemetry track and so is a replay source in its own right, not an attachment to
// one. Everything downstream reaches its telemetry through resolveSource.
func (s *Service) listReplays(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.log.Debug().Err(err).Str("dir", dir).Msg("reading replay directory")

		return nil
	}

	var replays []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if strings.HasSuffix(name, ".gtz") || strings.HasSuffix(name, ".gtr") || isVideoName(name) {
			replays = append(replays, name)
		}
	}

	slices.Sort(replays)

	return replays
}
