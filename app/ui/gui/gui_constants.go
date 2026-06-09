package gui

import "image/color"

const (
	fontXLarge = 48.0
	fontStatus = 14.0
	fontLarge  = 14.0
	fontMedium = 11.0
	fontSmall  = 9.0

	// liveValueWidthPercent is the percentage of the panel width the live-view
	// value is allowed to occupy before its font is shrunk to fit.
	liveValueWidthPercent = 90

	// fanIconSize is the rasterised pixel size of the fan footer icon and fanIconGap
	// the gap in pixels between it and the percentage. The fan's thin blades need
	// ~22px to stay readable without anti-aliasing.
	fanIconSize = 22
	fanIconGap  = 4
)

// valueColor is the standard light-grey used for values and primary text. Alpha
// is fully opaque: the ST7789 panel ignores alpha and reads RGB directly, and a
// near-zero alpha composites incorrectly when drawn over a non-black background.
func valueColor() color.RGBA { return color.RGBA{R: 223, G: 223, B: 223, A: 255} }

// fanFooterColor is the opaque tint for the fan footer icon and percentage. It is
// opaque (unlike mediumGrayColor) so the icon mask and text composite correctly
// over a coloured background such as the dashboard rev-limit flash.
func fanFooterColor() color.RGBA { return color.RGBA{R: 150, G: 150, B: 150, A: 255} }

func mediumGrayColor() color.RGBA { return color.RGBA{R: 128, G: 128, B: 128, A: 255} }

func lightGrayColor() color.RGBA { return color.RGBA{R: 192, G: 192, B: 192, A: 255} }
