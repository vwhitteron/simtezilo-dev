package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// packet is one deciphered telemetry frame and its sequence ID.
type packet struct {
	sequenceID uint32
	data       []byte
}

// readReplay loads a replay file and splits it into telemetry packets.
func readReplay(path string) ([]packet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read replay: %w", err)
	}

	raw, err = decompress(raw)
	if err != nil {
		return nil, err
	}

	return splitPackets(raw)
}

// decompress expands the replay when it carries a gzip header.
func decompress(raw []byte) ([]byte, error) {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return raw, nil
	}

	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("open gzip stream: %w", err)
	}

	defer reader.Close()

	out, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("decompress replay: %w", err)
	}

	return out, nil
}

// splitPackets divides the replay body into fixed size telemetry packets.
func splitPackets(raw []byte) ([]packet, error) {
	start := bytes.Index(raw, []byte(packetMagic))
	if start < 0 {
		return nil, errNoPackets
	}

	if start > 0 {
		fmt.Fprintf(os.Stderr, "warning: skipped %d leading bytes before first packet\n", start)
	}

	size, err := detectPacketSize(raw[start:])
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "packet size: %d bytes\n", size)

	packets := make([]packet, 0, (len(raw)-start)/size)

	for offset := start; offset+size <= len(raw); offset += size {
		frame := raw[offset : offset+size]

		if string(frame[:len(packetMagic)]) != packetMagic {
			return nil, fmt.Errorf("%w: bad magic at byte %d", errNoPackets, offset)
		}

		packets = append(packets, packet{
			sequenceID: binary.LittleEndian.Uint32(frame[sequenceIDOffset:]),
			data:       frame,
		})
	}

	if remainder := (len(raw) - start) % size; remainder != 0 {
		fmt.Fprintf(os.Stderr, "warning: dropped %d trailing bytes of partial packet\n", remainder)
	}

	if len(packets) == 0 {
		return nil, errNoPackets
	}

	return packets, nil
}

// detectPacketSize measures the distance between the first two packet headers.
// Packet length varies by GT7 format, so it is read from the file itself.
func detectPacketSize(raw []byte) (int, error) {
	magic := []byte(packetMagic)

	next := bytes.Index(raw[len(magic):], magic)
	if next < 0 {
		// A single packet file. Take the whole remainder as one packet.
		next = len(raw) - len(magic)
	}

	size := next + len(magic)

	if size < minPacketSize || size > maxPacketSize {
		return 0, fmt.Errorf("%w: implausible packet size %d bytes", errNoPackets, size)
	}

	if size < sequenceIDOffset+4 {
		return 0, fmt.Errorf("%w: packet size %d has no sequence ID", errNoPackets, size)
	}

	return size, nil
}

// sampleStats records how the replay was fitted onto the video timeline.
type sampleStats struct {
	packets   int
	gaps      int
	resets    int
	headPad   int
	tailPad   int
	covered   int
	replaySec float64
	videoSec  float64
}

// report prints a summary of the telemetry to video fit.
func (s sampleStats) report(frames int) {
	fmt.Fprintf(os.Stderr, "replay: %d packets, %.3f s, %d dropped frames\n",
		s.packets, s.replaySec, s.gaps)

	if s.resets > 0 {
		fmt.Fprintf(os.Stderr,
			"warning: %d sequence ID resets found, so the replay holds more than one session\n",
			s.resets)
		fmt.Fprintf(os.Stderr,
			"warning: frames after a reset are numbered contiguously and may not align\n")
	}

	fmt.Fprintf(os.Stderr, "mapped: %d/%d frames from real packets, %d padded at head, %d at tail\n",
		s.covered, frames, s.headPad, s.tailPad)

	if drift := s.replaySec - s.videoSec; math.Abs(drift) > 1 {
		fmt.Fprintf(os.Stderr, "warning: replay is %.1f s %s than the video\n",
			math.Abs(drift), longerOrShorter(drift))
	}
}

// longerOrShorter describes the sign of a duration difference.
func longerOrShorter(drift float64) string {
	if drift > 0 {
		return "longer"
	}

	return "shorter"
}

// buildSamples produces exactly one telemetry packet per video frame.
func buildSamples(packets []packet, info videoInfo, opts options) ([][]byte, sampleStats) {
	index, shape := indexBySequence(packets)

	stats := sampleStats{
		packets:   len(packets),
		gaps:      shape.gaps,
		resets:    shape.resets,
		replaySec: float64(shape.span) / telemetryRate,
		videoSec:  float64(info.frames) * float64(info.rateDen) / float64(info.rateNum),
	}

	offsetFrames := int(math.Round(opts.offset*telemetryRate)) + opts.offsetFrames
	samples := make([][]byte, info.frames)

	var (
		last     []byte
		seenReal bool
	)

	for frame := range info.frames {
		wanted := telemetryIndex(frame, info, opts) + offsetFrames

		data, ok := index[wanted]

		switch {
		case ok:
			last = data
			seenReal = true
			stats.covered++
		case !seenReal:
			// Hold the first packet until the replay reaches this frame.
			last = packets[0].data
			stats.headPad++
		default:
			// Hold the last packet across a gap or past the replay end.
			stats.tailPad++
		}

		samples[frame] = last
	}

	return samples, stats
}

// telemetryIndex returns the replay packet index for a video frame.
func telemetryIndex(frame int, info videoInfo, opts options) int {
	if opts.mapMode == "sequential" {
		return frame
	}

	seconds := float64(frame) * float64(info.rateDen) / float64(info.rateNum)

	return int(math.Round(seconds * telemetryRate))
}

// maxSequenceStep is the largest credible jump between two sequence IDs.
// A larger jump is treated as a counter reset rather than a one hour gap.
const maxSequenceStep = 60 * 60 * telemetryRate

// replayShape describes the sequence ID structure of a replay.
type replayShape struct {
	gaps   int // Frames the recorder dropped
	resets int // Points where the sequence ID restarted
	span   int // Frames from the first packet to the last
}

// indexBySequence maps a frame index to its telemetry payload.
// Frame indices follow the sequence ID, so a dropped frame leaves a hole
// rather than shifting every later packet forward.
func indexBySequence(packets []packet) (map[int][]byte, replayShape) {
	index := make(map[int][]byte, len(packets))

	var (
		shape replayShape
		frame int
	)

	index[0] = packets[0].data

	for current := 1; current < len(packets); current++ {
		// Compare as signed values so a backwards jump cannot underflow.
		step := int64(packets[current].sequenceID) - int64(packets[current-1].sequenceID)

		switch {
		case step == 1:
		case step > 1 && step <= maxSequenceStep:
			shape.gaps += int(step - 1)
		default:
			// A repeat, a backwards jump or an absurd gap. Keep the replay
			// contiguous rather than parking the packet out of reach.
			shape.resets++

			step = 1
		}

		frame += int(step)
		index[frame] = packets[current].data
	}

	shape.span = frame + 1

	return index, shape
}
