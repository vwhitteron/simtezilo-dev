package pitradio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"layeh.com/gopus"
)

func mpegtoPCM(mpegData []byte) ([]byte, error) {
	// mpegdata is mpeg container format with a single mp3 audio stream

	// Create a ReadCloser from the byte slice
	reader := io.NopCloser(bytes.NewReader(mpegData))

	// Decode the MP3 data
	streamer, format, err := mp3.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode MP3: %w", err)
	}
	defer streamer.Close()

	// Resample to 48kHz if needed (Discord requirement)
	var finalStreamer beep.Streamer = streamer
	if format.SampleRate != discordSampleRate {
		finalStreamer = newResampleStreamer(streamer, format.SampleRate, beep.SampleRate(discordSampleRate))
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
		n, ok := finalStreamer.Stream(samples)
		if !ok {
			break
		}

		// Convert float64 samples to 16-bit PCM
		for i := 0; i < n; i++ {
			// Convert left channel
			left := int16(samples[i][0] * 32767)
			if err := binary.Write(&pcmBuffer, binary.LittleEndian, left); err != nil {
				return nil, fmt.Errorf("failed to write left channel: %w", err)
			}

			// Convert right channel
			right := int16(samples[i][1] * 32767)
			if err := binary.Write(&pcmBuffer, binary.LittleEndian, right); err != nil {
				return nil, fmt.Errorf("failed to write right channel: %w", err)
			}
		}
	}

	return pcmBuffer.Bytes(), nil
}

func encodeToDCA(s16le []byte) []byte {
	// Convert raw S16LE PCM data to DCA format for Discord
	// The PCM file is already 48kHz stereo S16LE (created with: ffmpeg -i input.opus -f s16le -ar 48000 -ac 2 output.pcm)
	samples := convertToSamples(s16le)

	// The data should already be stereo at 48kHz, so use as-is
	finalSamples := samples

	// Create stereo Opus encoder for Discord (48kHz, stereo)
	encoder, err := gopus.NewEncoder(discordSampleRate, discordChannels, gopus.Voip)
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

	frameSamples := discordFrameSize * channels

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
		opusData, err := encoder.Encode(frame, discordFrameSize, discordMaxFrameSize)
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

func monoToStereo(monoSamples []int16) []int16 {
	stereoSamples := make([]int16, len(monoSamples)*2)
	for i, sample := range monoSamples {
		stereoSamples[i*2] = sample   // Left channel
		stereoSamples[i*2+1] = sample // Right channel (duplicate)
	}

	return stereoSamples
}

func isProbablyMono(samples []int16) bool {
	// Simple heuristic: if most consecutive stereo pairs (L,R) are identical, it's probably mono data stored as stereo
	if len(samples) < 10 {
		return false
	}

	identicalPairs := 0
	totalPairs := len(samples) / 2

	// Check consecutive pairs: samples[0,1], samples[2,3], samples[4,5], etc.
	for i := 0; i < len(samples)-1; i += 2 {
		if samples[i] == samples[i+1] {
			identicalPairs++
		}
	}

	// If more than 80% of pairs are identical, treat as mono
	threshold := (totalPairs * 4) / 5

	return identicalPairs > threshold
}

// monoToStereoStreamer converts mono audio to stereo by duplicating the channel
type monoToStereoStreamer struct {
	streamer beep.Streamer
}

func (m *monoToStereoStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	// Create a temporary buffer for mono samples
	monoSamples := make([][2]float64, len(samples))
	n, ok = m.streamer.Stream(monoSamples)

	// Convert mono to stereo by duplicating the left channel to right
	for i := 0; i < n; i++ {
		samples[i][0] = monoSamples[i][0] // Left channel
		samples[i][1] = monoSamples[i][0] // Right channel (duplicate)
	}

	return n, ok
}

func (m *monoToStereoStreamer) Err() error {
	return m.streamer.Err()
}

// resampleStreamer performs simple linear interpolation resampling
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
	for i := range samples {
		// Check if we need more data in buffer
		for int(r.bufferPos)+1 >= r.bufferFill {
			var bufOk bool
			r.bufferFill, bufOk = r.streamer.Stream(r.buffer)
			if !bufOk {
				return i, i > 0
			}
			r.bufferPos = 0
		}

		// Linear interpolation
		pos := int(r.bufferPos)
		frac := r.bufferPos - float64(pos)

		if pos+1 < r.bufferFill {
			// Interpolate left channel
			samples[i][0] = r.buffer[pos][0]*(1-frac) + r.buffer[pos+1][0]*frac
			// Interpolate right channel
			samples[i][1] = r.buffer[pos][1]*(1-frac) + r.buffer[pos+1][1]*frac
		} else {
			// Use last sample if at end
			samples[i] = r.buffer[pos]
		}

		r.bufferPos += r.ratio
		n++
	}

	return n, true
}

func (r *resampleStreamer) Err() error {
	return r.streamer.Err()
}
