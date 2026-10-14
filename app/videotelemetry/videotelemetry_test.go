package videotelemetry_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/videotelemetry"
)

// fixtureRelPath is the repo-relative location of the real, untracked demo
// capture used by the fixture-backed tests below.
const fixtureRelPath = "tools/replay_embed/simtezilo-demo-2-telemetry.mp4"

// fixturePath resolves the real fixture from VIDEOTELEMETRY_MP4 or, failing
// that, the known repo-relative path, mirroring the HAPTICCAPTURE_REPLAY /
// GEARSHIFT_PROBE_REPLAY pattern used elsewhere in this repo.
func fixturePath(t *testing.T) string {
	t.Helper()

	if path := os.Getenv("VIDEOTELEMETRY_MP4"); path != "" {
		return path
	}

	path, err := repoRelative(fixtureRelPath)
	if err == nil {
		_, statErr := os.Stat(path)
		if statErr == nil {
			return path
		}
	}

	t.Skip("set VIDEOTELEMETRY_MP4 to a telemetry mp4, or place it at " + fixtureRelPath)

	return ""
}

// repoRelative resolves a path relative to the repository root by walking up
// from the working directory until go.mod is found.
func repoRelative(rel string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil {
			return filepath.Join(dir, rel), nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}

		dir = parent
	}
}

// TestOpenFixture exercises the parser against the real demo capture. It is
// skipped unless the fixture is available; see fixturePath.
func TestOpenFixture(t *testing.T) {
	t.Parallel()

	path := fixturePath(t)

	index, err := videotelemetry.Open(path)
	require.NoError(t, err)

	defer index.Close()

	assert.Equal(t, 7704, index.FrameCount())
	assert.Equal(t, uint32(60000), index.Timescale())
	assert.Equal(t, uint32(1001), index.SampleDelta())
	assert.InDelta(t, 128.5284, index.Duration().Seconds(), 1001.0/60000.0)
	assert.Equal(t, uint32(85908), index.FirstSequenceID())

	last, err := index.Sample(index.FrameCount() - 1)
	require.NoError(t, err)

	lastSeq, ok := videotelemetry.SequenceID(last)
	require.True(t, ok)
	assert.Equal(t, uint32(93611), lastSeq)

	prevSeq := index.FirstSequenceID() - 1

	frameCount := index.FrameCount()
	for sampleIdx := range frameCount {
		sample, err := index.Sample(sampleIdx)
		require.NoError(t, err)
		require.Len(t, sample, 368)
		require.True(t, bytes.HasPrefix(sample, []byte("0S7G")))

		seq, ok := videotelemetry.SequenceID(sample)
		require.True(t, ok)
		assert.Equal(t, prevSeq+1, seq, "sample %d not contiguous with previous", sampleIdx)

		prevSeq = seq
	}
}

// TestOpenFixtureConcurrentSamples checks Sample is safe to call from
// multiple goroutines against the real fixture.
func TestOpenFixtureConcurrentSamples(t *testing.T) {
	t.Parallel()

	path := fixturePath(t)

	index, err := videotelemetry.Open(path)
	require.NoError(t, err)

	defer index.Close()

	const workers = 8

	done := make(chan error, workers)

	for worker := range workers {
		go func(offset int) {
			for n := offset; n < index.FrameCount(); n += workers {
				_, sampleErr := index.Sample(n)
				if sampleErr != nil {
					done <- sampleErr

					return
				}
			}

			done <- nil
		}(worker)
	}

	for range workers {
		require.NoError(t, <-done)
	}
}

