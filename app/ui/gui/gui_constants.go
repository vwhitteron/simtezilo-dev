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
)

func valueColor() color.RGBA {
	return color.RGBA{R: 223, G: 223, B: 223, A: 1}
}

func mediumGrayColor() color.RGBA {
	return color.RGBA{R: 128, G: 128, B: 128, A: 1}
}

func lightGrayColor() color.RGBA {
	return color.RGBA{R: 192, G: 192, B: 192, A: 1}
}

func whiteColor() color.RGBA {
	return color.RGBA{R: 223, G: 223, B: 223, A: 1}
}
