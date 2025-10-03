package st7789

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
)

// FillRectangle fills a rectangle at a given coordinates with a color.
func (d *Device) FillRectangle(x, y, width, height uint16, c color.RGBA) error {
	k, i := d.Size()
	if width == 0 || height == 0 ||
		x >= k || (x+width) > k || y >= i || (y+height) > i {
		return errors.New("rectangle coordinates outside display area")
	}

	d.SetWindow()

	c565 := RGBAToRGB565(c)
	c1 := uint8(c565)
	c2 := uint8(c565 >> 8)

	data := make([]uint8, d.PixelCount())
	for i := range int32(d.pixelColumns) {
		data[i*2] = c1
		data[i*2+1] = c2
	}

	j := int32(width) * int32(height)
	for j > 0 {
		if j >= int32(d.pixelRows) {
			_ = d.SendData(data)
		} else {
			_ = d.SendData(data[:j*2])
		}

		j -= int32(d.pixelRows)
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
func (d *Device) SetPixel(x uint16, y uint16, c color.RGBA) {
	resX, resY := d.getResolution()
	if x >= resX || y >= resY {
		return
	}

	_ = d.FillRectangle(x, y, 1, 1, c)
}

// FillScreen fills the screen with a given color.
func (d *Device) FillScreen(c color.RGBA) {
	x, y := d.getResolution()

	_ = d.FillRectangle(0, 0, x, y, c)
}

func (d *Device) DrawRAW(img image.Image) {
	d.SetWindow()

	rect := img.Bounds()
	rgbaimg := image.NewRGBA(rect)
	draw.Draw(rgbaimg, rect, img, rect.Min, draw.Src)

	data := []uint8{}

	for column := range d.pixelColumns {
		for row := range d.pixelRows {
			x := rect.Min.X + int(d.pixelColumns) - int(column)
			y := rect.Min.Y + int(row)
			rgba := rgbaimg.At(x, y).(color.RGBA)
			c565 := RGBAToRGB565(rgba)
			data = append(data, uint8(c565), uint8(c565>>8))
		}
	}

	chunkSize := 4096
	for offset := 0; offset < len(data); offset += chunkSize {
		end := min(offset+chunkSize, len(data))

		err := d.SendData(data[offset:end])
		if err != nil {
			d.log.Error().
				Err(err).
				Str("result", "failure").
				Int("offset", offset).
				Msg("send data")
		}
	}
}
