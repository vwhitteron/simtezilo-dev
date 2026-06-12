package st7789

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
)

// packRGB565 packs src into buf as a stream of RGB565 pixels (low byte first,
// then high byte) using the same coordinate mapping as DrawRAW: for output
// column c and row r, the source pixel is at x = src.Rect.Min.X + cols - c,
// y = src.Rect.Min.Y + r. When the mapped (x, y) falls outside src.Bounds()
// (which is always the case for column c == 0), the pixel is emitted as black
// (0x0000). The buffer is grown only when its capacity is insufficient; the
// returned slice has length cols*rows*2.
func packRGB565(src *image.RGBA, cols, rows int, buf []byte) []byte {
	size := cols * rows * 2
	if cap(buf) < size {
		buf = make([]byte, size)
	}

	buf = buf[:size]
	bounds := src.Bounds()
	idx := 0

	for c := range cols {
		xPos := src.Rect.Min.X + cols - c
		for r := range rows {
			yPos := src.Rect.Min.Y + r

			if xPos < bounds.Min.X || xPos >= bounds.Max.X || yPos < bounds.Min.Y || yPos >= bounds.Max.Y {
				// Out of bounds — emit black (0x0000), matching the old .At() behaviour.
				buf[idx] = 0
				buf[idx+1] = 0
			} else {
				off := (yPos-src.Rect.Min.Y)*src.Stride + (xPos-src.Rect.Min.X)*4
				rv := uint16(src.Pix[off]) >> 3
				gv := uint16(src.Pix[off+1]) >> 2
				bv := uint16(src.Pix[off+2]) >> 3
				c565 := (rv << 11) | (gv << 5) | bv
				buf[idx] = byte(c565)
				buf[idx+1] = byte(c565 >> 8)
			}

			idx += 2
		}
	}

	return buf
}

// FillRectangle fills a rectangle at a given coordinates with a color.
func (d *Device) FillRectangle(x, y, width, height uint16, colorRGBA color.RGBA) error {
	k, i := d.Size()
	if width == 0 || height == 0 ||
		x >= k || (x+width) > k || y >= i || (y+height) > i {
		return errors.New("rectangle coordinates outside display area")
	}

	d.SetWindow()

	c565 := RGBAToRGB565(colorRGBA)
	// Safe conversion from uint16 to uint8 - RGB565 values fit in uint8 when split
	c1 := byte(c565)      // Low byte
	c2 := byte(c565 >> 8) // High byte

	data := make([]uint8, d.PixelCount())
	for rowPixel := range int32(d.pixelColumns) {
		data[rowPixel*2] = c1
		data[rowPixel*2+1] = c2
	}

	column := int32(width) * int32(height)
	for column > 0 {
		if column >= int32(d.pixelRows) {
			_ = d.SendData(data)
		} else {
			_ = d.SendData(data[:column*2])
		}

		column -= int32(d.pixelRows)
	}

	return nil
}

// RGBAToRGB565 converts a 32 bit RGBA color to 16 bit RGB565 format.
func RGBAToRGB565(c color.RGBA) uint16 {
	r := uint16(c.R) >> 3
	g := uint16(c.G) >> 2
	b := uint16(c.B) >> 3

	return (r << 11) | (g << 5) | b
}

// SetPixel sets a pixel in the screen.
func (d *Device) SetPixel(xPos uint16, yPos uint16, colorRGBA color.RGBA) {
	resX, resY := d.getResolution()
	if xPos >= resX || yPos >= resY {
		return
	}

	_ = d.FillRectangle(xPos, yPos, 1, 1, colorRGBA)
}

// FillScreen fills the screen with a given color.
func (d *Device) FillScreen(c color.RGBA) {
	x, y := d.getResolution()

	_ = d.FillRectangle(0, 0, x, y, c)
}

// DrawRAW draws an image to the screen.
func (d *Device) DrawRAW(img image.Image) {
	d.SetWindow()

	// Fast path: if the caller already passes an *image.RGBA, use it directly
	// without an extra allocation or draw.Draw copy.
	src, ok := img.(*image.RGBA)
	if !ok {
		rect := img.Bounds()
		src = image.NewRGBA(rect)
		draw.Draw(src, rect, img, rect.Min, draw.Src)
	}

	d.frameBuf = packRGB565(src, int(d.pixelColumns), int(d.pixelRows), d.frameBuf)

	chunkSize := 4096
	for offset := 0; offset < len(d.frameBuf); offset += chunkSize {
		end := min(offset+chunkSize, len(d.frameBuf))

		err := d.SendData(d.frameBuf[offset:end])
		if err != nil {
			d.log.Error().
				Err(err).
				Str("result", "failure").
				Int("offset", offset).
				Msg("send data")
		}
	}
}