// TestOpenSyntheticTrack builds a minimal MP4 in a tempdir exercising the
// parser's less common paths: multi-entry stsc, a per-sample stsz table,
// co64 chunk offsets and a 64 bit largesize box. It does not depend on the
// large real fixture, so it always runs.
func TestOpenSyntheticTrack(t *testing.T) {
	t.Parallel()

	packets := [][]byte{
		makePacket(100),
		makePacket(101),
		makePacket(102),
		makePacket(103),
		makePacket(104),
	}

	path := filepath.Join(t.TempDir(), "synthetic.mp4")
	require.NoError(t, os.WriteFile(path, buildSyntheticMP4(t, packets), 0o600))

	index, err := videotelemetry.Open(path)
	require.NoError(t, err)

	defer index.Close()

	assert.Equal(t, len(packets), index.FrameCount())
	assert.Equal(t, uint32(60000), index.Timescale())
	assert.Equal(t, uint32(1001), index.SampleDelta())
	assert.Equal(t, uint32(100), index.FirstSequenceID())

	for n, want := range packets {
		got, sampleErr := index.Sample(n)
		require.NoError(t, sampleErr)
		assert.Equal(t, want, got)
	}

	var buf bytes.Buffer

	require.NoError(t, index.WriteGTR(&buf))
	assert.Equal(t, bytes.Join(packets, nil), buf.Bytes())
}

// TestOpenNoTelemetryTrack checks a file with no meta/gpmd track returns a
// descriptive error rather than a panic or a silently empty index.
// TestOpenRejectsVariableRateTrack pins the constant rate guarantee. Callers map a
// sample index to a presentation time with a single delta, so a track whose stts
// runs disagree must fail loudly rather than be silently mistimed.
func TestOpenRejectsVariableRateTrack(t *testing.T) {
	t.Parallel()

	// Arrange
	packets := [][]byte{makePacket(1), makePacket(2), makePacket(3), makePacket(4), makePacket(5)}

	cases := map[string]struct {
		runs      [][2]uint32
		wantError bool
	}{
		"split runs sharing a delta": {runs: [][2]uint32{{2, 1001}, {3, 1001}}, wantError: false},
		"runs with differing deltas": {runs: [][2]uint32{{2, 1001}, {3, 1000}}, wantError: true},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "synthetic.mp4")
			require.NoError(t, os.WriteFile(path, buildSyntheticMP4STTS(t, packets, testCase.runs), 0o600))

			// Act
			index, err := videotelemetry.Open(path)

			// Assert
			if testCase.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not constant rate")

				return
			}

			require.NoError(t, err)

			defer index.Close()

			assert.Equal(t, 5, index.FrameCount())
			assert.Equal(t, uint32(1001), index.SampleDelta())
		})
	}
}

func TestOpenNoTelemetryTrack(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "no-telemetry.mp4")
	require.NoError(t, os.WriteFile(path, buildMP4WithoutTelemetry(t), 0o600))

	_, err := videotelemetry.Open(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "meta/gpmd")
}

// TestSequenceID checks the standalone packet parsing helper.
func TestSequenceID(t *testing.T) {
	t.Parallel()

	seq, valid := videotelemetry.SequenceID(makePacket(42))
	assert.True(t, valid)
	assert.Equal(t, uint32(42), seq)

	_, valid = videotelemetry.SequenceID([]byte("too short"))
	assert.False(t, valid)

	badMagic := makePacket(1)
	badMagic[0] = 'X'
	_, valid = videotelemetry.SequenceID(badMagic)
	assert.False(t, valid)
}

// TestSampleOutOfRange checks Sample rejects indices outside the track.
func TestSampleOutOfRange(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "synthetic.mp4")
	require.NoError(t, os.WriteFile(path, buildSyntheticMP4(t, [][]byte{makePacket(1)}), 0o600))

	index, err := videotelemetry.Open(path)
	require.NoError(t, err)

	defer index.Close()

	_, err = index.Sample(-1)
	require.Error(t, err)

	_, err = index.Sample(index.FrameCount())
	require.Error(t, err)
}

// makePacket builds a minimal, valid deciphered GT7 packet carrying the given
// sequence ID, long enough for SequenceIDOffset+4 plus a little headroom.
func makePacket(seq uint32) []byte {
	const packetSize = videotelemetry.SequenceIDOffset + 4 + 16

	packet := make([]byte, packetSize)
	copy(packet, videotelemetry.PacketMagic)
	binary.LittleEndian.PutUint32(packet[videotelemetry.SequenceIDOffset:], seq)

	return packet
}

// --- minimal MP4 box builders, mirroring tools/replay_embed/mp4.go's writer
// but kept private to this test so the parser is exercised end to end
// without exporting internals from either package. ---

// synthBox accumulates a box body before its header is prepended.
type synthBox struct {
	buf []byte
}

