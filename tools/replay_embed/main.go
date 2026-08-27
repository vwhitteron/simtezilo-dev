// Command replay_embed muxes a GT7 telemetry replay into a video file as a
// timed-metadata track. One telemetry sample is written per video frame, so a
// consumer can index telemetry directly by frame number.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	errNoPackets    = errors.New("replay contains no valid telemetry packets")
	errNeedsConvert = errors.New("the command line path copies streams only")
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

	// Serve mode fields. serve holds the listen address when set.
	serve     string
	videoDir  string
	replayDir string
	outputDir string
}

func main() {
	opts := parseFlags()

	err := dispatch(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// dispatch runs either the web UI or a single command line embed.
func dispatch(opts options) error {
	if opts.serve != "" {
		return serve(opts)
	}

	return run(context.Background(), opts)
}

// parseFlags reads the command line into an options struct.
func parseFlags() options {
	var opts options

	flag.StringVar(&opts.video, "video", "", "Input video file. Convert WebM first, or use -serve")
	flag.StringVar(&opts.replay, "replay", "", "Input telemetry replay (.gtz or .gtr)")
	flag.StringVar(&opts.out, "out", "", "Output video file")
	flag.Float64Var(&opts.offset, "offset", 0,
		"Telemetry offset in seconds. Positive shifts telemetry later in the replay")
	flag.IntVar(&opts.offsetFrames, "offset-frames", 0,
		"Additional telemetry offset in whole frames, added to -offset")
	flag.StringVar(&opts.mapMode, "map", "sequential",
		"Frame mapping: sequential (packet N to frame N) or realtime (60 Hz by wall clock)")
	flag.StringVar(&opts.tmpDir, "tmpdir", "./tmp", "Directory for intermediate files")
	flag.BoolVar(&opts.keepTemp, "keep-temp", false, "Keep intermediate files")
	flag.BoolVar(&opts.verbose, "v", false, "Print ffmpeg command lines")
	flag.StringVar(&opts.serve, "serve", "",
		"Run the alignment web UI on this address, for example :8099")
	flag.StringVar(&opts.videoDir, "video-dir", "./input", "Directory scanned for source videos")
	flag.StringVar(&opts.replayDir, "replay-dir", "./input",
		"Directory scanned for telemetry replays")
	flag.StringVar(&opts.outputDir, "out-dir", "./output",
		"Default output directory for the web UI")

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

// ensureDir creates a directory and its parents when the directory is missing.
func ensureDir(dir string) error {
	if dir == "" {
		return nil
	}

	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	return nil
}

// run executes the full embed pipeline.
func run(ctx context.Context, opts options) error {
	err := validateOptions(&opts)
	if err != nil {
		return err
	}

	err = ensureDir(opts.tmpDir)
	if err != nil {
		return err
	}

	err = ensureDir(filepath.Dir(opts.out))
	if err != nil {
		return err
	}

	info, err := probeSource(ctx, opts.video)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "video: %d frames, %d/%d fps, %.3f s\n",
		info.frames, info.rateNum, info.rateDen, info.duration)

	// A stream copy cannot fix a crop, an HDR transfer or a non MP4 codec.
	if info.needsConvert() {
		return fmt.Errorf("%w: %s is %s in %s and needs conversion, so use -serve",
			errNeedsConvert, filepath.Base(opts.video), info.videoCodec, info.container)
	}

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

	err = mux(ctx, opts, opts.video, tmpFile)
	if err != nil {
		return err
	}

	return verify(ctx, opts.out, info)
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
