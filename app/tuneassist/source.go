package tuneassist

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/videotelemetry"
)

// videoMeta describes the video an analysis was read from, so the web UI can place
// a telemetry frame on the video's timeline.
//
// Time comes from the telemetry track's own timebase, never from a video frame
// number: an ffmpeg re-cut can leave the video and telemetry tracks with different
// sample counts, and only the telemetry track's timing describes the samples the
// analysis actually used.
type videoMeta struct {
	Name        string `json:"name"`
	FrameCount  int    `json:"frameCount"`
	Timescale   uint32 `json:"timescale"`
	SampleDelta uint32 `json:"sampleDelta"`
	FirstSeq    uint32 `json:"firstSeq"`
}

// resolveSource returns a file:// URL a gt-telemetry client can read for a replay
// listing entry, along with video metadata when the entry is a video.
//
// A video's embedded telemetry track is a concatenation of raw deciphered GT
// packets, which is exactly the .gtr format. Extracting it to a cached sidecar
// therefore lets every existing analysis path read a video without knowing that
// videos exist.
//
// A compressed .gtz replay resolves to an inflated .gtr copy in the cache, so each
// render skips the gzip inflate. The packets are identical, so the output is too.
func (s *Service) resolveSource(dir, filename string) (string, *videoMeta, error) {
	path := filepath.Join(dir, filename)

	if strings.HasSuffix(filename, ".gtz") {
		return s.inflatedSource(path, filename), nil, nil
	}

	if !isVideoName(filename) {
		return "file://" + filepath.ToSlash(path), nil, nil
	}

	index, err := videotelemetry.Open(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading telemetry track from %s: %w", filename, err)
	}

	defer index.Close()

	sidecar, err := s.sidecarPath(path, filename)
	if err != nil {
		return "", nil, err
	}

	err = s.useCached(sidecar, index.WriteGTR)
	if err != nil {
		return "", nil, err
	}

	meta := &videoMeta{
		Name:        filename,
		FrameCount:  index.FrameCount(),
		Timescale:   index.Timescale(),
		SampleDelta: index.SampleDelta(),
		FirstSeq:    index.FirstSequenceID(),
	}

	return "file://" + filepath.ToSlash(sidecar), meta, nil
}

// inflatedSource returns a file:// URL for an inflated copy of a .gtz replay. The
// cache is only an optimisation, so any failure falls back to the .gtz itself.
// A truncated .gtz fails to inflate and so is read as before, up to the truncation.
func (s *Service) inflatedSource(path, filename string) string {
	fallback := "file://" + filepath.ToSlash(path)

	cached, err := s.sidecarPath(path, filename)
	if errors.Is(err, errNoCacheDir) {
		return fallback
	}

	if err != nil {
		s.log.Warn().Err(err).Str("replay", filename).Msg("locating inflated replay cache")

		return fallback
	}

	err = s.useCached(cached, func(out io.Writer) error { return inflateReplay(path, out) })
	if err != nil {
		s.log.Warn().Err(err).Str("replay", filename).Msg("inflating replay into cache")

		return fallback
	}

	return "file://" + filepath.ToSlash(cached)
}

// maxInflatedReplay caps an inflated replay, so a malformed .gtz cannot fill the
// disk. A replay packet stream is a few MB per lap, so 1 GiB is hours of driving.
const maxInflatedReplay = 1 << 30

// errReplayTooLarge reports a .gtz that inflates past maxInflatedReplay.
var errReplayTooLarge = errors.New("inflated replay exceeds the cache size limit")

// inflateReplay writes the decompressed packet stream of the .gtz at path to out.
func inflateReplay(path string, out io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening replay: %w", err)
	}

	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("reading gzip header: %w", err)
	}

	defer reader.Close()

	written, err := io.Copy(out, io.LimitReader(reader, maxInflatedReplay+1))
	if err != nil {
		return fmt.Errorf("inflating replay: %w", err)
	}

	if written > maxInflatedReplay {
		return errReplayTooLarge
	}

	return nil
}

// sidecarPath returns where a replay's .gtr copy is cached: the telemetry extracted
// from a video, or a .gtz inflated. The size and modification time are part of the
// name so a changed source invalidates its own copy rather than silently reusing
// the previous one.
func (s *Service) sidecarPath(path, filename string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stating %s: %w", filename, err)
	}

	dir := s.cacheDir()
	if dir == "" {
		return "", errNoCacheDir
	}

	name := fmt.Sprintf("%s.%d-%d.gtr", filename, info.Size(), info.ModTime().UnixNano())

	return filepath.Join(dir, name), nil
}

// ensureCached writes a cached .gtr to path with write, unless it is already there.
// A copy that is already there has its modification time refreshed, because the
// cache sweep expires copies by idle time.
//
// The copy is written to a temporary file and renamed, so two requests racing on
// the same uncached source duplicate the work rather than reading a half-written
// copy.
func ensureCached(path string, write func(out io.Writer) error) error {
	_, err := os.Stat(path)
	if err == nil {
		now := time.Now()

		err = os.Chtimes(path, now, now)
		if err != nil {
			return fmt.Errorf("refreshing cached replay: %w", err)
		}

		return nil
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("stating telemetry sidecar: %w", err)
	}

	dir := filepath.Dir(path)

	err = os.MkdirAll(dir, 0o750)
	if err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating telemetry sidecar: %w", err)
	}

	defer os.Remove(temp.Name())

	err = write(temp)
	if err != nil {
		temp.Close()

		return fmt.Errorf("writing telemetry sidecar: %w", err)
	}

	err = temp.Close()
	if err != nil {
		return fmt.Errorf("closing telemetry sidecar: %w", err)
	}

	err = os.Rename(temp.Name(), path)
	if err != nil {
		return fmt.Errorf("publishing telemetry sidecar: %w", err)
	}

	return nil
}
