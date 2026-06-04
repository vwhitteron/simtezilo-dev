package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
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

	"github.com/vwhitteron/simtezilo-dev/app/signal"
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
	Frame   int     `json:"frame"`
	Jerk    float64 `json:"jerk"`
	Snap    float64 `json:"snap"`
	Surface string  `json:"surface"`
}

type mapCoord struct {
	X       float32 `json:"x"`
	Z       float32 `json:"z"`
	Surface string  `json:"surface"`
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
	source := flag.String("source", "", "Path to directory of replay files (.gtz/.gtr)")
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
	translationVelocities []gtmodels.Vector
	angularVelocities     []gtmodels.Vector
	surfaces              [][]string
	mapCoords             []mapCoord
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

	var longitudinalRadius float64

	var transverseRadius float64

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

			longitudinalRadius = wheelbaseMetres / 2
			transverseRadius = trackWidthMetres / 2

			vehicleCaptured = true
		}

		lastCoord = frame.PositionalMapCoordinates()

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

		acc.translationVelocities = append(acc.translationVelocities, frame.VelocityVector())
		acc.angularVelocities = append(acc.angularVelocities, frame.AngularVelocityVector())
		acc.surfaces = append(acc.surfaces, surfLabels)
		acc.mapCoords = append(acc.mapCoords, mapCoord{X: pos.X, Z: pos.Z, Surface: primarySurface})
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
		result[lap] = computeSnapJerk(acc.translationVelocities, acc.angularVelocities, longitudinalRadius, transverseRadius, acc.surfaces)
		mapResult[lap] = acc.mapCoords
	}

	return result, mapResult, meta, nil
}

// computeAccelMagnitudes computes per-frame acceleration magnitudes from a velocity series.
// Each element is the magnitude of the velocity delta scaled by framePeriod.
func computeAccelMagnitudes(velocities []gtmodels.Vector) []float64 {
	mags := make([]float64, len(velocities)-1)

	for idx := range len(velocities) - 1 {
		dx := float64(velocities[idx+1].X-velocities[idx].X) / framePeriod
		dy := float64(velocities[idx+1].Y-velocities[idx].Y) / framePeriod
		dz := float64(velocities[idx+1].Z-velocities[idx].Z) / framePeriod
		mags[idx] = math.Sqrt(dx*dx + dy*dy + dz*dz)
	}

	return mags
}

// differentiate returns the first derivative of a magnitude series, scaled by framePeriod.
func differentiate(mags []float64) []float64 {
	derivs := make([]float64, len(mags)-1)

	for idx := range len(mags) - 1 {
		derivs[idx] = (mags[idx+1] - mags[idx]) / framePeriod
	}

	return derivs
}

func computeSnapJerk(translationVelocities, angularVelocities []gtmodels.Vector, longitudinalRadius, transverseRadius float64, surfaces [][]string) []dataPoint {
	frameCount := len(translationVelocities)
	if frameCount < 4 {
		return nil
	}

	// Translation pipeline: matches kinematics SixDOFTranslationCalc.
	transJerks := differentiate(computeAccelMagnitudes(translationVelocities))
	transSnaps := differentiate(transJerks)

	// Rotation pipeline: matches kinematics SixDOFRotationCalc.
	// Angular velocity is scaled by vehicle dimensions (wheelbase/2 for X/Y, track/2 for Z)
	// before the same magnitude-based differentiation chain.
	scaledAngVel := make([]gtmodels.Vector, frameCount)

	for idx := range frameCount {
		scaledAngVel[idx] = gtmodels.Vector{
			X: angularVelocities[idx].X * float32(longitudinalRadius),
			Y: angularVelocities[idx].Y * float32(longitudinalRadius),
			Z: angularVelocities[idx].Z * float32(transverseRadius),
		}
	}

	rotJerks := differentiate(computeAccelMagnitudes(scaledAngVel))
	rotSnaps := differentiate(rotJerks)

	var points []dataPoint

	for idx := range transSnaps {
		surfIdx := idx + 3
		if surfIdx >= len(surfaces) {
			surfIdx = len(surfaces) - 1
		}

		// Match calculateChassisHapticPulseAmplitude: use the larger of translation and rotation
		jerkMag := signal.Abs(largestMagnitude(transJerks[idx+1], rotJerks[idx+1]))
		snapMag := signal.Abs(largestMagnitude(transSnaps[idx], rotSnaps[idx]))

		for _, label := range surfaces[surfIdx] {
			points = append(points, dataPoint{
				Frame:   idx + 3,
				Jerk:    jerkMag,
				Snap:    snapMag,
				Surface: label,
			})
		}
	}

	return points
}

// largestMagnitude returns whichever of a or b has the greater absolute value, preserving sign.
// This mirrors signal.LargestMagnitude used by calculateChassisHapticPulseAmplitude.
func largestMagnitude(a, b float64) float64 {
	if math.Abs(a) >= math.Abs(b) {
		return a
	}

	return b
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
