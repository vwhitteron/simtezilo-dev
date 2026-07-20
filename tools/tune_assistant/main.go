package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
	gtmodels "github.com/zetetos/gt-telemetry/v2/pkg/models"
)

const (
	sampleRate  = 60.0
	framePeriod = 1.0 / sampleRate
)

//go:embed index.html
var indexHTML embed.FS

// classifySurfaces returns the set of surface labels for a sample.
// Tarmac requires all four wheels on tarmac.
// Concrete and grass are reported when any wheel is on that surface.
func classifySurfaces(s gtmodels.CornerSetGeneric[gtmodels.SurfaceType]) []string {
	wheels := [4]gtmodels.SurfaceType{s.FrontLeft, s.FrontRight, s.RearLeft, s.RearRight}

	allTarmac := true
	hasConcrete := false
	hasGrass := false

	for _, wheelType := range wheels {
		if wheelType != gtmodels.SurfaceTypeTarmac {
			allTarmac = false
		}

		if wheelType == gtmodels.SurfaceTypeConcrete {
			hasConcrete = true
		}

		if wheelType == gtmodels.SurfaceTypeGrass {
			hasGrass = true
		}
	}

	var labels []string

	tarmac := gtmodels.SurfaceTypeTarmac
	concrete := gtmodels.SurfaceTypeConcrete
	grass := gtmodels.SurfaceTypeGrass

	if allTarmac {
		labels = append(labels, tarmac.String())
	}

	if hasConcrete {
		labels = append(labels, concrete.String())
	}

	if hasGrass {
		labels = append(labels, grass.String())
	}

	return labels
}

type dataPoint struct {
	Frame int `json:"frame"`
	// Jerk/Snap are the processed (live-path) values: the recovery-aware Resolved
	// derivatives, i.e. what the device actually receives. JerkRaw/SnapRaw are the
	// ungated calculated chain with no nyquist gate or gap resolution — the honest
	// underlying signal. The web UI toggles which pair the scatter plots.
	Jerk    float64 `json:"jerk"`
	Snap    float64 `json:"snap"`
	JerkRaw float64 `json:"jerkRaw"`
	SnapRaw float64 `json:"snapRaw"`
	Surface string  `json:"surface"`
}

// derivTracker computes the ungated ("raw") calculated jerk/snap chain for a single
// velocity signal, mirroring the kinematics SixDOF*Calc math but without the nyquist
// gate or gap resolution the live path applies. It is tool-only — the app never
// constructs one — so it adds nothing to the real-time haptic loop.
type derivTracker struct {
	lastVel      gtmodels.Vector
	lastAccelMag float64
	lastJerk     float64
	samples      int
}

// update advances the chain by one frame and returns the current raw jerk and snap.
// windowSeconds is the frame period. The returned values read zero until enough
// samples have accumulated (jerk needs 3, snap needs 4) so the second derivative is
// real rather than an artefact of differencing against the zero-initialised history;
// the internal chain always uses the true values.
func (d *derivTracker) update(vel gtmodels.Vector, windowSeconds float64) (jerk, snap float64) {
	accelFactor := float32(1.0 / windowSeconds)
	accel := vector.Scale(vector.Delta(vel, d.lastVel), accelFactor, accelFactor, accelFactor)
	accelMag := vector.Magnitude(accel)

	jerk = (accelMag - d.lastAccelMag) / windowSeconds
	snap = (jerk - d.lastJerk) / windowSeconds

	d.lastVel = vel
	d.lastAccelMag = accelMag
	d.lastJerk = jerk
	d.samples++

	if d.samples < 3 {
		jerk = 0
	}

	if d.samples < 4 {
		snap = 0
	}

	return jerk, snap
}

type mapCoord struct {
	X        float32 `json:"x"`
	Z        float32 `json:"z"`
	Surface  string  `json:"surface"`
	Speed    float32 `json:"speed"`    // ground speed in m/s from telemetry
	Throttle float32 `json:"throttle"` // throttle input, percent 0-100
	Brake    float32 `json:"brake"`    // brake input, percent 0-100
}

