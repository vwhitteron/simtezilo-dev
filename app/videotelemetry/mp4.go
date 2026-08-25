package videotelemetry

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// maxMoovSize caps the movie box so a truncated or hostile file can never
// force a huge allocation. 64 MiB comfortably holds a real world moov, whose
// sample tables are the only large children.
const maxMoovSize = 64 << 20

// gpmdFormat is the four character sample entry code the writer uses for the
// telemetry track, borrowed from GoPro so ffmpeg will copy the track.
const gpmdFormat = "gpmd"

// metaHandlerType is the mdia/hdlr handler_type of a timed metadata track.
const metaHandlerType = "meta"

var (
	errNoTelemetryTrack    = errors.New("no meta/gpmd telemetry track found")
	errMultipleTracks      = errors.New("more than one meta/gpmd telemetry track found")
	errMoovTooLarge        = errors.New("moov box exceeds sanity limit")
	errMalformedBox        = errors.New("malformed mp4 box")
	errMissingBox          = errors.New("required mp4 box not found")
	errSampleTableMismatch = errors.New("sample table entry counts do not agree")
	errNoSamples           = errors.New("telemetry track has zero samples")
	errVariableRate        = errors.New("telemetry track is not constant rate")
)

// boxHeader records where a box's body lies, without holding its contents.
type boxHeader struct {
	typ       string
	bodyStart int64
	bodyEnd   int64
}

// bodyLen returns the number of body bytes described by the header.
func (h boxHeader) bodyLen() int64 { return h.bodyEnd - h.bodyStart }

// readBoxHeader reads one box header at pos, honouring the 64 bit largesize
// form and a size of zero ("extends to the end of the enclosing range").
// It never trusts the file enough to read past end.
func readBoxHeader(reader io.ReaderAt, pos, end int64) (boxHeader, error) {
	if end-pos < 8 {
		return boxHeader{}, fmt.Errorf("%w: truncated header at %d", errMalformedBox, pos)
	}

	var hdr [8]byte

	_, err := reader.ReadAt(hdr[:], pos)
	if err != nil {
		return boxHeader{}, fmt.Errorf("read box header at %d: %w", pos, err)
	}

	size := int64(binary.BigEndian.Uint32(hdr[:4]))
	typ := string(hdr[4:8])
	headerLen := int64(8)

	switch size {
	case 0:
		size = end - pos
	case 1:
		size, err = readLargeSize(reader, pos, end)
		if err != nil {
			return boxHeader{}, err
		}

		headerLen = 16
	}

	if size < headerLen {
		return boxHeader{}, fmt.Errorf("%w: box %q size %d smaller than its header at %d", errMalformedBox, typ, size, pos)
	}

	bodyEnd := pos + size
	if bodyEnd > end || bodyEnd < pos {
		return boxHeader{}, fmt.Errorf("%w: box %q at %d overruns its enclosing range", errMalformedBox, typ, pos)
	}

	return boxHeader{typ: typ, bodyStart: pos + headerLen, bodyEnd: bodyEnd}, nil
}

// readLargeSize reads the 64 bit size that follows a box header whose 32 bit
// size field is 1.
func readLargeSize(reader io.ReaderAt, pos, end int64) (int64, error) {
	if end-pos < 16 {
		return 0, fmt.Errorf("%w: truncated largesize header at %d", errMalformedBox, pos)
	}

	var large [8]byte

	_, err := reader.ReadAt(large[:], pos+8)
	if err != nil {
		return 0, fmt.Errorf("read largesize at %d: %w", pos, err)
	}

	largeSize := binary.BigEndian.Uint64(large[:])
	if largeSize > 1<<62 {
		return 0, fmt.Errorf("%w: implausible largesize %d at %d", errMalformedBox, largeSize, pos)
	}

	return int64(largeSize), nil
}

// scanBoxes walks sibling boxes across [start, end) and returns their headers.
func scanBoxes(reader io.ReaderAt, start, end int64) ([]boxHeader, error) {
	var boxes []boxHeader

	pos := start
	for pos < end {
		header, err := readBoxHeader(reader, pos, end)
		if err != nil {
			return nil, err
		}

		boxes = append(boxes, header)
		pos = header.bodyEnd
	}

	return boxes, nil
}

// findBox returns the first box of the given type among siblings.
func findBox(boxes []boxHeader, typ string) (boxHeader, bool) {
	for _, h := range boxes {
		if h.typ == typ {
			return h, true
		}
	}

	return boxHeader{}, false
}

