package st7789

import "image"

// PackRGB565 exposes the unexported packRGB565 packing routine to the external
// st7789_test package so the golden/buffer-reuse tests can exercise it without
// living in the production package.
func PackRGB565(src *image.RGBA, cols, rows int, buf []byte) []byte {
	return packRGB565(src, cols, rows, buf)
}