type metadata struct {
	CircuitName       string `json:"circuitName"`
	CircuitVariation  string `json:"circuitVariation"`
	VehicleYear       int    `json:"vehicleYear"`
	VehicleMake       string `json:"vehicleMake"`
	VehicleModel      string `json:"vehicleModel"`
	VehicleCategory   string `json:"vehicleCategory"`
	VehicleDrivetrain string `json:"vehicleDrivetrain"`
}

func main() {
	source := flag.String("source", "./data/replays", "Path to directory of replay files (default: ./data/replays)")
	addr := flag.String("addr", ":0", "HTTP listen address (default: random port)")
	noBrowser := flag.Bool("no-browser", false, "Do not open the browser automatically")

	flag.Parse()

	if *source == "" {
		fmt.Fprintln(os.Stderr, "Error: -source flag is required")
		flag.Usage()
		os.Exit(1)
	}

	absDir, err := filepath.Abs(*source)
	if err != nil {
		log.Fatalf("Invalid source directory: %v", err)
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		log.Fatalf("Failed to read directory %s: %v", absDir, err)
	}

	var replays []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if strings.HasSuffix(name, ".gtz") || strings.HasSuffix(name, ".gtr") {
			replays = append(replays, name)
		}
	}

	slices.Sort(replays)

	if len(replays) == 0 {
		log.Fatalf("No .gtz or .gtr files found in %s", absDir)
	}

	log.Printf("Found %d replay(s) in %s", len(replays), absDir)

	serveChart(absDir, replays, *addr, *noBrowser)
}

type lapAccumulator struct {
	points    []dataPoint
	mapCoords []mapCoord
}

