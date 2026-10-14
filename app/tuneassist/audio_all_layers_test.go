package tuneassist //nolint:testpackage // reaches the unexported render path

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// TestRenderAllLayersWAVMatchesSingleLayers checks that each channel of the one-pass
// render is bit-identical to the single-layer render of that layer.
func TestRenderAllLayersWAVMatchesSingleLayers(t *testing.T) {
	t.Parallel()

	// Arrange
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	replay := filepath.Join(root, "data", "replays",
		"20260801.111955-circuit-de-spa-francorchamps-toyota-supra-rz-97.gtz")

	_, err := os.Stat(replay)
	if err != nil {
		t.Skipf("replay not present: %v", err)
	}

	source := "file://" + replay
	ctx := t.Context()

	windows := map[string]haptics.CaptureWindow{
		"lap section": {Lap: 2, FromFrame: 600, ToFrame: 1800},
		"all laps":    {AllLaps: true, FromFrame: 3000, ToFrame: 4200},
	}

	for name, window := range windows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Act
			all, err := renderAllLayersWAV(ctx, source, haptics.Tuning{}, false, window)
			require.NoError(t, err)

			// Assert
			assert.Equal(t, len(allLayerNames), int(binary.LittleEndian.Uint16(all[22:24])))

			for channel, layer := range allLayerNames {
				layers, known := captureLayers(layer)
				require.True(t, known)

				single, err := renderWindowWAV(ctx, source, haptics.Tuning{}, layers, false, window)
				require.NoError(t, err)

				got := deinterleaveChannel(all[wavHeaderLen:], len(allLayerNames), channel)
				assert.Equal(t, single[wavHeaderLen:], got, "layer %s differs from its single-layer render", layer)
				assert.Equal(t, single[24:28], all[24:28], "layer %s sample rate", layer)
			}
		})
	}
}

// TestInterleavePCM checks the frame order and the silence padding of a short channel.
func TestInterleavePCM(t *testing.T) {
	t.Parallel()

	// Arrange
	channels := [][]byte{
		{0x01, 0x02, 0x03, 0x04},
		{0x05, 0x06},
	}

	// Act
	out := interleavePCM(channels)

	// Assert
	assert.Equal(t, []byte{0x01, 0x02, 0x05, 0x06, 0x03, 0x04, 0x00, 0x00}, out[wavHeaderLen:])
}

// deinterleaveChannel extracts one 16-bit channel from interleaved PCM.
func deinterleaveChannel(pcm []byte, channels, channel int) []byte {
	const sampleLen = 2

	frameLen := channels * sampleLen
	out := make([]byte, 0, len(pcm)/channels)

	for at := channel * sampleLen; at+sampleLen <= len(pcm); at += frameLen {
		out = append(out, pcm[at:at+sampleLen]...)
	}

	return out
}
