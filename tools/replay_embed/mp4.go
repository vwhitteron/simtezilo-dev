package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
)

// errTrackTooLarge reports a track that overflows the 32 bit MP4 fields.
var errTrackTooLarge = errors.New("telemetry track exceeds 32 bit MP4 limits")

// box builds a single MP4 box body before its header is prepended.
type box struct {
	buf []byte
}

// u16 appends a big endian uint16.
func (b *box) u16(value uint16) { b.buf = binary.BigEndian.AppendUint16(b.buf, value) }

// u32 appends a big endian uint32.
func (b *box) u32(value uint32) { b.buf = binary.BigEndian.AppendUint32(b.buf, value) }

// u64 appends a big endian uint64.
func (b *box) u64(value uint64) { b.buf = binary.BigEndian.AppendUint64(b.buf, value) }

// raw appends literal bytes.
func (b *box) raw(value []byte) { b.buf = append(b.buf, value...) }

// str appends the bytes of a string.
func (b *box) str(value string) { b.buf = append(b.buf, value...) }

// zero appends a run of zero bytes.
func (b *box) zero(count int) { b.buf = append(b.buf, make([]byte, count)...) }

// fullBox appends the header of a version 0 full box with 24 bit flags.
func (b *box) fullBox(flags uint32) {
	b.u32(flags & 0x00FFFFFF)
}

// matrix appends the 9 entry identity transform used by tkhd and mvhd.
func (b *box) matrix() {
	unity := [9]uint32{
		0x00010000, 0, 0,
		0, 0x00010000, 0,
		0, 0, 0x40000000,
	}

	for _, value := range unity {
		b.u32(value)
	}
}

// child appends a nested box.
func (b *box) child(name string, body []byte) { b.raw(makeBox(name, body)) }

// makeBox prepends the size and type header to a box body.
func makeBox(name string, body []byte) []byte {
	out := make([]byte, 0, 8+len(body))
	out = binary.BigEndian.AppendUint32(out, uint32(8+len(body))) //nolint:gosec // header bodies are small
	out = append(out, name...)

	return append(out, body...)
}

// writeTelemetryMP4 writes a file holding only the telemetry metadata track.
func writeTelemetryMP4(path string, samples [][]byte, info videoInfo) error {
	sampleSize, err := uniformSampleSize(samples)
	if err != nil {
		return err
	}

	err = checkTrackLimits(len(samples), sampleSize, info)
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create telemetry track: %w", err)
	}

	defer file.Close()

	writer := bufio.NewWriterSize(file, 1<<20)

	ftyp := buildFtyp()

	_, err = writer.Write(ftyp)
	if err != nil {
		return fmt.Errorf("write ftyp: %w", err)
	}

	// Write mdat with a plain 32 bit header, then record where samples begin.
	mdatSize := 8 + len(samples)*sampleSize
	header := make([]byte, 0, 8)
	header = binary.BigEndian.AppendUint32(header, uint32(mdatSize)) //nolint:gosec // bounded by checkTrackLimits
	header = append(header, "mdat"...)

	_, err = writer.Write(header)
	if err != nil {
		return fmt.Errorf("write mdat header: %w", err)
	}

	dataOffset := uint64(len(ftyp) + 8) //nolint:gosec // ftyp is a fixed small box

	for _, sample := range samples {
		_, err = writer.Write(sample)
		if err != nil {
			return fmt.Errorf("write sample data: %w", err)
		}
	}

	_, err = writer.Write(buildMoov(len(samples), sampleSize, dataOffset, info))
	if err != nil {
		return fmt.Errorf("write moov: %w", err)
	}

	err = writer.Flush()
	if err != nil {
		return fmt.Errorf("flush telemetry track: %w", err)
	}

	return nil
}

