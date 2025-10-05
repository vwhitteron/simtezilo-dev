package pitradio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"layeh.com/gopus"
)

func transcodeMP3toDCA(mp3Data []byte) ([]byte, error) {
	pcmData, err := decodeMP3ToPCM(mp3Data)
	if err != nil {
		return []byte{}, fmt.Errorf("convert MP3 to PCM: %w", err)
	}

	dcaData := encodePCMToDCA(pcmData)
	if len(dcaData) == 0 {
		return []byte{}, errors.New("failed to encode audio to DCA format")
	}

	return dcaData, nil
}

func decodeMP3ToPCM(mp3Data []byte) ([]byte, error) {
	// Create a ReadCloser from the byte slice
	reader := io.NopCloser(bytes.NewReader(mp3Data))

	// Decode the MP3 data
	streamer, format, err := mp3.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MP3: %w", err)
	}
	defer streamer.Close()

	// Resample to 48kHz if needed (Discord requirement)
	var finalStreamer beep.Streamer = streamer
	if format.SampleRate != discordOpusSampleRate {
		finalStreamer = newResampleStreamer(streamer, format.SampleRate, beep.SampleRate(discordOpusSampleRate))
	}

	// Ensure stereo format (Discord requirement)
	if format.NumChannels == 1 {
		// Convert mono to stereo by duplicating the channel
		finalStreamer = &monoToStereoStreamer{streamer: finalStreamer}
	}

	// Create a buffer to store PCM samples
	var pcmBuffer bytes.Buffer

	// Read all samples and convert to PCM bytes
	samples := make([][2]float64, 512) // Buffer for stereo samples
	for {
		sampleCount, ok := finalStreamer.Stream(samples)
		if !ok {
			break
		}

		// Convert float64 samples to 16-bit PCM
		for index := range sampleCount {
			// Convert left channel
			left := int16(samples[index][0] * 32767)

			err := binary.Write(&pcmBuffer, binary.LittleEndian, left)
			if err != nil {
				return nil, fmt.Errorf("failed to write left channel: %w", err)
			}

			// Convert right channel
			right := int16(samples[index][1] * 32767)

			err = binary.Write(&pcmBuffer, binary.LittleEndian, right)
			if err != nil {
				return nil, fmt.Errorf("failed to write right channel: %w", err)
			}
		}
	}

	return pcmBuffer.Bytes(), nil
}

func encodePCMToDCA(s16le []byte) []byte {
	// Convert raw S16LE PCM data to DCA format for Discord
	// The PCM file is already 48kHz stereo S16LE (created with: ffmpeg -i input.opus -f s16le -ar 48000 -ac 2 output.pcm)
	samples := convertToSamples(s16le)

	// The data should already be stereo at 48kHz, so use as-is
	finalSamples := samples

	// Create stereo Opus encoder for Discord (48kHz, stereo)
	encoder, err := gopus.NewEncoder(discordOpusSampleRate, discordOpusChannels, gopus.Voip)
	if err != nil {
		return []byte{}
	}

	encoder.SetBitrate(64000) // 64kbps for stereo

	return encodeFramesToDCA(encoder, finalSamples, 2) // Stereo for Discord
}

func convertToSamples(audioData []byte) []int16 {
	// Convert raw S16LE bytes to []int16 samples
	samples := make([]int16, len(audioData)/2)
	for i := 0; i < len(samples); i++ {
		// Convert S16LE bytes to int16 samples
		val := binary.LittleEndian.Uint16(audioData[i*2 : i*2+2])
		// #nosec G115: This conversion is safe for audio data
		samples[i] = int16(val)
	}

	return samples
}

func encodeFramesToDCA(encoder *gopus.Encoder, samples []int16, channels int) []byte {
	var dcaBuffer bytes.Buffer

	frameSamples := discordOpusFrameSize * channels

	// Process audio in frames
	for index := 0; index < len(samples); index += frameSamples {
		var frame []int16

		end := index + frameSamples

		if end > len(samples) {
			// Pad the last frame with zeros
			frame = make([]int16, frameSamples)
			copy(frame, samples[index:])
		} else {
			frame = samples[index:end]
		}

		// Encode the frame
		opusData, err := encoder.Encode(frame, discordOpusFrameSize, discordOpusMaxFrameSize)
		if err == nil && len(opusData) > 0 {
			// Write DCA format: length (2 bytes) + opus data
			frameLen := len(opusData)
			if frameLen <= 65535 { // max int16 value
				// #nosec G115: frameLen is checked to be within int16 range
				_ = binary.Write(&dcaBuffer, binary.LittleEndian, int16(frameLen))
				dcaBuffer.Write(opusData)
			}
		}
	}

	return dcaBuffer.Bytes()
}

// monoToStereoStreamer converts mono audio to stereo by duplicating the channel.
type monoToStereoStreamer struct {
	streamer beep.Streamer
}

func (m *monoToStereoStreamer) Stream(samples [][2]float64) (sampleCount int, ok bool) {
	// Create a temporary buffer for mono samples
	monoSamples := make([][2]float64, len(samples))
	sampleCount, ok = m.streamer.Stream(monoSamples)

	// Convert mono to stereo by duplicating the left channel to right
	for i := range sampleCount {
		samples[i][0] = monoSamples[i][0] // Left channel
		samples[i][1] = monoSamples[i][0] // Right channel (duplicate)
	}

	return sampleCount, ok
}

func (m *monoToStereoStreamer) Err() error {
	return m.streamer.Err()
}

// resampleStreamer performs simple linear interpolation resampling.
type resampleStreamer struct {
	streamer   beep.Streamer
	ratio      float64
	buffer     [][2]float64
	bufferPos  float64
	bufferFill int
}

func newResampleStreamer(streamer beep.Streamer, oldRate, newRate beep.SampleRate) *resampleStreamer {
	return &resampleStreamer{
		streamer: streamer,
		ratio:    float64(oldRate) / float64(newRate),
		buffer:   make([][2]float64, 1024),
	}
}

func (r *resampleStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	for index := range samples {
		// Check if we need more data in buffer
		for int(r.bufferPos)+1 >= r.bufferFill {
			var bufOk bool

			r.bufferFill, bufOk = r.streamer.Stream(r.buffer)
			if !bufOk {
				return index, index > 0
			}

			r.bufferPos = 0
		}

		// Linear interpolation
		pos := int(r.bufferPos)
		frac := r.bufferPos - float64(pos)

		if pos+1 < r.bufferFill {
			// Interpolate left channel
			samples[index][0] = r.buffer[pos][0]*(1-frac) + r.buffer[pos+1][0]*frac
			// Interpolate right channel
			samples[index][1] = r.buffer[pos][1]*(1-frac) + r.buffer[pos+1][1]*frac
		} else {
			// Use last sample if at end
			samples[index] = r.buffer[pos]
		}

		r.bufferPos += r.ratio
		n++
	}

	return n, true
}

func (r *resampleStreamer) Err() error {
	return r.streamer.Err()
}