// findBoxes returns every box of the given type among siblings.
func findBoxes(boxes []boxHeader, typ string) []boxHeader {
	var out []boxHeader

	for _, h := range boxes {
		if h.typ == typ {
			out = append(out, h)
		}
	}

	return out
}

// readBody reads a box's entire body into memory. Callers only use this for
// boxes already bounded by maxMoovSize, so the allocation is never unbounded.
func readBody(reader io.ReaderAt, h boxHeader) ([]byte, error) {
	buf := make([]byte, h.bodyLen())

	_, err := reader.ReadAt(buf, h.bodyStart)
	if err != nil {
		return nil, fmt.Errorf("read box %q body: %w", h.typ, err)
	}

	return buf, nil
}

// childBoxes scans a box's own children.
func childBoxes(reader io.ReaderAt, h boxHeader) ([]boxHeader, error) {
	return scanBoxes(reader, h.bodyStart, h.bodyEnd)
}

// requireBox scans children of parent and returns the named box, or a
// descriptive error if it is absent.
func requireBox(reader io.ReaderAt, parent boxHeader, typ string) (boxHeader, error) {
	children, err := childBoxes(reader, parent)
	if err != nil {
		return boxHeader{}, err
	}

	h, ok := findBox(children, typ)
	if !ok {
		return boxHeader{}, fmt.Errorf("%w: %q inside %q", errMissingBox, typ, parent.typ)
	}

	return h, nil
}

// trakHandle names a candidate trak that turned out to be the telemetry track.
type trakHandle struct {
	stbl boxHeader
	mdhd []byte
}

// findTelemetryTrak locates the trak whose handler is meta and whose first
// sample description is gpmd. It errors if none or more than one match.
func findTelemetryTrak(reader io.ReaderAt, fileSize int64) (trakHandle, error) {
	moovChildren, err := loadMoovChildren(reader, fileSize)
	if err != nil {
		return trakHandle{}, err
	}

	matches, err := matchTelemetryTraks(reader, moovChildren)
	if err != nil {
		return trakHandle{}, err
	}

	switch len(matches) {
	case 0:
		return trakHandle{}, errNoTelemetryTrack
	case 1:
		return matches[0], nil
	default:
		return trakHandle{}, fmt.Errorf("%w: found %d", errMultipleTracks, len(matches))
	}
}

// loadMoovChildren finds the file's moov box, checks it against
// maxMoovSize, and returns its children.
func loadMoovChildren(reader io.ReaderAt, fileSize int64) ([]boxHeader, error) {
	top, err := scanBoxes(reader, 0, fileSize)
	if err != nil {
		return nil, err
	}

	moov, ok := findBox(top, "moov")
	if !ok {
		return nil, fmt.Errorf("%w: %q", errMissingBox, "moov")
	}

	if moov.bodyLen() > maxMoovSize {
		return nil, fmt.Errorf("%w: %d bytes", errMoovTooLarge, moov.bodyLen())
	}

	return childBoxes(reader, moov)
}

// matchTelemetryTraks inspects every trak under moov and returns the handles
// of those that look like the telemetry track.
func matchTelemetryTraks(reader io.ReaderAt, moovChildren []boxHeader) ([]trakHandle, error) {
	var matches []trakHandle

	for _, trak := range findBoxes(moovChildren, "trak") {
		handle, isTelemetry, err := inspectTrak(reader, trak)
		if err != nil {
			return nil, err
		}

		if isTelemetry {
			matches = append(matches, handle)
		}
	}

	return matches, nil
}

// inspectTrak reports whether a trak is the meta/gpmd telemetry track and, if
// so, returns the handle needed to parse its sample table.
func inspectTrak(reader io.ReaderAt, trak boxHeader) (trakHandle, bool, error) {
	mdia, err := requireBox(reader, trak, "mdia")
	if err != nil {
		return trakHandle{}, false, err
	}

	mdhd, isMeta, err := trakHandlerIsMeta(reader, mdia)
	if err != nil || !isMeta {
		return trakHandle{}, false, err
	}

	stbl, isGPMD, err := trakStblIsGPMD(reader, mdia)
	if err != nil || !isGPMD {
		return trakHandle{}, false, err
	}

	return trakHandle{stbl: stbl, mdhd: mdhd}, true, nil
}

