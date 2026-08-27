package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	errNoVideo      = errors.New("input has no video stream")
	errBadFrameFmt  = errors.New("unrecognised frame rate format")
	errNoFrameCount = errors.New("cannot determine the video frame count")
)

// videoInfo describes the timing and format of the input video stream.
type videoInfo struct {
	rateNum  int64
	rateDen  int64
	frames   int
	duration float64
	hasAudio bool

	// Format fields decide which conversion the export must perform.
	width         int
	height        int
	cropBottom    int
	pixFmt        string
	colorTransfer string
	colorSpace    string
	videoCodec    string
	audioCodec    string
	container     string
}

// displayHeight returns the height after the container crop is applied.
func (v videoInfo) displayHeight() int {
	return v.height - v.cropBottom
}

// isHDR reports whether the source carries a PQ transfer function.
func (v videoInfo) isHDR() bool {
	return v.colorTransfer == "smpte2084" || v.colorTransfer == "arib-std-b67"
}

// needsConvert reports whether the export must re-encode rather than copy.
func (v videoInfo) needsConvert() bool {
	if v.cropBottom != 0 || v.isHDR() {
		return true
	}

	if v.videoCodec != "h264" && v.videoCodec != "hevc" {
		return true
	}

	return !strings.Contains(v.container, "mp4") && !strings.Contains(v.container, "mov")
}

// probeSideData carries the container crop, which VP9 in Matroska uses.
//
//nolint:tagliatelle // ffprobe emits snake case keys
type probeSideData struct {
	SideDataType string `json:"side_data_type"`
	CropBottom   int    `json:"crop_bottom"`
}

// probeStream mirrors the subset of ffprobe stream JSON that is needed.
// Field names are fixed by ffprobe.
//
//nolint:tagliatelle // ffprobe emits snake case keys
type probeStream struct {
	Index         int             `json:"index"`
	CodecType     string          `json:"codec_type"`
	CodecName     string          `json:"codec_name"`
	CodecTag      string          `json:"codec_tag_string"`
	Width         int             `json:"width"`
	Height        int             `json:"height"`
	PixFmt        string          `json:"pix_fmt"`
	ColorSpace    string          `json:"color_space"`
	ColorTransfer string          `json:"color_transfer"`
	RFrameRate    string          `json:"r_frame_rate"`
	AvgFrameRate  string          `json:"avg_frame_rate"`
	NbFrames      string          `json:"nb_frames"`
	NbReadPackets string          `json:"nb_read_packets"`
	Duration      string          `json:"duration"`
	SideDataList  []probeSideData `json:"side_data_list"`
}

// probeFormat mirrors the ffprobe format block.
//
//nolint:tagliatelle // ffprobe emits snake case keys
type probeFormat struct {
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
}

// ffprobeOutput is the decoded ffprobe JSON document.
type ffprobeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

// standardRates lists the frame rates a capture device is likely to use.
//
//nolint:gochecknoglobals // a fixed lookup table with no state
var standardRates = [][2]int64{
	{120000, 1001},
	{120, 1},
	{100, 1},
	{60000, 1001},
	{60, 1},
	{50, 1},
	{30000, 1001},
	{30, 1},
	{25, 1},
	{24000, 1001},
	{24, 1},
}

// snapTolerance is the largest relative error that still counts as a match.
const snapTolerance = 0.001

// snapFrameRate rounds a derived rate onto the nearest standard rate.
// Matroska declares no rate, so ffprobe estimates one from millisecond
// timestamps. The estimate must be cleaned up before it becomes a timescale.
func snapFrameRate(num int64, den int64) (int64, int64) {
	rate := float64(num) / float64(den)

	best := -1
	bestErr := snapTolerance

	for i, std := range standardRates {
		want := float64(std[0]) / float64(std[1])

		relErr := math.Abs(rate-want) / want
		if relErr < bestErr {
			best = i
			bestErr = relErr
		}
	}

	if best < 0 {
		return num, den
	}

	return standardRates[best][0], standardRates[best][1]
}

// probeSource reads the timing and format of the input video.
func probeSource(ctx context.Context, path string) (videoInfo, error) {
	var info videoInfo

	probe, err := runFullProbe(ctx, path)
	if err != nil {
		return info, err
	}

	info.container = probe.Format.FormatName

	err = applyStreams(&info, probe.Streams)
	if err != nil {
		return info, err
	}

	if info.duration == 0 {
		info.duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	}

	err = resolveFrameCount(ctx, &info, path)
	if err != nil {
		return info, err
	}

	return info, nil
}

// applyStreams copies the first video and audio streams into the info struct.
func applyStreams(info *videoInfo, streams []probeStream) error {
	seen := false

	for _, stream := range streams {
		switch {
		case stream.CodecType == "audio" && !info.hasAudio:
			info.hasAudio = true
			info.audioCodec = stream.CodecName
		case stream.CodecType == "video" && !seen:
			seen = true

			err := applyVideoStream(info, stream)
			if err != nil {
				return err
			}
		}
	}

	if !seen {
		return errNoVideo
	}

	return nil
}

