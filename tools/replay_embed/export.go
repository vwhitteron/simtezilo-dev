package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	errJobRunning  = errors.New("an export is already running")
	errBadCut      = errors.New("invalid cut range")
	errNoToneMap   = errors.New("this ffmpeg cannot tone map HDR")
	errConvertFail = errors.New("ffmpeg conversion failed")
)

// logLines is how many trailing stderr lines a failure reports.
const logLines = 50

// Event names and modes that appear in more than one place.
const (
	eventDone     = "done"
	eventError    = "error"
	eventProgress = "progress"
	colourHDR     = "hdr"
	colourSDR     = "sdr"
	mapSequential = "sequential"
	mapRealtime   = "realtime"
)

// exportRequest is the alignment state the page submits.
type exportRequest struct {
	Video        string `json:"video"`
	Replay       string `json:"replay"`
	OffsetFrames int    `json:"offsetFrames"`
	CutStart     int    `json:"cutStart"`
	CutEnd       int    `json:"cutEnd"`
	Output       string `json:"output"`
	MapMode      string `json:"mapMode"`
	Colour       string `json:"colour"`
}

// sseEvent is one named server-sent event.
type sseEvent struct {
	Name string
	Data []byte
}

// jobRunner allows one export at a time and broadcasts its progress.
type jobRunner struct {
	mu      sync.Mutex
	running bool
	subs    map[chan sseEvent]struct{}
	history []sseEvent
}

// newJobRunner returns an idle runner.
func newJobRunner() *jobRunner {
	return &jobRunner{subs: make(map[chan sseEvent]struct{})}
}

// start claims the runner. It reports false when a job is already running.
func (j *jobRunner) start() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.running {
		return false
	}

	j.running = true
	j.history = nil

	return true
}

// finish releases the runner and closes every listener.
func (j *jobRunner) finish() {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.running = false

	for ch := range j.subs {
		close(ch)
		delete(j.subs, ch)
	}
}

// emit broadcasts one event and keeps it for a listener that joins late.
func (j *jobRunner) emit(name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	event := sseEvent{Name: name, Data: data}

	j.mu.Lock()
	defer j.mu.Unlock()

	j.history = append(j.history, event)

	for ch := range j.subs {
		// Drop a progress tick rather than stall the encoder on a slow reader.
		select {
		case ch <- event:
		default:
		}
	}
}

// subscribe returns a live channel plus the events already emitted.
// The channel is nil when no job is running.
func (j *jobRunner) subscribe() (chan sseEvent, []sseEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()

	backlog := append([]sseEvent(nil), j.history...)

	if !j.running {
		return nil, backlog
	}

	ch := make(chan sseEvent, 64)
	j.subs[ch] = struct{}{}

	return ch, backlog
}

// unsubscribe removes a listener that disconnected early.
func (j *jobRunner) unsubscribe(events chan sseEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if _, ok := j.subs[events]; ok {
		delete(j.subs, events)
		close(events)
	}
}

// ringLog keeps the last few lines of a command's stderr.
type ringLog struct {
	mu    sync.Mutex
	lines []string
}

// add appends one line and discards the oldest when full.
func (r *ringLog) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines = append(r.lines, line)
	if len(r.lines) > logLines {
		r.lines = r.lines[len(r.lines)-logLines:]
	}
}

// snapshot returns a copy of the retained lines.
func (r *ringLog) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.lines...)
}

// filterCache remembers which optional ffmpeg filters this build provides.
//
//nolint:gochecknoglobals // one process wide probe of the installed ffmpeg
var (
	filterOnce  sync.Once
	filterNames map[string]bool
)

// hasFilter reports whether the installed ffmpeg provides a named filter.
func hasFilter(ctx context.Context, name string) bool {
	filterOnce.Do(func() {
		filterNames = make(map[string]bool)

		out, err := exec.CommandContext(ctx,
			"ffmpeg", "-hide_banner", "-filters").Output()
		if err != nil {
			return
		}

		for line := range strings.SplitSeq(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				filterNames[fields[1]] = true
			}
		}
	})

	return filterNames[name]
}

// frameSeconds converts a frame index to its presentation time.
func frameSeconds(frame int, info videoInfo) float64 {
	return float64(frame) * float64(info.rateDen) / float64(info.rateNum)
}

// secondsFrame converts a presentation time back to a frame index.
func secondsFrame(seconds float64, info videoInfo) int {
	return int(math.Round(seconds * float64(info.rateNum) / float64(info.rateDen)))
}

