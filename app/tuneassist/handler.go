package tuneassist

import (
	"context"
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

	// heavy limits the tuning assistant to one data or audio job at a time, so the
	// web UI and the live haptic loop keep the other cores (target is a 4-core
	// Raspberry Pi Zero 2 W).
	heavy chan struct{}
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
		heavy:     make(chan struct{}, 1),
	}

	svc.tuningDefaults = buildTuningDefaults(opts.Log)

	return svc
}

// buildTuningDefaults marshals the shipped tuning defaults once at construction.
// A marshal failure is logged and results in an empty payload rather than a fatal
// exit, since this package always runs embedded in the long-lived app process.
func buildTuningDefaults(log zerolog.Logger) []byte {
	defaults := haptics.DefaultTuning()

	// Dereferenced here rather than marshalled as a pointer, so a nil would be a
	// build error instead of a silent null in the payload the sliders read.
	stepBlend := 0.0
	if defaults.TransmissionStepBlend != nil {
		stepBlend = *defaults.TransmissionStepBlend
	}

	defaultsJSON, err := json.Marshal(map[string]any{
		"jerkCompression": defaults.JerkCompression,
		"jerkCenter":      defaults.JerkCenter,
		"snapCompression": defaults.SnapCompression,
		"snapCenter":      defaults.SnapCenter,

		"transmissionJerkCompression": defaults.TransmissionJerkCompression,
		"transmissionStepBlend":       stepBlend,

		"surfaceRumble": defaults.SurfaceRumble,

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

	release, err := s.acquire(request.Context())
	if err != nil {
		s.log.Debug().Err(err).Str("replay", filename).Msg("acquiring heavy job slot")

		return
	}
	defer release()

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

	// json.Encoder.Encode still marshals replayData into its own internal buffer
	// before writing it out; it just spares us a second buffer of our own.
	response.Header().Set("Content-Type", "application/json")

	err = json.NewEncoder(response).Encode(replayData)
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("encoding replay analysis")
	}
}

// allLapsKey is the lap value that selects the whole replay instead of one lap.
const allLapsKey = "all"

// HandleAudio renders chassis audio for a lap section as a WAV. The render is done
// per request and only the requested section is held, so nothing accumulates across
// replay, lap, or tuning changes; the web UI caches the decoded buffer client-side.
// Query: replay, lap, from, to (per-lap frame indices; to<0 => whole lap), and the
// four tuning settings jerkCompression/jerkCenter/snapCompression/snapCenter, each a
// decimal setting value (0 => shipped default). A lap of "all" spans the whole replay,
// and from/to then index every frame of the replay in order. The bias each pair is
// anchored to is a fixed constant and is not a query parameter.
//
// It also takes the transmission pair transmissionJerkCompression/transmissionStepBlend
// (the blend is optional rather than sentinelled, since its whole range is legal),
// and the engine profile as primaryBalance/secondaryBalance/engineGain/pulseScale.
// The engine four are all-or-nothing: an absent parameter leaves the replay
// vehicle's stored profile in place. Road-texture surfaces (tarmac, concrete, grass,
// dirt, sand, snow) are each overridden by a <name>Level/<name>Coarseness pair; a
// surface is overridden only when both of its parameters are present.
//
// The layer parameter selects chassis (the default), texture, transmission or
// engine. A layer of "all" renders the four layers in one pass over the replay and
// returns one WAV with a channel per layer, in allLayerNames order.
func (s *Service) HandleAudio(response http.ResponseWriter, request *http.Request) {
	dir := s.replayDir()
	replays := s.listReplays(dir)

	filename := request.URL.Query().Get("replay")

	status, valid := validateReplayName(filename, replays)
	if !valid {
		http.Error(response, "invalid or unknown replay filename", status)

		return
	}

	window := haptics.CaptureWindow{
		Lap:       clampToInt16(parseIntParam(request, "lap", 0)),
		AllLaps:   request.URL.Query().Get("lap") == allLapsKey,
		FromFrame: parseIntParam(request, "from", 0),
		ToFrame:   parseIntParam(request, "to", -1),
	}

	tuning := haptics.Tuning{
		JerkCompression: parseFloatParam(request, "jerkCompression", 0),
		JerkCenter:      parseFloatParam(request, "jerkCenter", 0),
		SnapCompression: parseFloatParam(request, "snapCompression", 0),
		SnapCenter:      parseFloatParam(request, "snapCenter", 0),

		TransmissionJerkCompression: parseFloatParam(request, "transmissionJerkCompression", 0),
		TransmissionStepBlend:       optionalFloatParam(request, "transmissionStepBlend", 0, 1),

		SurfaceRumble: surfaceRumbleParams(request),

		EngineProfile: engineProfileParam(request),
	}

	unfiltered := request.URL.Query().Get("raw") == "1"

	layerName := request.URL.Query().Get("layer")

	layers, known := captureLayers(layerName)
	if !known && layerName != allLayersKey {
		http.Error(response, "unknown haptic layer", http.StatusBadRequest)

		return
	}

	release, err := s.acquire(request.Context())
	if err != nil {
		s.log.Debug().Err(err).Str("replay", filename).Msg("acquiring heavy job slot")

		return
	}
	defer release()

	source, _, err := s.resolveSource(dir, filename)
	if err != nil {
		s.log.Error().Err(err).Str("replay", filename).Msg("resolving replay source")
		http.Error(response, err.Error(), http.StatusInternalServerError)

		return
	}

	var wav []byte

	if layerName == allLayersKey {
		wav, err = renderAllLayersWAV(request.Context(), source, tuning, unfiltered, window)
	} else {
		wav, err = renderWindowWAV(request.Context(), source, tuning, layers, unfiltered, window)
	}

	if err != nil {
		if errors.Is(err, errNoAudio) {
			http.Error(response, "no audio for requested lap/section", http.StatusNotFound)

			return
		}

		s.log.Error().Err(err).Str("replay", filename).Msg("rendering haptic audio")
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

// acquire reserves the single heavy-job slot, blocking until it is free or ctx is
// done. On success the returned release func must be called to free the slot; on
// ctx expiry it returns a nil release func and ctx.Err().
func (s *Service) acquire(ctx context.Context) (release func(), err error) {
	select {
	case s.heavy <- struct{}{}:
		return func() { <-s.heavy }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
