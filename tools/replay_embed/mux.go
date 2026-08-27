package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
)

var errVerifyFailed = errors.New("output verification failed")

// mux copies the video and audio streams and adds the telemetry track.
// videoFile is the cut and converted source, which may differ from opts.video.
func mux(ctx context.Context, opts options, videoFile string, telemetryFile string) error {
	args := []string{"-y", "-i", videoFile, "-i", telemetryFile, "-map", "0:v:0"}

	// Map audio only when present, and drop any data track from the source.
	if opts.hasAudio {
		args = append(args, "-map", "0:a")
	}

	args = append(args,
		"-map", "1",
		"-c", "copy",
		"-copy_unknown",
		"-tag:d", sampleFormat,
		"-write_tmcd", "0",
		"-movflags", "+faststart",
		opts.out,
	)

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "ffmpeg %v\n", args)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("mux telemetry track: %w", err)
	}

	return nil
}

// verify confirms the output holds the expected telemetry data stream.
func verify(ctx context.Context, path string, info videoInfo) error {
	probe, err := runProbe(ctx, path,
		"stream=codec_type,codec_tag_string,nb_frames,duration,avg_frame_rate")
	if err != nil {
		return err
	}

	dataStreams := 0
	frames := 0
	duration := 0.0
	rate := ""

	for _, stream := range probe.Streams {
		// Match our own track by tag. ffmpeg may add a timecode track too.
		if stream.CodecType != "data" || stream.CodecTag != sampleFormat {
			continue
		}

		dataStreams++
		frames, _ = strconv.Atoi(stream.NbFrames)
		duration, _ = strconv.ParseFloat(stream.Duration, 64)
		rate = stream.AvgFrameRate
	}

	if dataStreams != 1 {
		return fmt.Errorf("%w: found %d data streams, want 1", errVerifyFailed, dataStreams)
	}

	if frames != info.frames {
		return fmt.Errorf("%w: data track has %d samples, want %d",
			errVerifyFailed, frames, info.frames)
	}

	frameSec := float64(info.rateDen) / float64(info.rateNum)
	if math.Abs(duration-info.duration) > frameSec {
		return fmt.Errorf("%w: data track is %.3f s, video is %.3f s",
			errVerifyFailed, duration, info.duration)
	}

	fmt.Fprintf(os.Stderr, "wrote %s: %d telemetry samples, %.3f s, %s fps\n",
		path, frames, duration, rate)

	return nil
}