// buildFilterChain returns the video filter chain for the conversion.
func buildFilterChain(ctx context.Context, info videoInfo, colour string) (string, error) {
	var chain []string

	if info.cropBottom != 0 {
		chain = append(chain, fmt.Sprintf("crop=%d:%d:0:0", info.width, info.displayHeight()))
	}

	if colour == colourHDR || !info.isHDR() {
		if colour != colourHDR {
			chain = append(chain, "format=yuv420p")
		}

		return strings.Join(chain, ","), nil
	}

	// A PQ source must be linearised before it can be tone mapped, and only
	// zscale can do that. Report the fix rather than emit a grey picture.
	if !hasFilter(ctx, "zscale") {
		return "", fmt.Errorf(
			"%w: the zscale filter is missing, so %s cannot become BT.709. "+
				"Install an ffmpeg built with libzimg, or export with colour=hdr",
			errNoToneMap, info.colorTransfer)
	}

	chain = append(chain,
		"zscale=t=linear:npl=100",
		"tonemap=hable:desat=0",
		"zscale=p=bt709:t=bt709:m=bt709:r=limited",
		"format=yuv420p")

	return strings.Join(chain, ","), nil
}

// buildConvertArgs returns the ffmpeg arguments that cut and convert a source.
func buildConvertArgs(ctx context.Context, src string, dst string, info videoInfo,
	req exportRequest, startSec float64, duration float64,
) ([]string, error) {
	args := []string{"-y", "-ss", fmt.Sprintf("%.6f", startSec), "-i", src}

	args = append(args, "-t", fmt.Sprintf("%.6f", duration), "-map", "0:v:0")

	if info.hasAudio {
		args = append(args, "-map", "0:a:0")
	}

	if !info.needsConvert() {
		// The source is already MP4 friendly, so keep every original byte.
		args = append(args, "-c", "copy", "-avoid_negative_ts", "make_zero")
	} else {
		chain, err := buildFilterChain(ctx, info, req.Colour)
		if err != nil {
			return nil, err
		}

		if chain != "" {
			args = append(args, "-vf", chain)
		}

		args = append(args, videoEncoderArgs(req.Colour, info)...)
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}

	args = append(args,
		"-fps_mode", "passthrough",
		"-video_track_timescale", strconv.FormatInt(info.rateNum, 10),
		"-movflags", "+faststart",
		"-progress", "pipe:1", "-nostats",
		dst)

	return args, nil
}

// videoEncoderArgs returns the encoder settings for the chosen colour mode.
func videoEncoderArgs(colour string, info videoInfo) []string {
	if colour != colourHDR {
		return []string{"-c:v", "libx264", "-crf", "18", "-preset", "slow"}
	}

	// Keep HDR by re-encoding 10 bit and restating the source colour tags.
	params := fmt.Sprintf("colorprim=%s:transfer=%s:colormatrix=%s",
		"bt2020", info.colorTransfer, info.colorSpace)

	return []string{
		"-c:v", "libx265", "-crf", "20", "-pix_fmt", "yuv420p10le",
		"-x265-params", params, "-tag:v", "hvc1",
	}
}

// runConvert executes ffmpeg and reports its frame counter as it works.
func runConvert(ctx context.Context, args []string, total int,
	job *jobRunner, log *ringLog, verbose bool,
) error {
	if verbose {
		fmt.Fprintf(os.Stderr, "ffmpeg %s\n", strings.Join(args, " "))
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open progress pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open error pipe: %w", err)
	}

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	var wait sync.WaitGroup

	wait.Add(2)

	go func() {
		defer wait.Done()

		readProgress(stdout, total, job)
	}()

	go func() {
		defer wait.Done()

		readLog(stderr, log)
	}()

	wait.Wait()

	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("%w: %w", errConvertFail, err)
	}

	return nil
}

// readProgress turns the ffmpeg progress stream into progress events.
func readProgress(stdout io.Reader, total int, job *jobRunner) {
	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		value, found := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "frame=")
		if !found {
			continue
		}

		frame, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			continue
		}

		job.emit(eventProgress, map[string]any{
			"stage": "convert", "frame": frame, "total": total,
		})
	}
}

// readLog keeps the tail of the ffmpeg error output for a failure report.
func readLog(stderr io.Reader, log *ringLog) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		log.add(scanner.Text())
	}
}

