package fancontroller

import (
	"image"
	"image/color"
)

// AlphaToRGB565LE packs an alpha-coverage mask into the little-endian RGB565
// pixel buffer the Display Image characteristic expects. Each pixel is fg blended
// over bg by the mask's coverage (0 = bg, 255 = fg, in between = a smooth,
// anti-aliased edge), written low byte first, row-major from the mask's top-left.
// A hard 1-bit mask (only 0/255) therefore yields exactly fg/bg. The result is
// exactly width*height*2 bytes, ready for SetDisplayImage.
func AlphaToRGB565LE(mask *image.Alpha, fg, bg color.Color) []byte {
	bounds := mask.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	fgR, fgG, fgB := rgb8(fg)
	bgR, bgG, bgB := rgb8(bg)

	out := make([]byte, 0, width*height*imageBytesPerPx)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cov := uint32(mask.AlphaAt(x, y).A)
			pixel := packRGB565(lerp8(bgR, fgR, cov), lerp8(bgG, fgG, cov), lerp8(bgB, fgB, cov))

			out = append(out, byte(pixel), byte(pixel>>8))
		}
	}

	return out
}

// rgb8 returns a colour's 8-bit red, green and blue components.
func rgb8(c color.Color) (uint32, uint32, uint32) {
	r, g, b, _ := c.RGBA() // 16-bit, alpha-premultiplied

	return r >> 8, g >> 8, b >> 8
}

// lerp8 linearly interpolates from a to b by coverage t (0-255), rounding to the
// nearest 8-bit value.
func lerp8(a, b, t uint32) uint32 {
	return (a*(255-t) + b*t + 127) / 255
}

// packRGB565 packs 8-bit r, g, b into a 16-bit RGB565 value (top 5/6/5 bits).
func packRGB565(r, g, b uint32) uint16 {
	//nolint:gosec // each shifted component is < 0x10000, so fits uint16.
	return uint16((r&0xF8)<<8) | uint16((g&0xFC)<<3) | uint16(b>>3)
}