func collectAllLaps(source string) (map[int16][]dataPoint, map[int16][]mapCoord, metadata, error) {
	client, err := gttelemetry.New(gttelemetry.Options{
		Source: source,
		Format: gtmodels.Addendum3,
	})
	if err != nil {
		return nil, nil, metadata{}, fmt.Errorf("creating telemetry client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	accumulators := make(map[int16]*lapAccumulator)

	var lastCoord gtmodels.Coordinate

	vehicleCaptured := false
	meta := metadata{}

	var dims vehicle.Dimensions

	// Drive the real app kinematics pipeline. State is stateful and sequential, so
	// a single instance is advanced frame-by-frame across the whole replay (never
	// reset at lap boundaries, matching the live app). This gives us the fs/2
	// nyquist gate and the gap-aware Resolved* derivatives for free rather than
	// reproducing the accel->jerk->snap chain here.
	state := kinematics.NewKinematicsState()

	// Raw (ungated) reference chains, advanced in lockstep with state so both series
	// share the same frame indices. Translation and rotation are tracked separately,
	// exactly as the processed path splits them.
	var rawTrans, rawRot derivTracker

	for frame, frameErr := range client.Scan(ctx) {
		if frameErr != nil {
			return nil, nil, metadata{}, fmt.Errorf("reading frame: %w", frameErr)
		}

		if !frame.TelemetryStarted() {
			continue
		}

		if !vehicleCaptured {
			meta.VehicleYear = frame.VehicleYear()
			meta.VehicleMake = frame.VehicleManufacturer()
			meta.VehicleModel = frame.VehicleModel()
			meta.VehicleCategory = frame.VehicleCategory()
			meta.VehicleDrivetrain = frame.VehicleDrivetrain()

			wheelbaseMillimetres := frame.VehicleWheelbaseMillimetres()

			var wheelbaseMetres float64

			if wheelbaseMillimetres > 0 {
				wheelbaseMetres = float64(wheelbaseMillimetres) / 1000
			} else {
				wheelbaseMetres = (float64(frame.VehicleLengthMillimetres()) / 1000) * 0.55
			}

			trackFrontMetres := float64(frame.VehicleTrackFrontMillimetres()) / 1000
			trackRearMetres := float64(frame.VehicleTrackRearMillimetres()) / 1000

			var trackWidthMetres float64

			if trackFrontMetres > 0 || trackRearMetres > 0 {
				trackWidthMetres = (trackFrontMetres + trackRearMetres) / 2
			} else {
				trackWidthMetres = (float64(frame.VehicleWidthMillimetres()) / 1000) * 0.85
			}

			dims = vehicle.Dimensions{
				WheelbaseMetres:    float32(wheelbaseMetres),
				TrackWidthMetres:   float32(trackWidthMetres),
				LongitudinalRadius: float32(wheelbaseMetres / 2),
				TransverseRadius:   float32(trackWidthMetres / 2),
			}

			vehicleCaptured = true
		}

		lastCoord = frame.PositionalMapCoordinates()

		// Advance the kinematics state for this frame. Scan yields client.Telemetry
		// (the same *Transformer Update reads), so the client is already positioned
		// on the current frame.
		state.Update(framePeriod, dims, client)

		lap := frame.CurrentLap()

		if _, exists := accumulators[lap]; !exists {
			accumulators[lap] = &lapAccumulator{}
		}

		acc := accumulators[lap]

		surf := frame.SurfaceType()
		pos := frame.PositionalMapCoordinates()

		surfLabels := classifySurfaces(surf)
		primarySurface := "tarmac"

		if len(surfLabels) > 0 {
			primarySurface = surfLabels[0]
		}

		// Frame index is the position within this lap's map-coords slice; the web UI
		// uses it to cross-reference scatter points against the map/speed tracks.
		frameIdx := len(acc.mapCoords)

		acc.mapCoords = append(acc.mapCoords, mapCoord{
			X: pos.X, Z: pos.Z, Surface: primarySurface,
			Speed:    frame.GroundSpeedMetresPerSecond(),
			Throttle: frame.ThrottleInputPercent(),
			Brake:    frame.BrakeInputPercent(),
		})

		// Processed: match calculateChassisHapticPulseAmplitude/Frequency — the larger
		// of the translation and rotation Resolved values (recovery-aware; zeroed
		// during warm-up and across telemetry gaps).
		jerkMag := signal.Abs(signal.LargestMagnitude(
			state.Current.ResolvedTransJerk, state.Current.ResolvedRotJerk))
		snapMag := signal.Abs(signal.LargestMagnitude(
			state.Current.ResolvedTransSnap, state.Current.ResolvedRotSnap))

		// Raw: the same larger-of-trans/rot rule over the ungated chains. Rotation is
		// scaled to metres at the wheels first, as the processed path does.
		rawTransJerk, rawTransSnap := rawTrans.update(frame.VelocityVector(), framePeriod)
		scaledAngVel := vector.Scale(
			frame.AngularVelocityVector(),
			dims.LongitudinalRadius, dims.LongitudinalRadius, dims.TransverseRadius,
		)
		rawRotJerk, rawRotSnap := rawRot.update(scaledAngVel, framePeriod)

		jerkRawMag := signal.Abs(signal.LargestMagnitude(rawTransJerk, rawRotJerk))
		snapRawMag := signal.Abs(signal.LargestMagnitude(rawTransSnap, rawRotSnap))

		for _, label := range surfLabels {
			acc.points = append(acc.points, dataPoint{
				Frame:   frameIdx,
				Jerk:    jerkMag,
				Snap:    snapMag,
				JerkRaw: jerkRawMag,
				SnapRaw: snapRawMag,
				Surface: label,
			})
		}
	}

	// Resolve circuit from the last known coordinate
	circuitID, found := client.CircuitDB.GetCircuitAtCoordinate(lastCoord, gtmodels.CoordinateTypeCircuit)
	if found {
		circuitInfo, infoFound := client.CircuitDB.GetCircuitByID(circuitID)
		if infoFound {
			meta.CircuitName = circuitInfo.Name
			meta.CircuitVariation = circuitInfo.Variation
		}
	}

	result := make(map[int16][]dataPoint, len(accumulators))
	mapResult := make(map[int16][]mapCoord, len(accumulators))

	for lap, acc := range accumulators {
		result[lap] = acc.points
		mapResult[lap] = acc.mapCoords
	}

	return result, mapResult, meta, nil
}

type lapResponse struct {
	Laps     []int16                `json:"laps"`
	Data     map[string][]dataPoint `json:"data"`
	Map      map[string][]mapCoord  `json:"map"`
	Metadata metadata               `json:"metadata"`
}

type replayCache struct {
	mu    sync.Mutex
	dir   string
	cache map[string][]byte
}

func (rc *replayCache) build(filename string) ([]byte, error) {
	rc.mu.Lock()

	if cached, ok := rc.cache[filename]; ok {
		rc.mu.Unlock()

		return cached, nil
	}

	rc.mu.Unlock()

	source := "file://" + filepath.ToSlash(filepath.Join(rc.dir, filename))

	lapPoints, lapMaps, meta, err := collectAllLaps(source)
	if err != nil {
		return nil, fmt.Errorf("loading replay: %w", err)
	}

	laps := make([]int16, 0, len(lapPoints))

	for lap := range lapPoints {
		laps = append(laps, lap)
	}

	slices.Sort(laps)

	data := make(map[string][]dataPoint, len(lapPoints))
	mapData := make(map[string][]mapCoord, len(lapMaps))

	for lap, pts := range lapPoints {
		data[strconv.Itoa(int(lap))] = pts
	}

	for lap, coords := range lapMaps {
		mapData[strconv.Itoa(int(lap))] = coords
	}

	resp := lapResponse{Laps: laps, Data: data, Map: mapData, Metadata: meta}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshalling response: %w", err)
	}

	rc.mu.Lock()

	rc.cache[filename] = respJSON

	rc.mu.Unlock()

	return respJSON, nil
}

func rootHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		htmlData, err := indexHTML.ReadFile("index.html")
		if err != nil {
			http.Error(writer, "internal error", http.StatusInternalServerError)

			return
		}

		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write(htmlData)
	})
}

