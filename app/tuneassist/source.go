package tuneassist

import (
	"fmt"
	"os"
	"path/filepath"

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
func (s *Service) resolveSource(dir, filename string) (string, *videoMeta, error) {
	path := filepath.Join(dir, filename)

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

	err = ensureSidecar(sidecar, index)
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

// sidecarPath returns where a video's extracted telemetry is cached. The size and
// modification time are part of the name so a re-embedded video invalidates its own
// sidecar rather than silently reusing the previous extraction.
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

// ensureSidecar extracts the telemetry track to path unless it is already there.
//
// The extraction is written to a temporary file and renamed, so two requests racing
// on the same uncached video duplicate the work rather than reading a half-written
// sidecar.
func ensureSidecar(path string, index *videotelemetry.Index) error {
	_, err := os.Stat(path)
	if err == nil {
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

	err = index.WriteGTR(temp)
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