// uniformSampleSize returns the shared sample length, which stsz requires.
func uniformSampleSize(samples [][]byte) (int, error) {
	if len(samples) == 0 {
		return 0, errNoPackets
	}

	size := len(samples[0])

	for _, sample := range samples {
		if len(sample) != size {
			return 0, fmt.Errorf("%w: mixed packet sizes %d and %d",
				errNoPackets, size, len(sample))
		}
	}

	return size, nil
}

// checkTrackLimits rejects a track too large for the 32 bit box fields.
// The caller may then rely on the uint32 conversions below being safe.
func checkTrackLimits(sampleCount int, sampleSize int, info videoInfo) error {
	payload := uint64(sampleCount)*uint64(sampleSize) + 8 //nolint:gosec // sample count is never negative
	if payload > math.MaxUint32 {
		return fmt.Errorf("%w: %d bytes of sample data", errTrackTooLarge, payload)
	}

	duration := uint64(sampleCount) * uint64(info.rateDen) //nolint:gosec // both values are positive
	if duration > math.MaxUint32 {
		return fmt.Errorf("%w: duration %d ticks", errTrackTooLarge, duration)
	}

	if info.rateNum > math.MaxUint32 {
		return fmt.Errorf("%w: timescale %d", errTrackTooLarge, info.rateNum)
	}

	return nil
}

// buildFtyp returns the file type box.
func buildFtyp() []byte {
	var body box

	body.str("isom")
	body.u32(512)
	body.str("isom")
	body.str("iso2")
	body.str("mp41")

	return makeBox("ftyp", body.buf)
}

// buildMoov assembles the movie box for the telemetry track.
func buildMoov(sampleCount int, sampleSize int, dataOffset uint64, info videoInfo) []byte {
	// The track adopts the video timebase, so one sample spans one frame.
	timescale := uint32(info.rateNum)       //nolint:gosec // bounded by checkTrackLimits
	delta := uint32(info.rateDen)           //nolint:gosec // bounded by checkTrackLimits
	duration := uint32(sampleCount) * delta //nolint:gosec // bounded by checkTrackLimits

	var body box

	body.raw(buildMvhd(timescale, duration))
	body.raw(buildTrak(sampleCount, sampleSize, dataOffset, timescale, delta, duration))

	return makeBox("moov", body.buf)
}

// buildMvhd returns the movie header box.
func buildMvhd(timescale uint32, duration uint32) []byte {
	var body box

	body.fullBox(0)
	body.u32(0) // creation time
	body.u32(0) // modification time
	body.u32(timescale)
	body.u32(duration)
	body.u32(0x00010000) // rate
	body.u16(0)          // volume
	body.zero(2 + 8)     // reserved
	body.matrix()
	body.zero(24) // predefined
	body.u32(2)   // next track ID

	return makeBox("mvhd", body.buf)
}

// buildTrak returns the track box.
func buildTrak(sampleCount int, sampleSize int, dataOffset uint64, timescale uint32, delta uint32, duration uint32) []byte {
	var body box

	body.raw(buildTkhd(duration))
	body.raw(buildMdia(sampleCount, sampleSize, dataOffset, timescale, delta, duration))

	return makeBox("trak", body.buf)
}

// buildTkhd returns the track header box, enabled and in the presentation.
func buildTkhd(duration uint32) []byte {
	var body box

	body.fullBox(0x000007)
	body.u32(0) // creation time
	body.u32(0) // modification time
	body.u32(1) // track ID
	body.u32(0) // reserved
	body.u32(duration)
	body.zero(8) // reserved
	body.u16(0)  // layer
	body.u16(0)  // alternate group
	body.u16(0)  // volume
	body.u16(0)  // reserved
	body.matrix()
	body.u32(0) // width
	body.u32(0) // height

	return makeBox("tkhd", body.buf)
}

// buildMdia returns the media box.
func buildMdia(sampleCount int, sampleSize int, dataOffset uint64, timescale uint32, delta uint32, duration uint32) []byte {
	var body box

	body.raw(buildMdhd(timescale, duration))
	body.raw(buildHdlr())
	body.raw(buildMinf(sampleCount, sampleSize, dataOffset, delta))

	return makeBox("mdia", body.buf)
}

