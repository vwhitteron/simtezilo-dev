package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// audioCache renders and caches whole-replay chassis captures keyed by replay file
// and tuning. A capture is large (a 20-minute replay is ~10 M float64 samples), so
// the cache is bounded and evicts the oldest entry past the limit.
type audioCache struct {
	mu    sync.Mutex
	dir   string
	max   int
	cache map[string]*haptics.Capture
	order []string
}

func newAudioCache(dir string) *audioCache {
	return &audioCache{dir: dir, max: 6, cache: make(map[string]*haptics.Capture)}
}

func tuningKey(filename string, t haptics.Tuning, unfiltered bool) string {
	return fmt.Sprintf("%s|jc=%d|jm=%d|sc=%d|sm=%d|raw=%t", filename, t.JerkCurve, t.JerkMax, t.SnapCurve, t.SnapMax, unfiltered)
}

// get returns the capture for a replay+tuning, rendering it on first use. When
// unfiltered is set the render bypasses the kinematics fs/2 nyquist gate so the
// waveform reflects the raw (ungated) signal, matching the chart's raw series.
func (ac *audioCache) get(filename string, t haptics.Tuning, unfiltered bool) (*haptics.Capture, error) {
	key := tuningKey(filename, t, unfiltered)

	ac.mu.Lock()

	if cached, ok := ac.cache[key]; ok {
		ac.mu.Unlock()

		return cached, nil
	}

	ac.mu.Unlock()

	source := "file://" + filepath.ToSlash(filepath.Join(ac.dir, filename))

	capture, err := haptics.CaptureChassis(haptics.CaptureOptions{Source: source, Tuning: t, Unfiltered: unfiltered})
	if err != nil {
		return nil, fmt.Errorf("rendering chassis audio: %w", err)
	}

	ac.mu.Lock()

	if _, ok := ac.cache[key]; !ok {
		ac.cache[key] = capture
		ac.order = append(ac.order, key)

		for len(ac.order) > ac.max {
			oldest := ac.order[0]
			ac.order = ac.order[1:]
			delete(ac.cache, oldest)
		}
	}

	ac.mu.Unlock()

	return capture, nil
}

// sliceForSection returns the [start, end) sample range covering the frames of lap
// whose per-lap FrameIndex falls in [fromFrame, toFrame]. When toFrame < 0 the whole
// lap is used. It returns ok=false when the lap/range yields no frames.
func sliceForSection(capture *haptics.Capture, lap int16, fromFrame, toFrame int) (start, end int, ok bool) {
	start = -1
	end = len(capture.Samples)

	for i := range capture.Frames {
		f := &capture.Frames[i]
		if f.Lap != lap {
			continue
		}

		if f.FrameIndex < fromFrame {
			continue
		}

		if toFrame >= 0 && f.FrameIndex > toFrame {
			// First frame past the window: its cursor is the exclusive end.
			end = f.OutCursor

			break
		}

		if start < 0 {
			start = f.OutCursor
		}
	}

	if start < 0 {
		return 0, 0, false
	}

	if end < start {
		end = len(capture.Samples)
	}

	return start, end, true
}

// encodeWAV writes a 16-bit PCM mono WAV of samples (float, nominally [-1, 1]) at
// sampleRate to a byte slice.
func encodeWAV(samples []float64, sampleRate int) []byte {
	const (
		bitsPerSample = 16
		numChannels   = 1
	)

	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataLen := len(samples) * blockAlign

	buf := make([]byte, 44+dataLen)

	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataLen))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(buf[22:24], numChannels)
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], bitsPerSample)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataLen))

	for i, s := range samples {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}

		v := int16(math.Round(s * math.MaxInt16))
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(v))
	}

	return buf
}

// parseIntParam returns the integer query value, or def when absent/blank/invalid.
func parseIntParam(req *http.Request, name string, def int) int {
	raw := req.URL.Query().Get(name)
	if raw == "" {
		return def
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}

	return v
}

// audioHandler renders (or serves cached) chassis audio for a lap section as a WAV.
// Query: replay, lap, from, to (per-lap frame indices; to<0 => whole lap), and the
// four tuning knobs jerkCurve/jerkMax/snapCurve/snapMax (0 => shipped default).
func audioHandler(replays []string, cache *audioCache) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		filename := req.URL.Query().Get("replay")

		if filename == "" || filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\`) {
			http.Error(writer, "invalid replay filename", http.StatusBadRequest)

			return
		}

		if !slices.Contains(replays, filename) {
			http.Error(writer, "replay not found", http.StatusNotFound)

			return
		}

		lap := int16(parseIntParam(req, "lap", 0))
		fromFrame := parseIntParam(req, "from", 0)
		toFrame := parseIntParam(req, "to", -1)

		tuning := haptics.Tuning{
			JerkCurve: parseIntParam(req, "jerkCurve", 0),
			JerkMax:   parseIntParam(req, "jerkMax", 0),
			SnapCurve: parseIntParam(req, "snapCurve", 0),
			SnapMax:   parseIntParam(req, "snapMax", 0),
		}

		unfiltered := req.URL.Query().Get("raw") == "1"

		capture, err := cache.get(filename, tuning, unfiltered)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)

			return
		}

		start, end, ok := sliceForSection(capture, lap, fromFrame, toFrame)
		if !ok {
			http.Error(writer, "no audio for requested lap/section", http.StatusNotFound)

			return
		}

		wav := encodeWAV(capture.Samples[start:end], capture.InternalRate)

		writer.Header().Set("Content-Type", "audio/wav")
		writer.Header().Set("Cache-Control", "no-store")
		_, _ = writer.Write(wav)
	})
}
