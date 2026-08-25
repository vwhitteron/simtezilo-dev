// Package videotelemetry reads a GT7 telemetry track embedded in an MP4 file
// by tools/replay_embed. It is a pure Go, read-only MP4 demuxer: no ffmpeg at
// runtime, stdlib only. The app ships to a Raspberry Pi where spawning ffmpeg
// per playback is undesirable.
package videotelemetry

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

// PacketMagic is the header that starts every deciphered GT7 telemetry packet.
const PacketMagic = "0S7G"

// SequenceIDOffset is the byte offset of the little-endian uint32 sequence ID
// within a deciphered GT7 packet.
const SequenceIDOffset = 112

// validationSpread bounds how many samples Open checks against SequenceIDOffset
// and PacketMagic. Reading all 7704+ samples at open is affordable, but a
// bounded spread (first, last, and roughly this many in between) catches a
// corrupt track just as reliably in practice and keeps Open fast regardless of
// file size.
const validationSpread = 32

// errSampleRange reports a Sample call outside the indexed track.
var errSampleRange = errors.New("sample index out of range")

// errBadSample reports a sample that fails the magic or length check.
var errBadSample = errors.New("sample does not look like a GT7 telemetry packet")

// SequenceID reads the little-endian sequence ID from a deciphered GT packet.
// It returns false if the packet is too short or lacks the magic.
func SequenceID(packet []byte) (uint32, bool) {
	if len(packet) < SequenceIDOffset+4 {
		return 0, false
	}

	if !bytes.HasPrefix(packet, []byte(PacketMagic)) {
		return 0, false
	}

	return binary.LittleEndian.Uint32(packet[SequenceIDOffset : SequenceIDOffset+4]), true
}

// sampleLoc locates one sample's bytes within the file.
type sampleLoc struct {
	offset uint64
	size   uint32
}

// Index gives random access to the telemetry track of an MP4. It holds an
// open file handle and a fully resolved sample table; the samples themselves
// are read on demand.
type Index struct {
	file            *os.File
	samples         []sampleLoc
	timescale       uint32
	sampleDelta     uint32
	firstSequenceID uint32
}

// Open parses the MP4 at path and indexes its telemetry track. The returned
// Index holds an open file handle; the caller must Close it.
func Open(path string) (*Index, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	index, err := indexFile(file)
	if err != nil {
		file.Close()

		return nil, err
	}

	return index, nil
}

// indexFile builds an Index from an already open file.
func indexFile(file *os.File) (*Index, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}

	trak, err := findTelemetryTrak(file, info.Size())
	if err != nil {
		return nil, err
	}

	samples, timescale, sampleDelta, err := parseSampleTable(file, trak)
	if err != nil {
		return nil, err
	}

	index := &Index{
		file:        file,
		samples:     samples,
		timescale:   timescale,
		sampleDelta: sampleDelta,
	}

	err = index.validate()
	if err != nil {
		return nil, err
	}

	return index, nil
}

// validationIndices returns the sample indices Open should spot check: the
// first, the last, and roughly validationSpread evenly spaced samples between
// them.
func validationIndices(sampleCount int) []int {
	if sampleCount == 0 {
		return nil
	}

	seen := map[int]struct{}{0: {}, sampleCount - 1: {}}

	step := max(sampleCount/validationSpread, 1)

	for idx := 0; idx < sampleCount; idx += step {
		seen[idx] = struct{}{}
	}

	out := make([]int, 0, len(seen))
	for idx := range seen {
		out = append(out, idx)
	}

	sort.Ints(out)

	return out
}

// FrameCount returns the number of samples in the telemetry track.
func (i *Index) FrameCount() int { return len(i.samples) }

// Timescale returns the media timescale of the telemetry track, e.g. 60000.
func (i *Index) Timescale() uint32 { return i.timescale }

// SampleDelta returns the number of timescale ticks each sample spans, e.g. 1001.
//
// This is a single value because Open rejects a track whose stts runs disagree, so
// a sample index maps to a presentation time as index*SampleDelta/Timescale. Prefer
// that over any video frame index: an ffmpeg re-cut can leave the video and
// telemetry tracks with different sample counts.
func (i *Index) SampleDelta() uint32 { return i.sampleDelta }

// Duration returns the track's presentation duration, computed from
// FrameCount, SampleDelta and Timescale rather than read back from the file.
func (i *Index) Duration() time.Duration {
	ticks := uint64(i.FrameCount()) * uint64(i.sampleDelta) //nolint:gosec // frame count is never negative
	seconds := float64(ticks) / float64(i.timescale)

	return time.Duration(seconds * float64(time.Second))
}

// FirstSequenceID returns the sequence ID of sample 0.
func (i *Index) FirstSequenceID() uint32 { return i.firstSequenceID }

// Sample returns a fresh copy of sample n's bytes. The caller owns the
// returned slice. It returns an error if n is out of range or the read fails.
func (i *Index) Sample(n int) ([]byte, error) { //nolint:varnamelen // n matches the documented Sample(n int) signature
	if n < 0 || n >= len(i.samples) {
		return nil, fmt.Errorf("%w: %d, have %d", errSampleRange, n, len(i.samples))
	}

	loc := i.samples[n]

	buf := make([]byte, loc.size)

	_, err := i.file.ReadAt(buf, int64(loc.offset)) //nolint:gosec // offsets come from a bounded sample table
	if err != nil {
		return nil, fmt.Errorf("read sample %d: %w", n, err)
	}

	return buf, nil
}

// WriteGTR writes every sample to w in order, undoing the MP4 muxing to
// reproduce a flat stream of GT7 packets. It reuses an internal buffer across
// samples for speed; callers of Sample still get a slice they own.
func (i *Index) WriteGTR(w io.Writer) error { //nolint:varnamelen // w matches the documented WriteGTR(w io.Writer) signature
	var buf []byte

	for idx, loc := range i.samples {
		if cap(buf) < int(loc.size) {
			buf = make([]byte, loc.size)
		} else {
			buf = buf[:loc.size]
		}

		_, err := i.file.ReadAt(buf, int64(loc.offset)) //nolint:gosec // offsets come from a bounded sample table
		if err != nil {
			return fmt.Errorf("read sample %d: %w", idx, err)
		}

		_, err = w.Write(buf)
		if err != nil {
			return fmt.Errorf("write sample %d: %w", idx, err)
		}
	}

	return nil
}

// Close releases the underlying file handle.
func (i *Index) Close() error {
	return i.file.Close()
}

// validate spot checks a bounded spread of samples (see validationSpread) and
// records the first sample's sequence ID.
func (i *Index) validate() error {
	for _, idx := range validationIndices(len(i.samples)) {
		sample, err := i.Sample(idx)
		if err != nil {
			return err
		}

		_, ok := SequenceID(sample)
		if !ok {
			return fmt.Errorf("%w: sample %d", errBadSample, idx)
		}
	}

	first, err := i.Sample(0)
	if err != nil {
		return err
	}

	i.firstSequenceID, _ = SequenceID(first)

	return nil
}
