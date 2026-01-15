package gui

import "image/color"

const (
	headerSize      = 11.0
	footerSize      = 9.0
	fontXLarge      = 48.0
	fontLarge       = 14.0
	fontMedium      = 11.0
	fontSmall       = 9.0
	valueLargeSize  = 48.0
	valueSmallSize  = 20.0
	valueMediumSize = 14.0
)

func headerColor() color.RGBA {
	return color.RGBA{R: 255, G: 255, B: 255, A: 1}
}

func footerColor() color.RGBA {
	return color.RGBA{R: 128, G: 128, B: 128, A: 1}
}

func valueColor() color.RGBA {
	return color.RGBA{R: 223, G: 223, B: 223, A: 1}
}

func whiteColor() color.RGBA {
	return color.RGBA{R: 255, G: 255, B: 255, A: 1}
}

func mediumGrayColor() color.RGBA {
	return color.RGBA{R: 128, G: 128, B: 128, A: 1}
}

func lightGrayColor() color.RGBA {
	return color.RGBA{R: 192, G: 192, B: 192, A: 1}
}

func offWhiteColor() color.RGBA {
	return color.RGBA{R: 223, G: 223, B: 223, A: 1}
}