// trakHandlerIsMeta reads mdia/mdhd and mdia/hdlr, reporting whether the
// handler is the "meta" timed metadata handler. It returns the mdhd body
// either way, since the caller needs it once a trak is confirmed telemetry.
func trakHandlerIsMeta(reader io.ReaderAt, mdia boxHeader) ([]byte, bool, error) {
	mdhdBox, err := requireBox(reader, mdia, "mdhd")
	if err != nil {
		return nil, false, err
	}

	mdhd, err := readBody(reader, mdhdBox)
	if err != nil {
		return nil, false, err
	}

	hdlrBox, err := requireBox(reader, mdia, "hdlr")
	if err != nil {
		return nil, false, err
	}

	hdlr, err := readBody(reader, hdlrBox)
	if err != nil {
		return nil, false, err
	}

	if len(hdlr) < 12 {
		return nil, false, fmt.Errorf("%w: hdlr body too short", errMalformedBox)
	}

	// handler_type sits at body offset 8..12: version+flags(4), predefined(4).
	return mdhd, string(hdlr[8:12]) == metaHandlerType, nil
}

// trakStblIsGPMD reads mdia/minf/stbl and reports whether its first sample
// description is gpmd, returning the stbl box either way.
func trakStblIsGPMD(reader io.ReaderAt, mdia boxHeader) (boxHeader, bool, error) {
	minf, err := requireBox(reader, mdia, "minf")
	if err != nil {
		return boxHeader{}, false, err
	}

	stbl, err := requireBox(reader, minf, "stbl")
	if err != nil {
		return boxHeader{}, false, err
	}

	stsdBox, err := requireBox(reader, stbl, "stsd")
	if err != nil {
		return boxHeader{}, false, err
	}

	format, err := firstSampleFormat(reader, stsdBox)
	if err != nil {
		return boxHeader{}, false, err
	}

	return stbl, format == gpmdFormat, nil
}

// firstSampleFormat returns the four character code of stsd's first entry.
func firstSampleFormat(reader io.ReaderAt, stsdBox boxHeader) (string, error) {
	if stsdBox.bodyLen() < 8 {
		return "", fmt.Errorf("%w: stsd body too short", errMalformedBox)
	}

	body, err := readBody(reader, stsdBox)
	if err != nil {
		return "", err
	}

	entryCount := binary.BigEndian.Uint32(body[4:8])
	if entryCount == 0 || len(body) < 16 {
		return "", fmt.Errorf("%w: stsd has no sample entries", errMalformedBox)
	}

	// Each entry begins with its own size(4) and format(4); we only need the
	// first entry's format.
	return string(body[12:16]), nil
}

// mdhdTimescale extracts the media timescale from an mdhd body, honouring
// both the 32 bit (version 0) and 64 bit (version 1) field layouts.
func mdhdTimescale(mdhd []byte) (uint32, error) {
	if len(mdhd) < 4 {
		return 0, fmt.Errorf("%w: mdhd body too short", errMalformedBox)
	}

	version := mdhd[0]

	var offset int
	if version == 1 {
		offset = 4 + 8 + 8 // version+flags, 64 bit creation, 64 bit modification
	} else {
		offset = 4 + 4 + 4 // version+flags, 32 bit creation, 32 bit modification
	}

	if len(mdhd) < offset+4 {
		return 0, fmt.Errorf("%w: mdhd body too short for its version", errMalformedBox)
	}

	return binary.BigEndian.Uint32(mdhd[offset : offset+4]), nil
}

// parseSampleTable resolves a trak's stbl into a flat, ordered sample table
// plus the media timescale and per-sample tick delta.
func parseSampleTable(reader io.ReaderAt, trak trakHandle) ([]sampleLoc, uint32, uint32, error) {
	timescale, err := mdhdTimescale(trak.mdhd)
	if err != nil {
		return nil, 0, 0, err
	}

	sampleDelta, sampleSizes, err := parseSampleSizing(reader, trak.stbl)
	if err != nil {
		return nil, 0, 0, err
	}

	samples, err := parseChunkPlan(reader, trak.stbl, sampleSizes)
	if err != nil {
		return nil, 0, 0, err
	}

	return samples, timescale, sampleDelta, nil
}