func replaysHandler(replaysJSON []byte) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(replaysJSON)
	})
}

func dataHandler(replays []string, buildFn func(string) ([]byte, error)) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		filename := req.URL.Query().Get("replay")

		// Reject empty, path-traversal attempts, or filenames not in the known list.
		if filename == "" || filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\`) {
			http.Error(writer, "invalid replay filename", http.StatusBadRequest)

			return
		}

		if !slices.Contains(replays, filename) {
			http.Error(writer, "replay not found", http.StatusNotFound)

			return
		}

		replayData, err := buildFn(filename)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)

			return
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(replayData)
	})
}

func serveChart(dir string, replays []string, addr string, noBrowser bool) {
	replaysJSON, err := json.Marshal(map[string][]string{"replays": replays})
	if err != nil {
		log.Fatalf("Failed to marshal replays list: %v", err)
	}

	lapCache := &replayCache{dir: dir, cache: make(map[string][]byte)}

	mux := http.NewServeMux()
	mux.Handle("/", rootHandler())
	mux.Handle("/replays", replaysHandler(replaysJSON))
	mux.Handle("/data", dataHandler(replays, lapCache.build))

	ctx := context.Background()

	listener, listenErr := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if listenErr != nil {
		log.Fatalf("Failed to listen: %v", listenErr)
	}

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		log.Fatal("listener address is not a TCP address")
	}

	listenURL := fmt.Sprintf("http://localhost:%d", tcpAddr.Port)
	log.Printf("Serving scatter chart at %s", listenURL)

	if !noBrowser {
		openBrowser(listenURL)
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := server.Serve(listener)
	if serveErr != nil {
		log.Fatalf("Server error: %v", serveErr)
	}
}

func openBrowser(url string) {
	ctx := context.Background()

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "linux":
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}

	_ = cmd.Start()
}
