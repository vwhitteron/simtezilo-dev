package fancontroller

import (
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeImageFrames(t *testing.T) {
	t.Parallel()

	t.Run("single DATA frame when the image fits one chunk", func(t *testing.T) {
		t.Parallel()

		pixels := []byte{0x11, 0x22, 0x33, 0x44} // 2x1 RGB565
		frames, err := encodeImageFrames(pixels, 2, 1, 7, 244)
		require.NoError(t, err)

		require.Equal(t, [][]byte{
			{imgFrameBegin, 7, 2, 1, 0x04, 0x00},                  // total = 4 bytes (LE)
			{imgFrameData, 7, 0x00, 0x00, 0x11, 0x22, 0x33, 0x44}, // offset 0
			{imgFrameCommit, 7},
		}, frames)
	})

	t.Run("splits DATA on chunk boundaries with running offsets", func(t *testing.T) {
		t.Parallel()

		pixels := []byte{1, 2, 3, 4, 5, 6, 7, 8} // 2x2 RGB565, total 8 bytes
		frames, err := encodeImageFrames(pixels, 2, 2, 0, 3)
		require.NoError(t, err)

		require.Equal(t, [][]byte{
			{imgFrameBegin, 0, 2, 2, 0x08, 0x00},
			{imgFrameData, 0, 0x00, 0x00, 1, 2, 3},
			{imgFrameData, 0, 0x03, 0x00, 4, 5, 6},
			{imgFrameData, 0, 0x06, 0x00, 7, 8},
			{imgFrameCommit, 0},
		}, frames)
	})

	t.Run("rejects out-of-range dimensions", func(t *testing.T) {
		t.Parallel()

		_, err := encodeImageFrames(make([]byte, 2), 0, 1, 0, 244)
		require.Error(t, err)

		_, err = encodeImageFrames(make([]byte, (imageMaxWidth+1)*imageBytesPerPx), imageMaxWidth+1, 1, 0, 244)
		require.Error(t, err)
	})

	t.Run("rejects a pixel buffer that does not match the dimensions", func(t *testing.T) {
		t.Parallel()

		_, err := encodeImageFrames(make([]byte, 5), 2, 1, 0, 244)
		require.Error(t, err)
	})
}

func TestAlphaToRGB565LE(t *testing.T) {
	t.Parallel()

	mask := image.NewAlpha(image.Rect(0, 0, 2, 1))
	mask.SetAlpha(0, 0, color.Alpha{A: 255}) // foreground
	mask.SetAlpha(1, 0, color.Alpha{A: 0})   // background

	out := AlphaToRGB565LE(mask, color.White, color.Black)

	// White = 0xFFFF, black = 0x0000, both little-endian.
	require.Equal(t, []byte{0xFF, 0xFF, 0x00, 0x00}, out)
	require.Len(t, out, 2*1*imageBytesPerPx)
}