// parseSampleSizing reads stts and stsz, checking they agree on the number
// of samples in the track.
func parseSampleSizing(reader io.ReaderAt, stbl boxHeader) (uint32, []uint32, error) {
	sttsBox, err := requireBox(reader, stbl, "stts")
	if err != nil {
		return 0, nil, err
	}

	sttsCount, sampleDelta, err := parseSTTS(reader, sttsBox)
	if err != nil {
		return 0, nil, err
	}

	stszBox, err := requireBox(reader, stbl, "stsz")
	if err != nil {
		return 0, nil, err
	}

	sampleSizes, err := parseSTSZ(reader, stszBox)
	if err != nil {
		return 0, nil, err
	}

	if len(sampleSizes) == 0 {
		return 0, nil, errNoSamples
	}

	if sttsCount != uint64(len(sampleSizes)) {
		return 0, nil, fmt.Errorf("%w: stts describes %d samples, stsz describes %d",
			errSampleTableMismatch, sttsCount, len(sampleSizes))
	}

	return sampleDelta, sampleSizes, nil
}

// parseChunkPlan reads stsc and the chunk offset table, then expands them
// against sampleSizes into a flat, ordered sample table.
func parseChunkPlan(reader io.ReaderAt, stbl boxHeader, sampleSizes []uint32) ([]sampleLoc, error) {
	children, err := childBoxes(reader, stbl)
	if err != nil {
		return nil, err
	}

	stscBox, err := requireBox(reader, stbl, "stsc")
	if err != nil {
		return nil, err
	}

	stscEntries, err := parseSTSC(reader, stscBox)
	if err != nil {
		return nil, err
	}

	chunkOffsets, err := parseChunkOffsets(reader, children)
	if err != nil {
		return nil, err
	}

	return expandSamples(chunkOffsets, stscEntries, sampleSizes)
}

// parseSTTS reads the time-to-sample box, returning the total sample count it
// describes and the delta of its first run (the track is constant rate, so
// every real run shares one delta; a mismatched later run would only affect
// duration accounting, which this package does not attempt to reproduce
// exactly).
func parseSTTS(reader io.ReaderAt, box boxHeader) (uint64, uint32, error) {
	const entrySize = 8

	entries, err := parseRunLengthTable(reader, box, entrySize)
	if err != nil {
		return 0, 0, err
	}

	var total uint64

	var firstDelta uint32

	// Callers map a sample index to a presentation time with a single delta, so a
	// track whose runs disagree would be silently mistimed. Runs may still be split
	// across several entries, which is harmless as long as the delta never changes.
	for pos := 0; pos < len(entries); pos += entrySize {
		count := binary.BigEndian.Uint32(entries[pos : pos+4])
		delta := binary.BigEndian.Uint32(entries[pos+4 : pos+8])

		total += uint64(count)

		if pos == 0 {
			firstDelta = delta

			continue
		}

		if delta != firstDelta {
			return 0, 0, fmt.Errorf("%w: stts delta changes from %d to %d",
				errVariableRate, firstDelta, delta)
		}
	}

	return total, firstDelta, nil
}

// stscEntry is one sample-to-chunk run.
type stscEntry struct {
	firstChunk      uint32
	samplesPerChunk uint32
}

// parseSTSC reads the sample-to-chunk box's run table.
func parseSTSC(reader io.ReaderAt, box boxHeader) ([]stscEntry, error) {
	raw, err := parseRunLengthTable(reader, box, 12)
	if err != nil {
		return nil, err
	}

	entries := make([]stscEntry, 0, len(raw)/12)

	for i := 0; i < len(raw); i += 12 {
		entries = append(entries, stscEntry{
			firstChunk:      binary.BigEndian.Uint32(raw[i : i+4]),
			samplesPerChunk: binary.BigEndian.Uint32(raw[i+4 : i+8]),
		})
	}

	return entries, nil
}

// parseRunLengthTable reads a version 0 full box shaped as
// entry_count(4) followed by entry_count entries of entrySize bytes, guarding
// the declared count against the box's actual remaining length before
// allocating.
func parseRunLengthTable(reader io.ReaderAt, box boxHeader, entrySize int) ([]byte, error) {
	if box.bodyLen() < 8 {
		return nil, fmt.Errorf("%w: %q body too short", errMalformedBox, box.typ)
	}

	body, err := readBody(reader, box)
	if err != nil {
		return nil, err
	}

	count := binary.BigEndian.Uint32(body[4:8])

	maxEntries := uint64(len(body)-8) / uint64(entrySize) //nolint:gosec // entrySize is a small positive constant
	if uint64(count) > maxEntries {
		return nil, fmt.Errorf("%w: %q declares %d entries, room for %d", errMalformedBox, box.typ, count, maxEntries)
	}

	end := 8 + uint64(count)*uint64(entrySize) //nolint:gosec // entrySize is a small positive constant

	return body[8:end], nil
}

