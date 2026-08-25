// Command replay_embed muxes a GT7 telemetry replay into a video file as a
// timed-metadata track. One telemetry sample is written per video frame, so a
// consumer can index telemetry directly by frame number.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// minPacketSize and maxPacketSize bound a credible telemetry packet length.
// The exact length varies by GT7 packet format and is detected per file.
const (
	minPacketSize = 128
	maxPacketSize = 4096
)

// telemetryRate is the GT7 telemetry packet rate in hertz.
const telemetryRate = 60

// sequenceIDOffset is the byte offset of the uint32 sequence ID in a packet.
const sequenceIDOffset = 112

// sampleFormat is the four character code used for the metadata sample entry.
// ffmpeg maps only a known metadata tag onto its bin_data codec. The GoPro
// tag is therefore reused so standard tools can copy the track.
const sampleFormat = "gpmd"

// packetMagic is the header that starts every deciphered telemetry packet.
const packetMagic = "0S7G"

var (
	errNoPackets   = errors.New("replay contains no valid telemetry packets")
	errNoVideo     = errors.New("input has no video stream")
	errBadFrameFmt = errors.New("unrecognised frame rate format")
)

// options holds the parsed command line configuration.
type options struct {
	video        string
	replay       string
	out          string
	offset       float64
	offsetFrames int
	mapMode      string
	tmpDir       string
	keepTemp     bool
	verbose      bool
	hasAudio     bool
}

func main() {
	opts := parseFlags()

	err := run(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// parseFlags reads the command line into an options struct.
func parseFlags() options {
	var opts options

	flag.StringVar(&opts.video, "video", "", "Input video file (mp4 or mov)")
	flag.StringVar(&opts.replay, "replay", "", "Input telemetry replay (.gtz or .gtr)")
	flag.StringVar(&opts.out, "out", "", "Output video file")
	flag.Float64Var(&opts.offset, "offset", 0,
		"Telemetry offset in seconds. Positive shifts telemetry later in the replay")
	flag.IntVar(&opts.offsetFrames, "offset-frames", 0,
		"Additional telemetry offset in whole frames, added to -offset")
	flag.StringVar(&opts.mapMode, "map", "sequential",
		"Frame mapping: sequential (packet N to frame N) or realtime (60 Hz by wall clock)")
	flag.StringVar(&opts.tmpDir, "tmpdir", os.TempDir(), "Directory for intermediate files")
	flag.BoolVar(&opts.keepTemp, "keep-temp", false, "Keep intermediate files")
	flag.BoolVar(&opts.verbose, "v", false, "Print ffmpeg command lines")

	flag.Usage = usage

	flag.Parse()

	return opts
}

// usage prints the command help text.
func usage() {
	out := flag.CommandLine.Output()

	fmt.Fprintf(out, `replay_embed embeds a GT7 telemetry replay into a video as a metadata track.

The video and audio streams are copied without re-encoding. A new data track is
added with exactly one 344 byte telemetry packet per video frame, sharing the
video timebase. Sample index therefore equals video frame index.

There is no shared timecode between a screen recording and a replay capture.
Use -offset to align them by hand.

Mapping modes:
  sequential  Telemetry packet N maps to video frame N. Use when the recorder
              captured every game frame, whatever rate the container declares.
  realtime    Telemetry is treated as exactly 60 Hz and indexed by wall clock
              time. Use when the recorder truly sampled at its declared rate.

Usage:
`)
	flag.PrintDefaults()
}

// run executes the full embed pipeline.
func run(opts options) error {
	err := validateOptions(&opts)
	if err != nil {
		return err
	}

	info, err := probeVideo(opts.video)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "video: %d frames, %d/%d fps, %.3f s\n",
		info.frames, info.rateNum, info.rateDen, info.duration)

	packets, err := readReplay(opts.replay)
	if err != nil {
		return err
	}

	samples, stats := buildSamples(packets, info, opts)

	stats.report(info.frames)

	opts.hasAudio = info.hasAudio

	tmpFile := filepath.Join(opts.tmpDir, "replay_embed_telemetry.mp4")

	err = writeTelemetryMP4(tmpFile, samples, info)
	if err != nil {
		return err
	}

	if !opts.keepTemp {
		defer os.Remove(tmpFile)
	}

	err = mux(opts, tmpFile)
	if err != nil {
		return err
	}

	return verify(opts.out, info)
}

// validateOptions checks required flags and applies defaults.
func validateOptions(opts *options) error {
	if opts.video == "" || opts.replay == "" || opts.out == "" {
		flag.Usage()

		return errors.New("-video, -replay and -out are required")
	}

	if opts.mapMode != "sequential" && opts.mapMode != "realtime" {
		return fmt.Errorf("unknown -map mode %q", opts.mapMode)
	}

	return nil
}

// videoInfo describes the timing of the input video stream.
type videoInfo struct {
	rateNum  int64
	rateDen  int64
	frames   int
	duration float64
	hasAudio bool
}

// ffprobeOutput mirrors the subset of ffprobe JSON that is needed.
// Field names are fixed by ffprobe.
//
//nolint:tagliatelle // ffprobe emits snake case keys
type ffprobeOutput struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecTag    string `json:"codec_tag_string"`
		RFrameRate  string `json:"r_frame_rate"`
		NbFrames    string `json:"nb_frames"`
		Duration    string `json:"duration"`
		AvgFrameRat string `json:"avg_frame_rate"`
	} `json:"streams"`
}

// probeVideo reads frame rate, frame count and duration from the input video.
func probeVideo(path string) (videoInfo, error) {
	var info videoInfo

	probe, err := runProbe(path, "stream=codec_type,r_frame_rate,nb_frames,duration")
	if err != nil {
		return info, err
	}

	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			info.hasAudio = true
		}

		if stream.CodecType != "video" || info.rateNum != 0 {
			continue
		}

		info.rateNum, info.rateDen, err = parseRational(stream.RFrameRate)
		if err != nil {
			return info, err
		}

		info.duration, _ = strconv.ParseFloat(stream.Duration, 64)
		info.frames, _ = strconv.Atoi(stream.NbFrames)
	}

	if info.rateNum == 0 {
		return info, errNoVideo
	}

	if info.frames == 0 {
		rate := float64(info.rateNum) / float64(info.rateDen)
		info.frames = int(math.Round(info.duration * rate))
	}

	return info, nil
}

// runProbe executes ffprobe and decodes its JSON output.
func runProbe(path string, entries string) (ffprobeOutput, error) {
	var probe ffprobeOutput

	cmd := exec.CommandContext(context.Background(), "ffprobe", "-v", "error",
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