func (b *synthBox) u16(v uint16)                   { b.buf = binary.BigEndian.AppendUint16(b.buf, v) }
func (b *synthBox) u32(v uint32)                   { b.buf = binary.BigEndian.AppendUint32(b.buf, v) }
func (b *synthBox) u64(v uint64)                   { b.buf = binary.BigEndian.AppendUint64(b.buf, v) }
func (b *synthBox) raw(v []byte)                   { b.buf = append(b.buf, v...) }
func (b *synthBox) str(v string)                   { b.buf = append(b.buf, v...) }
func (b *synthBox) zero(count int)                 { b.buf = append(b.buf, make([]byte, count)...) }
func (b *synthBox) fullBox()                       { b.u32(0) }
func (b *synthBox) child(name string, body []byte) { b.raw(makeSynthBox(name, body)) }

// makeSynthBox prepends a plain 32 bit size and type header.
func makeSynthBox(name string, body []byte) []byte {
	size := uint32(8 + len(body)) //nolint:gosec // test fixture, small values

	out := make([]byte, 0, 8+len(body))
	out = binary.BigEndian.AppendUint32(out, size)
	out = append(out, name...)

	return append(out, body...)
}

// makeSynthLargeBox prepends a 64 bit largesize header (size field 1),
// exercising the parser's largesize path.
func makeSynthLargeBox(name string, body []byte) []byte {
	size := uint64(16 + len(body)) //nolint:gosec // test fixture, small values

	out := make([]byte, 0, 16+len(body))
	out = binary.BigEndian.AppendUint32(out, 1)
	out = append(out, name...)
	out = binary.BigEndian.AppendUint64(out, size)

	return append(out, body...)
}

func synthMatrix(b *synthBox) {
	for _, v := range [9]uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000} {
		b.u32(v)
	}
}

// buildSyntheticMP4 assembles a telemetry-only MP4 whose sample table uses a
// multi-entry stsc (two chunks: one sample, then the rest), a per-sample
// (non-uniform) stsz table, and co64 chunk offsets. The moov box itself is
// wrapped in the 64 bit largesize form.
func buildSyntheticMP4(t *testing.T, packets [][]byte) []byte {
	t.Helper()

	return buildSyntheticMP4STTS(t, packets, nil)
}

// buildSyntheticMP4STTS is buildSyntheticMP4 with control over the stts runs, so a
// test can produce the variable rate track that Open is required to reject. A nil
// sttsRuns yields the single constant rate run a real file has.
func buildSyntheticMP4STTS(t *testing.T, packets [][]byte, sttsRuns [][2]uint32) []byte {
	t.Helper()

	require.NotEmpty(t, packets)

	ftyp := makeSynthBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41"))

	mdatBody := bytes.Join(packets, nil)
	mdat := makeSynthBox("mdat", mdatBody)

	dataOffset := uint64(len(ftyp) + 8) //nolint:gosec // test fixture, small values

	moovBody := buildSynthMoov(t, packets, dataOffset, sttsRuns)
	moov := makeSynthLargeBox("moov", moovBody)

	out := make([]byte, 0, len(ftyp)+len(mdat)+len(moov))

	out = append(out, ftyp...)
	out = append(out, mdat...)
	out = append(out, moov...)

	return out
}

// buildSynthMoov builds a moov box holding a single meta/gpmd trak.
func buildSynthMoov(t *testing.T, packets [][]byte, dataOffset uint64, sttsRuns [][2]uint32) []byte {
	t.Helper()

	const (
		timescale = 60000
		delta     = 1001
	)

	duration := uint32(len(packets)) * delta //nolint:gosec // test fixture, small values

	var mvhd synthBox

	mvhd.fullBox()
	mvhd.zero(8) // creation, modification
	mvhd.u32(timescale)
	mvhd.u32(duration)
	mvhd.u32(0x00010000)
	mvhd.u16(0)
	mvhd.zero(10)
	synthMatrix(&mvhd)
	mvhd.zero(24)
	mvhd.u32(2)

	trak := buildSynthTrak(t, packets, dataOffset, timescale, delta, duration, sttsRuns)

	var moov synthBox

	moov.child("mvhd", mvhd.buf)
	moov.raw(trak)

	return moov.buf
}