// parseSTSZ reads the sample size box, returning one size per sample whether
// the box holds a uniform size or a per-sample table.
func parseSTSZ(reader io.ReaderAt, h boxHeader) ([]uint32, error) {
	if h.bodyLen() < 12 {
		return nil, fmt.Errorf("%w: stsz body too short", errMalformedBox)
	}

	body, err := readBody(reader, h)
	if err != nil {
		return nil, err
	}

	uniformSize := binary.BigEndian.Uint32(body[4:8])
	sampleCount := binary.BigEndian.Uint32(body[8:12])

	if uniformSize != 0 {
		sizes := make([]uint32, sampleCount)
		for i := range sizes {
			sizes[i] = uniformSize
		}

		return sizes, nil
	}

	maxEntries := uint64(len(body)-12) / 4 //nolint:gosec // body length is bounded by maxMoovSize
	if uint64(sampleCount) > maxEntries {
		return nil, fmt.Errorf("%w: stsz declares %d samples, room for %d", errMalformedBox, sampleCount, maxEntries)
	}

	sizes := make([]uint32, sampleCount)
	for i := range sizes {
		off := 12 + i*4
		sizes[i] = binary.BigEndian.Uint32(body[off : off+4])
	}

	return sizes, nil
}

// parseChunkOffsets reads stco (32 bit) or co64 (64 bit) chunk offsets,
// whichever is present.
func parseChunkOffsets(reader io.ReaderAt, stblChildren []boxHeader) ([]uint64, error) {
	if box, ok := findBox(stblChildren, "co64"); ok {
		raw, err := parseRunLengthTable(reader, box, 8)
		if err != nil {
			return nil, err
		}

		offsets := make([]uint64, len(raw)/8)
		for idx := range offsets {
			offsets[idx] = binary.BigEndian.Uint64(raw[idx*8 : idx*8+8])
		}

		return offsets, nil
	}

	if box, ok := findBox(stblChildren, "stco"); ok {
		raw, err := parseRunLengthTable(reader, box, 4)
		if err != nil {
			return nil, err
		}

		offsets := make([]uint64, len(raw)/4)
		for idx := range offsets {
			offsets[idx] = uint64(binary.BigEndian.Uint32(raw[idx*4 : idx*4+4]))
		}

		return offsets, nil
	}

	return nil, fmt.Errorf("%w: %q or %q inside %q", errMissingBox, "stco", "co64", "stbl")
}

// expandSamples walks chunks in order, assigning each sample its offset from
// the chunk's base offset plus the running size of the samples before it in
// that chunk.
func expandSamples(chunkOffsets []uint64, stsc []stscEntry, sizes []uint32) ([]sampleLoc, error) {
	if len(stsc) == 0 {
		return nil, fmt.Errorf("%w: stsc has no entries", errMalformedBox)
	}

	samples := make([]sampleLoc, 0, len(sizes))

	sampleIndex := 0
	stscIndex := 0

	for chunkIdx, chunkOffset := range chunkOffsets {
		chunkNum := uint32(chunkIdx) + 1 //nolint:gosec // chunk counts are bounded by the moov cap

		for stscIndex+1 < len(stsc) && stsc[stscIndex+1].firstChunk <= chunkNum {
			stscIndex++
		}

		samplesPerChunk := stsc[stscIndex].samplesPerChunk

		offset := chunkOffset

		for range samplesPerChunk {
			if sampleIndex >= len(sizes) {
				return nil, fmt.Errorf("%w: chunk table describes more samples than stsz", errSampleTableMismatch)
			}

			size := sizes[sampleIndex]
			samples = append(samples, sampleLoc{offset: offset, size: size})
			offset += uint64(size)
			sampleIndex++
		}
	}

	if sampleIndex != len(sizes) {
		return nil, fmt.Errorf("%w: chunk table describes %d samples, stsz describes %d",
			errSampleTableMismatch, sampleIndex, len(sizes))
	}

	return samples, nil
}
