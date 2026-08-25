package tuneassist

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// errNoAudio is returned by renderSectionWAV when the requested lap/section yields
// no frames, so the caller can map it to a 404 rather than a 500.
var errNoAudio = errors.New("no audio for requested lap/section")

const (
	// wavHeaderLen is the size of the RIFF/fmt/data header preceding the PCM payload.
	wavHeaderLen = 44

	// Sizing hints for the render buffer only — the real rates come from the capture
	// and the telemetry stream, and nothing but the initial allocation depends on
	// these being exact.
	internalRateHint       = 8000
	telemetryFrameRateHint = 60
)

// renderSectionWAV renders the chassis haptic stream for one lap section and returns
// it as a 16-bit PCM WAV. Samples are converted to PCM as they are produced and only
// the requested section is held in memory, so a long replay costs a section-sized
// buffer rather than a whole-replay float64 capture.
//
// The buffer opens with header-sized padding and the PCM is written straight after
// it, so the finished WAV is the buffer itself — the payload is never copied into a
// second full-size slice just to prepend 44 bytes.
func renderSectionWAV(ctx context.Context, source string, tuning haptics.Tuning, unfiltered bool, lap int16, fromFrame, toFrame int) ([]byte, error) {
	var wav bytes.Buffer

	wav.Grow(wavHeaderLen + estimatePCMLen(fromFrame, toFrame))
	wav.Write(make([]byte, wavHeaderLen))

	capture, err := haptics.CaptureChassis(ctx, haptics.CaptureOptions{
		Source:     source,
		Tuning:     tuning,
		Unfiltered: unfiltered,
		Window:     &haptics.CaptureWindow{Lap: lap, FromFrame: fromFrame, ToFrame: toFrame},
		Sink:       func(samples []float64) { encodePCM(&wav, samples) },
	})
	if err != nil {
		return nil, err
	}

	if wav.Len() == wavHeaderLen {
		return nil, errNoAudio
	}

	out := wav.Bytes()
	writeWAVHeader(out[:wavHeaderLen], len(out)-wavHeaderLen, capture.InternalRate)

	return out, nil
}

// estimatePCMLen sizes the render buffer up front so a long section does not walk up
// through a dozen doubling reallocations, each copying everything rendered so far. It
// is a hint only: an open-ended section (toFrame < 0) has no known length, and an
// over- or under-estimate costs nothing but the usual growth.
func estimatePCMLen(fromFrame, toFrame int) int {
	if toFrame < fromFrame {
		return 0
	}

	const bytesPerFrame = (internalRateHint / telemetryFrameRateHint) * 2

	return (toFrame - fromFrame + 1) * bytesPerFrame
}

// encodePCM appends samples (float, nominally [-1, 1]) to buf as 16-bit little-endian
// PCM, clamping out-of-range values.
func encodePCM(buf *bytes.Buffer, samples []float64) {
	var sampleBytes [2]byte

	for _, sample := range samples {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}

		pcm := int16(math.Round(sample * math.MaxInt16))
		binary.LittleEndian.PutUint16(sampleBytes[:], uint16(pcm)) //nolint:gosec // reinterpreting the int16 bit pattern is the PCM encoding
		buf.Write(sampleBytes[:])
	}
}

// writeWAVHeader fills buf (exactly wavHeaderLen bytes, sitting in front of the PCM
// payload it describes) with the RIFF/WAVE header for dataLen bytes of 16-bit mono
// PCM at sampleRate.
func writeWAVHeader(buf []byte, dataLen, sampleRate int) {
	const (
		bitsPerSample = 16
		numChannels   = 1
	)

	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8

	copy(buf[0:4], "RIFF")
	//nolint:gosec // dataLen is one rendered section, far below 4 GiB.
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataLen))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(buf[22:24], numChannels)
	//nolint:gosec // sampleRate is an audio rate, always well below 2^32.
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	//nolint:gosec // byteRate derives from sampleRate, always well below 2^32.
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], bitsPerSample)
	copy(buf[36:40], "data")
	//nolint:gosec // dataLen is one rendered section, far below 4 GiB.
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataLen))
}

// parseIntParam returns the integer query value, or def when absent/blank/invalid.
// parseFloatParam reads a float query parameter, falling back to def when absent
// or unparseable.
func parseFloatParam(req *http.Request, name string, def float64) float64 {
	raw := req.URL.Query().Get(name)
	if raw == "" {
		return def
	}

	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}

	return v
}

func parseIntParam(req *http.Request, name string, def int) int {
	raw := req.URL.Query().Get(name)
	if raw == "" {
		return def
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}

	return v
}

// clampToInt16 bounds an untrusted query value (a client-supplied lap number) into
// int16's range, since a raw conversion would otherwise wrap silently.
func clampToInt16(lap int) int16 {
	switch {
	case lap > math.MaxInt16:
		return math.MaxInt16
	case lap < math.MinInt16:
		return math.MinInt16
	default:
		return int16(lap)
	}
}
