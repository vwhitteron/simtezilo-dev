package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/hajimehoshi/go-mp3"
)

// mp3Channels is the channel count go-mp3 always decodes to (stereo).
const mp3Channels = 2

type MP3 struct {
	data []byte
}

func NewMP3(data []byte) *MP3 {
	return &MP3{data: data}
}

// ToDCA converts MP3 audio data to Discord audio format (DCA).
func (m *MP3) ToDCA() ([]byte, error) {
	pcmInt16, err := m.ToPCMInt16()
	if err != nil {
		return []byte{}, fmt.Errorf("convert MP3 to int16: %w", err)
	}

	dcaData, err := pcmInt16.ToDCA()
	if err != nil {
		return []byte{}, fmt.Errorf("MP3 -> int16 -> DCA: %w", err)
	}

	return dcaData, nil
}

// ToPCMInt16 decodes MP3 data to PCM int16 format.
func (m *MP3) ToPCMInt16() (PCMInt16, error) {
	// go-mp3 always decodes to 16-bit little-endian stereo PCM.
	decoder, err := mp3.NewDecoder(bytes.NewReader(m.data))
	if err != nil {
		return PCMInt16{}, fmt.Errorf("failed to decode MP3: %w", err)
	}

	raw, err := io.ReadAll(decoder)
	if err != nil {
		return PCMInt16{}, fmt.Errorf("failed to read MP3 samples: %w", err)
	}

	pcmInt16 := PCMInt16{
		samples:    make([]int16, len(raw)/2),
		sampleRate: decoder.SampleRate(),
		channels:   mp3Channels,
	}

	// Each frame is a pair of little-endian int16 samples (L, R) already in
	// interleaved order, which is the layout this type expects.
	for i := range pcmInt16.samples {
		// #nosec G115: reinterpreting the 16-bit PCM word as signed is intentional, not an overflow
		pcmInt16.samples[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
	}

	return pcmInt16, nil
}

// ToPCMFloat64 decodes MP3 data to PCM float64 format.
func (m *MP3) ToPCMFloat64() (PCMFloat64, error) {
	// go-mp3 always decodes to 16-bit little-endian stereo PCM.
	decoder, err := mp3.NewDecoder(bytes.NewReader(m.data))
	if err != nil {
		return PCMFloat64{}, fmt.Errorf("failed to decode MP3: %w", err)
	}

	raw, err := io.ReadAll(decoder)
	if err != nil {
		return PCMFloat64{}, fmt.Errorf("failed to read MP3 samples: %w", err)
	}

	pcmFloat64 := PCMFloat64{
		samples:    make([]float64, len(raw)/2),
		sampleRate: decoder.SampleRate(),
		channels:   mp3Channels,
	}

	// Normalize each interleaved int16 sample to the [-1, 1) float64 range.
	for i := range pcmFloat64.samples {
		// #nosec G115: reinterpreting the 16-bit PCM word as signed is intentional, not an overflow
		sample := int16(binary.LittleEndian.Uint16(raw[i*2:]))
		pcmFloat64.samples[i] = float64(sample) / 32768.0
	}

	return pcmFloat64, nil
}