// applyVideoStream copies one ffprobe video stream into the info struct.
func applyVideoStream(info *videoInfo, stream probeStream) error {
	num, den, err := parseRational(stream.RFrameRate)
	if err != nil {
		return err
	}

	info.rateNum, info.rateDen = snapFrameRate(num, den)

	info.videoCodec = stream.CodecName
	info.width = stream.Width
	info.height = stream.Height
	info.pixFmt = stream.PixFmt
	info.colorSpace = stream.ColorSpace
	info.colorTransfer = stream.ColorTransfer

	for _, side := range stream.SideDataList {
		if side.SideDataType == "Frame Cropping" {
			info.cropBottom = side.CropBottom
		}
	}

	info.duration, _ = strconv.ParseFloat(stream.Duration, 64)
	info.frames, _ = strconv.Atoi(stream.NbFrames)

	return nil
}

// resolveFrameCount fills in a frame count the container did not declare.
func resolveFrameCount(ctx context.Context, info *videoInfo, path string) error {
	if info.frames > 0 {
		return nil
	}

	// Matroska stores no frame count, so the packets must be counted.
	frames, err := countPackets(ctx, path)
	if err != nil {
		return err
	}

	if frames == 0 {
		return fmt.Errorf("%w: %s declares none and holds no packets",
			errNoFrameCount, filepath.Base(path))
	}

	info.frames = frames

	if info.duration == 0 {
		info.duration = float64(frames) * float64(info.rateDen) / float64(info.rateNum)
	}

	return nil
}

// countPackets counts video packets without decoding them.
func countPackets(ctx context.Context, path string) (int, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-select_streams", "v:0", "-count_packets",
		"-show_entries", "stream=nb_read_packets", "-of", "csv=p=0", path)

	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("count packets in %s: %w", path, err)
	}

	// A single field csv row still carries a trailing separator.
	count, err := strconv.Atoi(strings.Trim(string(out), " ,\r\n\t"))
	if err != nil {
		return 0, fmt.Errorf("%w: unreadable packet count %q", errNoFrameCount, string(out))
	}

	return count, nil
}

// runFullProbe executes ffprobe and decodes the whole stream and format JSON.
func runFullProbe(ctx context.Context, path string) (ffprobeOutput, error) {
	var probe ffprobeOutput

	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_streams", "-show_format", "-of", "json", path)

	out, err := cmd.Output()
	if err != nil {
		return probe, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	err = json.Unmarshal(out, &probe)
	if err != nil {
		return probe, fmt.Errorf("decode ffprobe output: %w", err)
	}

	return probe, nil
}

// runProbe executes ffprobe for a named subset of entries.
func runProbe(ctx context.Context, path string, entries string) (ffprobeOutput, error) {
	var probe ffprobeOutput

	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", entries, "-of", "json", path)

	out, err := cmd.Output()
	if err != nil {
		return probe, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	err = json.Unmarshal(out, &probe)
	if err != nil {
		return probe, fmt.Errorf("decode ffprobe output: %w", err)
	}

	return probe, nil
}

// parseRational splits an ffprobe rational such as "60000/1001".
func parseRational(value string) (int64, int64, error) {
	num, den, found := strings.Cut(value, "/")
	if !found {
		return 0, 0, fmt.Errorf("%w: %q", errBadFrameFmt, value)
	}

	numerator, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %q", errBadFrameFmt, value)
	}

	denominator, err := strconv.ParseInt(den, 10, 64)
	if err != nil || denominator == 0 || numerator == 0 {
		return 0, 0, fmt.Errorf("%w: %q", errBadFrameFmt, value)
	}

	return numerator, denominator, nil
}

// keyframeBefore returns the time of the last keyframe at or before seconds.
// A stream copy can only start on a keyframe.
func keyframeBefore(ctx context.Context, path string, seconds float64) (float64, error) {
	// Read a window back from the cut so a long GOP is still covered.
	const window = 20.0

	from := math.Max(0, seconds-window)

	// The path is validated by the caller before it reaches this command.
	//nolint:gosec // arguments are built here, never taken from the request
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-read_intervals", fmt.Sprintf("%.3f%%+%.3f", from, seconds-from+0.001),
		"-skip_frame", "nokey",
		"-show_entries", "frame=pts_time", "-of", "csv=p=0", path)

	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("read keyframes from %s: %w", path, err)
	}

	best := 0.0
	found := false

	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		value, convErr := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(line), ","), 64)
		if convErr != nil {
			continue
		}

		if value <= seconds+1e-6 && (!found || value > best) {
			best = value
			found = true
		}
	}

	if !found {
		return 0, nil
	}

	return best, nil
}