// buildSynthTrak builds a single meta/gpmd trak with a deliberately awkward
// sample table: chunk 1 holds one sample and every later chunk holds two
// (five packets across three chunks), sample sizes are given per sample
// rather than uniformly, and chunk offsets use co64.
func buildSynthTrak(t *testing.T, packets [][]byte, dataOffset uint64, timescale, delta, duration uint32, sttsRuns [][2]uint32) []byte {
	t.Helper()

	var tkhd synthBox

	tkhd.fullBox()
	tkhd.zero(4) // creation
	tkhd.zero(4) // modification
	tkhd.u32(1)  // track ID
	tkhd.zero(4) // reserved
	tkhd.u32(duration)
	tkhd.zero(8)
	tkhd.u16(0)
	tkhd.u16(0)
	tkhd.u16(0)
	tkhd.u16(0)
	synthMatrix(&tkhd)
	tkhd.u32(0)
	tkhd.u32(0)

	var mdhd synthBox

	mdhd.fullBox()
	mdhd.zero(4) // creation
	mdhd.zero(4) // modification
	mdhd.u32(timescale)
	mdhd.u32(duration)
	mdhd.u16(0x55C4)
	mdhd.u16(0)

	var hdlr synthBox

	hdlr.fullBox()
	hdlr.u32(0) // predefined
	hdlr.str("meta")
	hdlr.zero(12)
	hdlr.str("GT Telemetry")
	hdlr.zero(1)

	var stsd synthBox

	var stsdEntry synthBox

	stsdEntry.zero(6)
	stsdEntry.u16(1)

	stsd.fullBox()
	stsd.u32(1)
	stsd.child("gpmd", stsdEntry.buf)

	if sttsRuns == nil {
		sttsRuns = [][2]uint32{{uint32(len(packets)), delta}} //nolint:gosec // test fixture, small values
	}

	var stts synthBox

	stts.fullBox()
	stts.u32(uint32(len(sttsRuns))) //nolint:gosec // test fixture, small values

	for _, run := range sttsRuns {
		stts.u32(run[0])
		stts.u32(run[1])
	}

	// Two chunk runs: chunk 1 holds one sample, every later chunk holds two.
	var stsc synthBox

	stsc.fullBox()
	stsc.u32(2)
	stsc.u32(1)
	stsc.u32(1)
	stsc.u32(1)
	stsc.u32(2)
	stsc.u32(2)
	stsc.u32(1)

	var stsz synthBox

	stsz.fullBox()
	stsz.u32(0)                    // per-sample sizes follow
	stsz.u32(uint32(len(packets))) //nolint:gosec // test fixture, small values

	for _, p := range packets {
		stsz.u32(uint32(len(p))) //nolint:gosec // test fixture, small values
	}

	// Split the packets into chunks matching the stsc plan above: chunk 1
	// holds packet 0, every later chunk holds two packets.
	chunkOffsets := synthChunkOffsets(packets, dataOffset)

	var co64 synthBox

	co64.fullBox()
	co64.u32(uint32(len(chunkOffsets))) //nolint:gosec // test fixture, small values

	for _, off := range chunkOffsets {
		co64.u64(off)
	}

	var stbl synthBox

	stbl.child("stsd", stsd.buf)
	stbl.child("stts", stts.buf)
	stbl.child("stsc", stsc.buf)
	stbl.child("stsz", stsz.buf)
	stbl.child("co64", co64.buf)

	var nmhd synthBox

	nmhd.fullBox()

	var url synthBox

	url.fullBox()

	var dref synthBox

	dref.fullBox()
	dref.u32(1)
	dref.child("url ", url.buf)

	var dinf synthBox

	dinf.child("dref", dref.buf)

	var minf synthBox

	minf.child("nmhd", nmhd.buf)
	minf.raw(makeSynthBox("dinf", dinf.buf))
	minf.raw(makeSynthBox("stbl", stbl.buf))

	var mdia synthBox

	mdia.raw(makeSynthBox("mdhd", mdhd.buf))
	mdia.raw(makeSynthBox("hdlr", hdlr.buf))
	mdia.raw(makeSynthBox("minf", minf.buf))

	var trak synthBox

	trak.raw(makeSynthBox("tkhd", tkhd.buf))
	trak.raw(makeSynthBox("mdia", mdia.buf))

	return makeSynthBox("trak", trak.buf)
}

