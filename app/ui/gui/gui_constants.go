package gui

import "image/color"

const (
	headerSize     = 11
	footerSize     = 9
	valueLargeSize = 48
	valueSmallSize = 20
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
