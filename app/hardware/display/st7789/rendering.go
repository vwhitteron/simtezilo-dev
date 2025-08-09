package st7789

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
)

// FillRectangle fills a rectangle at a given coordinates with a color
func (d *Device) FillRectangle(x, y, width, height uint16, c color.RGBA) error {
	k, i := d.Size()
	if width == 0 || height == 0 ||
		x >= k || (x+width) > k || y >= i || (y+height) > i {
		return errors.New("rectangle coordinates outside display area")
	}

	d.SetWindow()
	c565 := RGBATo565(c)
	c1 := uint8(c565)
	c2 := uint8(c565 >> 8)

	data := make([]uint8, d.PixelCount())
	for i := int32(0); i < int32(d.pixelColumns); i++ {
		data[i*2] = c1
		data[i*2+1] = c2
	}

	j := int32(width) * int32(height)
	for j > 0 {
		if j >= int32(d.pixelRows) {
			d.SendData(data)
		} else {
			d.SendData(data[:j*2])
		}
		j -= int32(d.pixelRows)
	}

	return nil
}

// RGBATo565 converts a color.RGBA to uint16 used in the display (bits r:5, g:6, b:5)
func RGBATo565(c color.RGBA) uint16 {
	r, g, b, _ := c.RGBA()

	return uint16((r & 0xF800) +
		((g & 0xFC00) >> 5) +
		((b & 0xF800) >> 11))
}

// SetPixel sets a pixel in the screen
func (d *Device) SetPixel(x uint16, y uint16, c color.RGBA) {
	if ((d.rotation == ROTATION_NONE || d.rotation == ROTATION_180) && (x >= d.pixelColumns || y >= d.pixelRows)) ||
		((d.rotation == ROTATION_90 || d.rotation == ROTATION_270) && (x >= d.pixelRows || y >= d.pixelColumns)) {

		return
	}

	d.FillRectangle(x, y, 1, 1, c)
}

// FillScreen fills the screen with a given color
func (d *Device) FillScreen(c color.RGBA) {
	if d.rotation == ROTATION_NONE || d.rotation == ROTATION_180 {
		d.FillRectangle(0, 0, d.pixelColumns, d.pixelRows, c)
	} else {
		d.FillRectangle(0, 0, d.pixelRows, d.pixelColumns, c)
	}
}

// IsBGR changes the color mode (RGB/BGR)
func (d *Device) IsBGR(bgr bool) {
	d.isBGR = bgr
}

func (d *Device) DrawRAW(img image.Image) {
	d.SetWindow()
	rect := img.Bounds()
	rgbaimg := image.NewRGBA(rect)
	draw.Draw(rgbaimg, rect, img, rect.Min, draw.Src)

	np := []uint8{}
	for i := 0; i < int(d.pixelColumns); i++ {
		for j := 0; j < int(d.pixelRows); j++ {
			rgba := rgbaimg.At(rect.Min.X+int(d.pixelColumns)-i, rect.Min.Y+j).(color.RGBA)
			c565 := RGBATo565(rgba)
			c1 := uint8(c565)
			c2 := uint8(c565 >> 8)
			np = append(np, c1, c2)
		}
	}

	for i := 0; i < len(np); i += 4096 {
		d.SendData(np[i : i+4096])
	}
}