// synthChunkOffsets returns one offset per chunk under the "chunk 1 has one
// sample, every later chunk has two" plan used by buildSynthTrak.
func synthChunkOffsets(packets [][]byte, dataOffset uint64) []uint64 {
	var offsets []uint64

	offset := dataOffset
	packetIdx := 0

	for packetIdx < len(packets) {
		offsets = append(offsets, offset)

		spc := 1
		if packetIdx > 0 {
			spc = 2
		}

		for c := 0; c < spc && packetIdx < len(packets); c++ {
			offset += uint64(len(packets[packetIdx]))
			packetIdx++
		}
	}

	return offsets
}

// buildMP4WithoutTelemetry returns a minimal MP4 with a single audio-ish
// trak, matching neither the meta handler nor the gpmd format.
func buildMP4WithoutTelemetry(t *testing.T) []byte {
	t.Helper()

	ftyp := makeSynthBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41"))

	var mvhd synthBox

	mvhd.fullBox()
	mvhd.zero(8)
	mvhd.u32(60000)
	mvhd.u32(0)
	mvhd.u32(0x00010000)
	mvhd.u16(0)
	mvhd.zero(10)
	synthMatrix(&mvhd)
	mvhd.zero(24)
	mvhd.u32(2)

	var tkhd synthBox

	tkhd.fullBox()
	tkhd.zero(4)
	tkhd.zero(4)
	tkhd.u32(1)
	tkhd.zero(4)
	tkhd.u32(0)
	tkhd.zero(8)
	tkhd.u16(0)
	tkhd.u16(0)
	tkhd.u16(0)
	tkhd.u16(0)
	synthMatrix(&tkhd)
	tkhd.u32(0)
	tkhd.u32(0)

	var mdhd synthBox

	mdhd.fullBox()
	mdhd.zero(4)
	mdhd.zero(4)
	mdhd.u32(60000)
	mdhd.u32(0)
	mdhd.u16(0x55C4)
	mdhd.u16(0)

	var hdlr synthBox

	hdlr.fullBox()
	hdlr.u32(0)
	hdlr.str("soun") // not "meta"
	hdlr.zero(12)
	hdlr.str("SoundHandler")
	hdlr.zero(1)

	var stsd synthBox

	var stsdEntry synthBox

	stsdEntry.zero(6)
	stsdEntry.u16(1)

	stsd.fullBox()
	stsd.u32(1)
	stsd.child("mp4a", stsdEntry.buf)

	var stts synthBox

	stts.fullBox()
	stts.u32(0)

	var stsc synthBox

	stsc.fullBox()
	stsc.u32(0)

	var stsz synthBox

	stsz.fullBox()
	stsz.u32(0)
	stsz.u32(0)

	var stco synthBox

	stco.fullBox()
	stco.u32(0)

	var stbl synthBox

	stbl.child("stsd", stsd.buf)
	stbl.child("stts", stts.buf)
	stbl.child("stsc", stsc.buf)
	stbl.child("stsz", stsz.buf)
	stbl.child("stco", stco.buf)

	var smhd synthBox

	smhd.u32(0)
	smhd.zero(4)

	var minf synthBox

	minf.child("smhd", smhd.buf)
	minf.raw(makeSynthBox("stbl", stbl.buf))

	var mdia synthBox

	mdia.raw(makeSynthBox("mdhd", mdhd.buf))
	mdia.raw(makeSynthBox("hdlr", hdlr.buf))
	mdia.raw(makeSynthBox("minf", minf.buf))

	var trak synthBox

	trak.raw(makeSynthBox("tkhd", tkhd.buf))
	trak.raw(makeSynthBox("mdia", mdia.buf))

	var moov synthBox

	moov.child("mvhd", mvhd.buf)
	moov.raw(makeSynthBox("trak", trak.buf))

	ftypAndMoov := append(append([]byte{}, ftyp...), makeSynthBox("moov", moov.buf)...)

	// No mdat needed since the (nonexistent) telemetry track is never read.
	return ftypAndMoov
}
