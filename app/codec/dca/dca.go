// Package dca encodes PCM audio to the Discord audio format (DCA / framed Opus).
// It is split out of app/codec because it depends on the CGO Opus library
// (layeh.com/gopus); keeping it in its own package lets consumers that only need the
// pure-Go PCM types (app/codec) — such as the synthesizer and the offline haptic
// tooling — build without a C toolchain.
package dca

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/vwhitteron/simtezilo-dev/app/codec"
	"layeh.com/gopus"
)

// FromInt16 encodes PCM int16 audio to DCA, resampling to the Discord rate and
// upmixing to stereo first.
func FromInt16(pcm codec.PCMInt16) ([]byte, error) {
	resampled := pcm.Resample(codec.OpusSampleRate)
	resampled = resampled.ToStereo()

	dcaData, err := encode(resampled)
	if err != nil {
		return nil, fmt.Errorf("int16 -> DCA: %w", err)
	}

	return dcaData, nil
}

// FromFloat64 encodes PCM float64 audio to DCA.
func FromFloat64(pcm codec.PCMFloat64) ([]byte, error) {
	dcaData, err := FromInt16(pcm.ToInt16())
	if err != nil {
		return nil, fmt.Errorf("float64 -> int16 -> DCA: %w", err)
	}

	return dcaData, nil
}

// FromMP3 decodes MP3 data and encodes it to DCA.
func FromMP3(mp3 *codec.MP3) ([]byte, error) {
	pcmInt16, err := mp3.ToPCMInt16()
	if err != nil {
		return nil, fmt.Errorf("convert MP3 to int16: %w", err)
	}

	dcaData, err := FromInt16(pcmInt16)
	if err != nil {
		return nil, fmt.Errorf("MP3 -> int16 -> DCA: %w", err)
	}

	return dcaData, nil
}

// encode encodes stereo 48kHz PCM int16 samples to DCA (length-prefixed Opus frames)
// using an Opus encoder.
func encode(pcm codec.PCMInt16) ([]byte, error) {
	var dcaBuffer bytes.Buffer

	frameSamples := codec.OpusFrameSize * codec.OpusChannels

	// Create stereo Opus encoder for Discord (48kHz, stereo)
	encoder, err := gopus.NewEncoder(codec.OpusSampleRate, codec.OpusChannels, gopus.Voip)
	if err != nil {
		return nil, fmt.Errorf("create Opus encoder: %w", err)
	}

	// Process audio in frames
	for index := 0; index < pcm.Len(); index += frameSamples {
		var frame []int16

		end := index + frameSamples

		if end > pcm.Len() {
			// Pad the last frame with zeros
			frame = make([]int16, frameSamples)
			copy(frame, pcm.Samples()[index:])
		} else {
			frame = pcm.Samples()[index:end]
		}

		// Encode the frame
		opusData, err := encoder.Encode(frame, codec.OpusFrameSize, codec.OpusMaxFrameSize)
		if err != nil {
			return nil, fmt.Errorf("encode Opus frame: %w", err)
		}

		if len(opusData) > 0 {
			// Write DCA format: length (2 bytes) + opus data
			frameLen := len(opusData)
			if frameLen <= 65535 { // max int16 value
				// #nosec G115: frameLen is checked to be within int16 range
				_ = binary.Write(&dcaBuffer, binary.LittleEndian, int16(frameLen))
				dcaBuffer.Write(opusData)
			}
		}
	}

	return dcaBuffer.Bytes(), nil
}