// buildMdhd returns the media header box.
func buildMdhd(timescale uint32, duration uint32) []byte {
	var body box

	body.fullBox(0)
	body.u32(0) // creation time
	body.u32(0) // modification time
	body.u32(timescale)
	body.u32(duration)
	body.u16(0x55C4) // language "und"
	body.u16(0)      // predefined

	return makeBox("mdhd", body.buf)
}

// buildHdlr returns the handler box declaring a metadata track.
func buildHdlr() []byte {
	var body box

	body.fullBox(0)
	body.u32(0) // predefined
	body.str("meta")
	body.zero(12) // reserved
	body.str("GT Telemetry")
	body.zero(1) // null terminator

	return makeBox("hdlr", body.buf)
}

// buildMinf returns the media information box.
func buildMinf(sampleCount int, sampleSize int, dataOffset uint64, delta uint32) []byte {
	var nmhd box

	nmhd.fullBox(0)

	var body box

	body.child("nmhd", nmhd.buf)
	body.raw(buildDinf())
	body.raw(buildStbl(sampleCount, sampleSize, dataOffset, delta))

	return makeBox("minf", body.buf)
}

// buildDinf returns the data information box for self contained media.
func buildDinf() []byte {
	var url box

	url.fullBox(0x000001)

	var dref box

	dref.fullBox(0)
	dref.u32(1)
	dref.child("url ", url.buf)

	var body box

	body.child("dref", dref.buf)

	return makeBox("dinf", body.buf)
}

// buildStbl returns the sample table box.
func buildStbl(sampleCount int, sampleSize int, dataOffset uint64, delta uint32) []byte {
	var body box

	body.raw(buildStsd())
	body.raw(buildStts(sampleCount, delta))
	body.raw(buildStsc(sampleCount))
	body.raw(buildStsz(sampleCount, sampleSize))
	body.raw(buildChunkOffset(dataOffset))

	return makeBox("stbl", body.buf)
}

// buildStsd returns the sample description box with one metadata entry.
func buildStsd() []byte {
	var entry box

	entry.zero(6) // reserved
	entry.u16(1)  // data reference index

	var body box

	body.fullBox(0)
	body.u32(1)
	body.child(sampleFormat, entry.buf)

	return makeBox("stsd", body.buf)
}

// buildStts returns the time to sample box, one entry for a constant rate.
func buildStts(sampleCount int, delta uint32) []byte {
	var body box

	body.fullBox(0)
	body.u32(1)
	body.u32(uint32(sampleCount)) //nolint:gosec // bounded by checkTrackLimits
	body.u32(delta)

	return makeBox("stts", body.buf)
}

// buildStsc returns the sample to chunk box, placing every sample in one chunk.
func buildStsc(sampleCount int) []byte {
	var body box

	body.fullBox(0)
	body.u32(1)
	body.u32(1)                   // first chunk
	body.u32(uint32(sampleCount)) //nolint:gosec // bounded by checkTrackLimits
	body.u32(1)                   // sample description index

	return makeBox("stsc", body.buf)
}

// buildStsz returns the sample size box for uniformly sized samples.
func buildStsz(sampleCount int, sampleSize int) []byte {
	var body box

	body.fullBox(0)
	body.u32(uint32(sampleSize))  //nolint:gosec // bounded by maxPacketSize
	body.u32(uint32(sampleCount)) //nolint:gosec // bounded by checkTrackLimits

	return makeBox("stsz", body.buf)
}

// buildChunkOffset returns stco, or co64 when the offset exceeds 32 bits.
func buildChunkOffset(dataOffset uint64) []byte {
	var body box

	body.fullBox(0)
	body.u32(1)

	if dataOffset > 0xFFFFFFFF {
		body.u64(dataOffset)

		return makeBox("co64", body.buf)
	}

	body.u32(uint32(dataOffset))

	return makeBox("stco", body.buf)
}