// exportPipeline cuts, converts and embeds in one pass.
// ensureExportDirs creates the scratch directory and the output directory.
func ensureExportDirs(opts options, req exportRequest) error {
	err := ensureDir(opts.tmpDir)
	if err != nil {
		return err
	}

	return ensureDir(filepath.Dir(req.Output))
}

func exportPipeline(ctx context.Context, opts options, req exportRequest,
	job *jobRunner, log *ringLog,
) error {
	videoPath := filepath.Join(opts.videoDir, req.Video)
	replayPath := filepath.Join(opts.replayDir, req.Replay)

	info, err := probeSource(ctx, videoPath)
	if err != nil {
		return err
	}

	if req.CutEnd <= req.CutStart || req.CutStart < 0 || req.CutEnd >= info.frames {
		return fmt.Errorf("%w: frames %d to %d of %d",
			errBadCut, req.CutStart, req.CutEnd, info.frames)
	}

	startFrame := req.CutStart

	if !info.needsConvert() {
		// A stream copy can only begin on a keyframe, so snap backwards.
		keyTime, keyErr := keyframeBefore(ctx, videoPath, frameSeconds(req.CutStart, info))
		if keyErr != nil {
			return keyErr
		}

		startFrame = secondsFrame(keyTime, info)
	}

	startSec := frameSeconds(startFrame, info)
	duration := frameSeconds(req.CutEnd+1, info) - startSec
	total := req.CutEnd - startFrame + 1

	cutFile := filepath.Join(opts.tmpDir, "replay_embed_cut.mp4")

	args, err := buildConvertArgs(ctx, videoPath, cutFile, info, req, startSec, duration)
	if err != nil {
		return err
	}

	err = runConvert(ctx, args, total, job, log, opts.verbose)
	if err != nil {
		return err
	}

	if !opts.keepTemp {
		defer os.Remove(cutFile)
	}

	return embedCut(ctx, opts, req, replayPath, cutFile, startFrame, job)
}

// embedCut builds the telemetry track for the cut file and muxes the output.
func embedCut(ctx context.Context, opts options, req exportRequest, replayPath string,
	cutFile string, startFrame int, job *jobRunner,
) error {
	// Re-probe so the sample count matches the frames ffmpeg actually wrote.
	cutInfo, err := probeSource(ctx, cutFile)
	if err != nil {
		return err
	}

	job.emit(eventProgress, map[string]any{
		"stage": "telemetry", "frame": 0, "total": cutInfo.frames,
	})

	packets, err := readReplay(replayPath)
	if err != nil {
		return err
	}

	// Output frame 0 is source frame startFrame, so fold that into the offset.
	embedOpts := opts
	embedOpts.offset = 0
	embedOpts.offsetFrames = req.OffsetFrames + startFrame
	embedOpts.mapMode = req.MapMode
	embedOpts.hasAudio = cutInfo.hasAudio
	embedOpts.out = req.Output

	samples, stats := buildSamples(packets, cutInfo, embedOpts)

	stats.report(cutInfo.frames)

	tmpFile := filepath.Join(opts.tmpDir, "replay_embed_telemetry.mp4")

	err = writeTelemetryMP4(tmpFile, samples, cutInfo)
	if err != nil {
		return err
	}

	if !opts.keepTemp {
		defer os.Remove(tmpFile)
	}

	job.emit(eventProgress, map[string]any{
		"stage": "mux", "frame": 0, "total": cutInfo.frames,
	})

	err = mux(ctx, embedOpts, cutFile, tmpFile)
	if err != nil {
		return err
	}

	job.emit(eventProgress, map[string]any{
		"stage": "verify", "frame": cutInfo.frames, "total": cutInfo.frames,
	})

	err = verify(ctx, req.Output, cutInfo)
	if err != nil {
		return err
	}

	job.emit(eventDone, map[string]any{
		"output":     req.Output,
		"frames":     cutInfo.frames,
		"startFrame": startFrame,
		"covered":    stats.covered,
	})

	return nil
}

// runExport drives one export to completion and reports the outcome.
func runExport(ctx context.Context, opts options, req exportRequest, job *jobRunner) {
	defer job.finish()

	log := &ringLog{}

	err := ensureExportDirs(opts, req)
	if err == nil {
		err = exportPipeline(ctx, opts, req, job, log)
	}

	if err != nil {
		job.emit(eventError, map[string]any{
			"message": err.Error(),
			"log":     log.snapshot(),
		})
	}
}
