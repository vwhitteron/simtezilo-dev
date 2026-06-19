// Package display provides types and functions for managing display content.
package display

import "image"

// panelSaturation approximates the extra saturation the physical ST7789 panel
// shows versus a calibrated monitor: each channel is pushed away from the pixel's
// luminance by this factor. 1.0 disables the boost; raise it for a punchier match.
// This is a perceptual approximation — calibrate against a photo of your panel.
const panelSaturation = 1.35

// Per-channel gain approximates the panel's white point: the ST7789 reads cooler
// (stronger blue) than a monitor, so blue is lifted slightly. 1.0 per channel is
// neutral. Applied after the saturation boost; tune against a photo of the panel.
const (
	panelGainRed   = 1.0
	panelGainGreen = 1.0
	panelGainBlue  = 1.15
)

// Content represents the content to be displayed, either as text or as a graphical canvas.
type Content struct {
	Text   string
	Canvas *image.RGBA
}

// SimulatePanelRGB565 returns a copy of src as it would appear on an RGB565 panel
// (e.g. the ST7789): each channel is truncated to the panel's bit depth (5/6/5)
// and expanded back to 8 bits by bit-replication, a saturation boost approximates
// the panel's punchier colour response, and the result is fully opaque (the panel
// ignores alpha, compositing over an opaque black background). It is the single
// source of truth for previewing on-device colours off-hardware (web UI dev view,
// livepreview tool).
func SimulatePanelRGB565(src *image.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())

	for idx := 0; idx+3 < len(src.Pix); idx += 4 {
		red := quantise5(src.Pix[idx])
		green := quantise6(src.Pix[idx+1])
		blue := quantise5(src.Pix[idx+2])

		red, green, blue = saturate(red, green, blue, panelSaturation)

		out.Pix[idx] = scaleChannel(red, panelGainRed)
		out.Pix[idx+1] = scaleChannel(green, panelGainGreen)
		out.Pix[idx+2] = scaleChannel(blue, panelGainBlue)
		out.Pix[idx+3] = 255 // panel is opaque
	}

	return out
}

// scaleChannel multiplies a channel by gain, clamped to the 0-255 range.
func scaleChannel(channel byte, gain float64) byte {
	value := float64(channel) * gain
	if value > 255 {
		return 255
	}

	return byte(value + 0.5)
}

// saturate pushes each channel away from the pixel's luminance by factor,
// increasing perceived saturation while preserving brightness. factor 1.0 is a
// no-op; results are clamped to the 0-255 range.
func saturate(red, green, blue byte, factor float64) (byte, byte, byte) {
	if factor == 1.0 {
		return red, green, blue
	}

	luma := 0.299*float64(red) + 0.587*float64(green) + 0.114*float64(blue)

	adjust := func(channel byte) byte {
		value := luma + (float64(channel)-luma)*factor

		switch {
		case value < 0:
			return 0
		case value > 255:
			return 255
		default:
			return byte(value + 0.5)
		}
	}

	return adjust(red), adjust(green), adjust(blue)
}

// quantise5 truncates an 8-bit channel to 5 bits (red/blue on RGB565) and expands
// it back to 8 bits by bit-replication.
func quantise5(v byte) byte {
	t := v >> 3

	return (t << 3) | (t >> 2)
}

// quantise6 truncates an 8-bit channel to 6 bits (green on RGB565) and expands it
// back to 8 bits by bit-replication.
func quantise6(v byte) byte {
	t := v >> 2

	return (t << 2) | (t >> 4)
}

// SumAngle90 sums two angles in degrees with the result being a valid 90-degree angle between 0 and 270 degrees.
// If a non-90 degree rotation is provided, the first angle is returned unmodified.
func SumAngle90(angle1 int, angle2 int) int {
	angle := angle1 + angle2

	if angle%90 != 0 {
		return angle1
	}

	return angle % 360
}
