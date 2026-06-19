package gui

import "image/color"

// This file is the single home for colour definitions shared across the UI. It
// has three layers:
//
//   1. The complete Material Design 2014 reference palette: every hue (Red …
//      Blue Grey) across shades 50–900 plus the A100/A200/A400/A700 accents
//      (the neutral hues Brown, Grey and Blue Grey have no accents). The
//      suffix-less helper for each hue (e.g. MaterialRed) is the 500 swatch.
//   2. A handful of project colours that have no close Material match.
//   3. Semantic colour tokens (frameViolet, tyreHotColor, dashColor*…) that name
//      where a colour is used and map onto the palette, so views read intent
//      rather than RGB triples and a hue can be retuned in one place.
//
// Every helper returns a fully opaque color.RGBA; alpha is always 255 because the
// ST7789 panels read RGB directly and ignore alpha (and a near-zero alpha
// composites incorrectly over a non-black background).
//
// The Material section is mechanically generated from the canonical 2014 hex
// table; keep it sorted by hue and shade so it stays diffable against a swatch
// chart.

// Material Red.
func MaterialRed50() color.RGBA   { return color.RGBA{R: 0xFF, G: 0xEB, B: 0xEE, A: 0xFF} }
func MaterialRed100() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xCD, B: 0xD2, A: 0xFF} }
func MaterialRed200() color.RGBA  { return color.RGBA{R: 0xEF, G: 0x9A, B: 0x9A, A: 0xFF} }
func MaterialRed300() color.RGBA  { return color.RGBA{R: 0xE5, G: 0x73, B: 0x73, A: 0xFF} }
func MaterialRed400() color.RGBA  { return color.RGBA{R: 0xEF, G: 0x53, B: 0x50, A: 0xFF} }
func MaterialRed500() color.RGBA  { return color.RGBA{R: 0xF4, G: 0x43, B: 0x36, A: 0xFF} }
func MaterialRed600() color.RGBA  { return color.RGBA{R: 0xE5, G: 0x39, B: 0x35, A: 0xFF} }
func MaterialRed700() color.RGBA  { return color.RGBA{R: 0xD3, G: 0x2F, B: 0x2F, A: 0xFF} }
func MaterialRed800() color.RGBA  { return color.RGBA{R: 0xC6, G: 0x28, B: 0x28, A: 0xFF} }
func MaterialRed900() color.RGBA  { return color.RGBA{R: 0xB7, G: 0x1C, B: 0x1C, A: 0xFF} }
func MaterialRedA100() color.RGBA { return color.RGBA{R: 0xFF, G: 0x8A, B: 0x80, A: 0xFF} }
func MaterialRedA200() color.RGBA { return color.RGBA{R: 0xFF, G: 0x52, B: 0x52, A: 0xFF} }
func MaterialRedA400() color.RGBA { return color.RGBA{R: 0xFF, G: 0x17, B: 0x44, A: 0xFF} }
func MaterialRedA700() color.RGBA { return color.RGBA{R: 0xD5, G: 0x00, B: 0x00, A: 0xFF} }
func MaterialRed() color.RGBA     { return MaterialRed500() }

// Material Pink.
func MaterialPink50() color.RGBA   { return color.RGBA{R: 0xFC, G: 0xE4, B: 0xEC, A: 0xFF} }
func MaterialPink100() color.RGBA  { return color.RGBA{R: 0xF8, G: 0xBB, B: 0xD0, A: 0xFF} }
func MaterialPink200() color.RGBA  { return color.RGBA{R: 0xF4, G: 0x8F, B: 0xB1, A: 0xFF} }
func MaterialPink300() color.RGBA  { return color.RGBA{R: 0xF0, G: 0x62, B: 0x92, A: 0xFF} }
func MaterialPink400() color.RGBA  { return color.RGBA{R: 0xEC, G: 0x40, B: 0x7A, A: 0xFF} }
func MaterialPink500() color.RGBA  { return color.RGBA{R: 0xE9, G: 0x1E, B: 0x63, A: 0xFF} }
func MaterialPink600() color.RGBA  { return color.RGBA{R: 0xD8, G: 0x1B, B: 0x60, A: 0xFF} }
func MaterialPink700() color.RGBA  { return color.RGBA{R: 0xC2, G: 0x18, B: 0x5B, A: 0xFF} }
func MaterialPink800() color.RGBA  { return color.RGBA{R: 0xAD, G: 0x14, B: 0x57, A: 0xFF} }
func MaterialPink900() color.RGBA  { return color.RGBA{R: 0x88, G: 0x0E, B: 0x4F, A: 0xFF} }
func MaterialPinkA100() color.RGBA { return color.RGBA{R: 0xFF, G: 0x80, B: 0xAB, A: 0xFF} }
func MaterialPinkA200() color.RGBA { return color.RGBA{R: 0xFF, G: 0x40, B: 0x81, A: 0xFF} }
func MaterialPinkA400() color.RGBA { return color.RGBA{R: 0xF5, G: 0x00, B: 0x57, A: 0xFF} }
func MaterialPinkA700() color.RGBA { return color.RGBA{R: 0xC5, G: 0x11, B: 0x62, A: 0xFF} }
func MaterialPink() color.RGBA     { return MaterialPink500() }

// Material Purple.
func MaterialPurple50() color.RGBA   { return color.RGBA{R: 0xF3, G: 0xE5, B: 0xF5, A: 0xFF} }
func MaterialPurple100() color.RGBA  { return color.RGBA{R: 0xE1, G: 0xBE, B: 0xE7, A: 0xFF} }
func MaterialPurple200() color.RGBA  { return color.RGBA{R: 0xCE, G: 0x93, B: 0xD8, A: 0xFF} }
func MaterialPurple300() color.RGBA  { return color.RGBA{R: 0xBA, G: 0x68, B: 0xC8, A: 0xFF} }
func MaterialPurple400() color.RGBA  { return color.RGBA{R: 0xAB, G: 0x47, B: 0xBC, A: 0xFF} }
func MaterialPurple500() color.RGBA  { return color.RGBA{R: 0x9C, G: 0x27, B: 0xB0, A: 0xFF} }
func MaterialPurple600() color.RGBA  { return color.RGBA{R: 0x8E, G: 0x24, B: 0xAA, A: 0xFF} }
func MaterialPurple700() color.RGBA  { return color.RGBA{R: 0x7B, G: 0x1F, B: 0xA2, A: 0xFF} }
func MaterialPurple800() color.RGBA  { return color.RGBA{R: 0x6A, G: 0x1B, B: 0x9A, A: 0xFF} }
func MaterialPurple900() color.RGBA  { return color.RGBA{R: 0x4A, G: 0x14, B: 0x8C, A: 0xFF} }
func MaterialPurpleA100() color.RGBA { return color.RGBA{R: 0xEA, G: 0x80, B: 0xFC, A: 0xFF} }
func MaterialPurpleA200() color.RGBA { return color.RGBA{R: 0xE0, G: 0x40, B: 0xFB, A: 0xFF} }
func MaterialPurpleA400() color.RGBA { return color.RGBA{R: 0xD5, G: 0x00, B: 0xF9, A: 0xFF} }
func MaterialPurpleA700() color.RGBA { return color.RGBA{R: 0xAA, G: 0x00, B: 0xFF, A: 0xFF} }
func MaterialPurple() color.RGBA     { return MaterialPurple500() }

// Material Deep Purple.
func MaterialDeepPurple50() color.RGBA   { return color.RGBA{R: 0xED, G: 0xE7, B: 0xF6, A: 0xFF} }
func MaterialDeepPurple100() color.RGBA  { return color.RGBA{R: 0xD1, G: 0xC4, B: 0xE9, A: 0xFF} }
func MaterialDeepPurple200() color.RGBA  { return color.RGBA{R: 0xB3, G: 0x9D, B: 0xDB, A: 0xFF} }
func MaterialDeepPurple300() color.RGBA  { return color.RGBA{R: 0x95, G: 0x75, B: 0xCD, A: 0xFF} }
func MaterialDeepPurple400() color.RGBA  { return color.RGBA{R: 0x7E, G: 0x57, B: 0xC2, A: 0xFF} }
func MaterialDeepPurple500() color.RGBA  { return color.RGBA{R: 0x67, G: 0x3A, B: 0xB7, A: 0xFF} }
func MaterialDeepPurple600() color.RGBA  { return color.RGBA{R: 0x5E, G: 0x35, B: 0xB1, A: 0xFF} }
func MaterialDeepPurple700() color.RGBA  { return color.RGBA{R: 0x51, G: 0x2D, B: 0xA8, A: 0xFF} }
func MaterialDeepPurple800() color.RGBA  { return color.RGBA{R: 0x45, G: 0x27, B: 0xA0, A: 0xFF} }
func MaterialDeepPurple900() color.RGBA  { return color.RGBA{R: 0x31, G: 0x1B, B: 0x92, A: 0xFF} }
func MaterialDeepPurpleA100() color.RGBA { return color.RGBA{R: 0xB3, G: 0x88, B: 0xFF, A: 0xFF} }
func MaterialDeepPurpleA200() color.RGBA { return color.RGBA{R: 0x7C, G: 0x4D, B: 0xFF, A: 0xFF} }
func MaterialDeepPurpleA400() color.RGBA { return color.RGBA{R: 0x65, G: 0x1F, B: 0xFF, A: 0xFF} }
func MaterialDeepPurpleA700() color.RGBA { return color.RGBA{R: 0x62, G: 0x00, B: 0xEA, A: 0xFF} }
func MaterialDeepPurple() color.RGBA     { return MaterialDeepPurple500() }

// Material Indigo.
func MaterialIndigo50() color.RGBA   { return color.RGBA{R: 0xE8, G: 0xEA, B: 0xF6, A: 0xFF} }
func MaterialIndigo100() color.RGBA  { return color.RGBA{R: 0xC5, G: 0xCA, B: 0xE9, A: 0xFF} }
func MaterialIndigo200() color.RGBA  { return color.RGBA{R: 0x9F, G: 0xA8, B: 0xDA, A: 0xFF} }
func MaterialIndigo300() color.RGBA  { return color.RGBA{R: 0x79, G: 0x86, B: 0xCB, A: 0xFF} }
func MaterialIndigo400() color.RGBA  { return color.RGBA{R: 0x5C, G: 0x6B, B: 0xC0, A: 0xFF} }
func MaterialIndigo500() color.RGBA  { return color.RGBA{R: 0x3F, G: 0x51, B: 0xB5, A: 0xFF} }
func MaterialIndigo600() color.RGBA  { return color.RGBA{R: 0x39, G: 0x49, B: 0xAB, A: 0xFF} }
func MaterialIndigo700() color.RGBA  { return color.RGBA{R: 0x30, G: 0x3F, B: 0x9F, A: 0xFF} }
func MaterialIndigo800() color.RGBA  { return color.RGBA{R: 0x28, G: 0x35, B: 0x93, A: 0xFF} }
func MaterialIndigo900() color.RGBA  { return color.RGBA{R: 0x1A, G: 0x23, B: 0x7E, A: 0xFF} }
func MaterialIndigoA100() color.RGBA { return color.RGBA{R: 0x8C, G: 0x9E, B: 0xFF, A: 0xFF} }
func MaterialIndigoA200() color.RGBA { return color.RGBA{R: 0x53, G: 0x6D, B: 0xFE, A: 0xFF} }
func MaterialIndigoA400() color.RGBA { return color.RGBA{R: 0x3D, G: 0x5A, B: 0xFE, A: 0xFF} }
func MaterialIndigoA700() color.RGBA { return color.RGBA{R: 0x30, G: 0x4F, B: 0xFE, A: 0xFF} }
func MaterialIndigo() color.RGBA     { return MaterialIndigo500() }

// Material Blue.
func MaterialBlue50() color.RGBA   { return color.RGBA{R: 0xE3, G: 0xF2, B: 0xFD, A: 0xFF} }
func MaterialBlue100() color.RGBA  { return color.RGBA{R: 0xBB, G: 0xDE, B: 0xFB, A: 0xFF} }
func MaterialBlue200() color.RGBA  { return color.RGBA{R: 0x90, G: 0xCA, B: 0xF9, A: 0xFF} }
func MaterialBlue300() color.RGBA  { return color.RGBA{R: 0x64, G: 0xB5, B: 0xF6, A: 0xFF} }
func MaterialBlue400() color.RGBA  { return color.RGBA{R: 0x42, G: 0xA5, B: 0xF5, A: 0xFF} }
func MaterialBlue500() color.RGBA  { return color.RGBA{R: 0x21, G: 0x96, B: 0xF3, A: 0xFF} }
func MaterialBlue600() color.RGBA  { return color.RGBA{R: 0x1E, G: 0x88, B: 0xE5, A: 0xFF} }
func MaterialBlue700() color.RGBA  { return color.RGBA{R: 0x19, G: 0x76, B: 0xD2, A: 0xFF} }
func MaterialBlue800() color.RGBA  { return color.RGBA{R: 0x15, G: 0x65, B: 0xC0, A: 0xFF} }
func MaterialBlue900() color.RGBA  { return color.RGBA{R: 0x0D, G: 0x47, B: 0xA1, A: 0xFF} }
func MaterialBlueA100() color.RGBA { return color.RGBA{R: 0x82, G: 0xB1, B: 0xFF, A: 0xFF} }
func MaterialBlueA200() color.RGBA { return color.RGBA{R: 0x44, G: 0x8A, B: 0xFF, A: 0xFF} }
func MaterialBlueA400() color.RGBA { return color.RGBA{R: 0x29, G: 0x79, B: 0xFF, A: 0xFF} }
func MaterialBlueA700() color.RGBA { return color.RGBA{R: 0x29, G: 0x62, B: 0xFF, A: 0xFF} }
func MaterialBlue() color.RGBA     { return MaterialBlue500() }

// Material Light Blue.
func MaterialLightBlue50() color.RGBA   { return color.RGBA{R: 0xE1, G: 0xF5, B: 0xFE, A: 0xFF} }
func MaterialLightBlue100() color.RGBA  { return color.RGBA{R: 0xB3, G: 0xE5, B: 0xFC, A: 0xFF} }
func MaterialLightBlue200() color.RGBA  { return color.RGBA{R: 0x81, G: 0xD4, B: 0xFA, A: 0xFF} }
func MaterialLightBlue300() color.RGBA  { return color.RGBA{R: 0x4F, G: 0xC3, B: 0xF7, A: 0xFF} }
func MaterialLightBlue400() color.RGBA  { return color.RGBA{R: 0x29, G: 0xB6, B: 0xF6, A: 0xFF} }
func MaterialLightBlue500() color.RGBA  { return color.RGBA{R: 0x03, G: 0xA9, B: 0xF4, A: 0xFF} }
func MaterialLightBlue600() color.RGBA  { return color.RGBA{R: 0x03, G: 0x9B, B: 0xE5, A: 0xFF} }
func MaterialLightBlue700() color.RGBA  { return color.RGBA{R: 0x02, G: 0x88, B: 0xD1, A: 0xFF} }
func MaterialLightBlue800() color.RGBA  { return color.RGBA{R: 0x02, G: 0x77, B: 0xBD, A: 0xFF} }
func MaterialLightBlue900() color.RGBA  { return color.RGBA{R: 0x01, G: 0x57, B: 0x9B, A: 0xFF} }
func MaterialLightBlueA100() color.RGBA { return color.RGBA{R: 0x80, G: 0xD8, B: 0xFF, A: 0xFF} }
func MaterialLightBlueA200() color.RGBA { return color.RGBA{R: 0x40, G: 0xC4, B: 0xFF, A: 0xFF} }
func MaterialLightBlueA400() color.RGBA { return color.RGBA{R: 0x00, G: 0xB0, B: 0xFF, A: 0xFF} }
func MaterialLightBlueA700() color.RGBA { return color.RGBA{R: 0x00, G: 0x91, B: 0xEA, A: 0xFF} }
func MaterialLightBlue() color.RGBA     { return MaterialLightBlue500() }

// Material Cyan.
func MaterialCyan50() color.RGBA   { return color.RGBA{R: 0xE0, G: 0xF7, B: 0xFA, A: 0xFF} }
func MaterialCyan100() color.RGBA  { return color.RGBA{R: 0xB2, G: 0xEB, B: 0xF2, A: 0xFF} }
func MaterialCyan200() color.RGBA  { return color.RGBA{R: 0x80, G: 0xDE, B: 0xEA, A: 0xFF} }
func MaterialCyan300() color.RGBA  { return color.RGBA{R: 0x4D, G: 0xD0, B: 0xE1, A: 0xFF} }
func MaterialCyan400() color.RGBA  { return color.RGBA{R: 0x26, G: 0xC6, B: 0xDA, A: 0xFF} }
func MaterialCyan500() color.RGBA  { return color.RGBA{R: 0x00, G: 0xBC, B: 0xD4, A: 0xFF} }
func MaterialCyan600() color.RGBA  { return color.RGBA{R: 0x00, G: 0xAC, B: 0xC1, A: 0xFF} }
func MaterialCyan700() color.RGBA  { return color.RGBA{R: 0x00, G: 0x97, B: 0xA7, A: 0xFF} }
func MaterialCyan800() color.RGBA  { return color.RGBA{R: 0x00, G: 0x83, B: 0x8F, A: 0xFF} }
func MaterialCyan900() color.RGBA  { return color.RGBA{R: 0x00, G: 0x60, B: 0x64, A: 0xFF} }
func MaterialCyanA100() color.RGBA { return color.RGBA{R: 0x84, G: 0xFF, B: 0xFF, A: 0xFF} }
func MaterialCyanA200() color.RGBA { return color.RGBA{R: 0x18, G: 0xFF, B: 0xFF, A: 0xFF} }
func MaterialCyanA400() color.RGBA { return color.RGBA{R: 0x00, G: 0xE5, B: 0xFF, A: 0xFF} }
func MaterialCyanA700() color.RGBA { return color.RGBA{R: 0x00, G: 0xB8, B: 0xD4, A: 0xFF} }
func MaterialCyan() color.RGBA     { return MaterialCyan500() }

// Material Teal.
func MaterialTeal50() color.RGBA   { return color.RGBA{R: 0xE0, G: 0xF2, B: 0xF1, A: 0xFF} }
func MaterialTeal100() color.RGBA  { return color.RGBA{R: 0xB2, G: 0xDF, B: 0xDB, A: 0xFF} }
func MaterialTeal200() color.RGBA  { return color.RGBA{R: 0x80, G: 0xCB, B: 0xC4, A: 0xFF} }
func MaterialTeal300() color.RGBA  { return color.RGBA{R: 0x4D, G: 0xB6, B: 0xAC, A: 0xFF} }
func MaterialTeal400() color.RGBA  { return color.RGBA{R: 0x26, G: 0xA6, B: 0x9A, A: 0xFF} }
func MaterialTeal500() color.RGBA  { return color.RGBA{R: 0x00, G: 0x96, B: 0x88, A: 0xFF} }
func MaterialTeal600() color.RGBA  { return color.RGBA{R: 0x00, G: 0x89, B: 0x7B, A: 0xFF} }
func MaterialTeal700() color.RGBA  { return color.RGBA{R: 0x00, G: 0x79, B: 0x6B, A: 0xFF} }
func MaterialTeal800() color.RGBA  { return color.RGBA{R: 0x00, G: 0x69, B: 0x5C, A: 0xFF} }
func MaterialTeal900() color.RGBA  { return color.RGBA{R: 0x00, G: 0x4D, B: 0x40, A: 0xFF} }
func MaterialTealA100() color.RGBA { return color.RGBA{R: 0xA7, G: 0xFF, B: 0xEB, A: 0xFF} }
func MaterialTealA200() color.RGBA { return color.RGBA{R: 0x64, G: 0xFF, B: 0xDA, A: 0xFF} }
func MaterialTealA400() color.RGBA { return color.RGBA{R: 0x1D, G: 0xE9, B: 0xB6, A: 0xFF} }
func MaterialTealA700() color.RGBA { return color.RGBA{R: 0x00, G: 0xBF, B: 0xA5, A: 0xFF} }
func MaterialTeal() color.RGBA     { return MaterialTeal500() }

// Material Green.
func MaterialGreen50() color.RGBA   { return color.RGBA{R: 0xE8, G: 0xF5, B: 0xE9, A: 0xFF} }
func MaterialGreen100() color.RGBA  { return color.RGBA{R: 0xC8, G: 0xE6, B: 0xC9, A: 0xFF} }
func MaterialGreen200() color.RGBA  { return color.RGBA{R: 0xA5, G: 0xD6, B: 0xA7, A: 0xFF} }
func MaterialGreen300() color.RGBA  { return color.RGBA{R: 0x81, G: 0xC7, B: 0x84, A: 0xFF} }
func MaterialGreen400() color.RGBA  { return color.RGBA{R: 0x66, G: 0xBB, B: 0x6A, A: 0xFF} }
func MaterialGreen500() color.RGBA  { return color.RGBA{R: 0x4C, G: 0xAF, B: 0x50, A: 0xFF} }
func MaterialGreen600() color.RGBA  { return color.RGBA{R: 0x43, G: 0xA0, B: 0x47, A: 0xFF} }
func MaterialGreen700() color.RGBA  { return color.RGBA{R: 0x38, G: 0x8E, B: 0x3C, A: 0xFF} }
func MaterialGreen800() color.RGBA  { return color.RGBA{R: 0x2E, G: 0x7D, B: 0x32, A: 0xFF} }
func MaterialGreen900() color.RGBA  { return color.RGBA{R: 0x1B, G: 0x5E, B: 0x20, A: 0xFF} }
func MaterialGreenA100() color.RGBA { return color.RGBA{R: 0xB9, G: 0xF6, B: 0xCA, A: 0xFF} }
func MaterialGreenA200() color.RGBA { return color.RGBA{R: 0x69, G: 0xF0, B: 0xAE, A: 0xFF} }
func MaterialGreenA400() color.RGBA { return color.RGBA{R: 0x00, G: 0xE6, B: 0x76, A: 0xFF} }
func MaterialGreenA700() color.RGBA { return color.RGBA{R: 0x00, G: 0xC8, B: 0x53, A: 0xFF} }
func MaterialGreen() color.RGBA     { return MaterialGreen500() }

// Material Light Green.
func MaterialLightGreen50() color.RGBA   { return color.RGBA{R: 0xF1, G: 0xF8, B: 0xE9, A: 0xFF} }
func MaterialLightGreen100() color.RGBA  { return color.RGBA{R: 0xDC, G: 0xED, B: 0xC8, A: 0xFF} }
func MaterialLightGreen200() color.RGBA  { return color.RGBA{R: 0xC5, G: 0xE1, B: 0xA5, A: 0xFF} }
func MaterialLightGreen300() color.RGBA  { return color.RGBA{R: 0xAE, G: 0xD5, B: 0x81, A: 0xFF} }
func MaterialLightGreen400() color.RGBA  { return color.RGBA{R: 0x9C, G: 0xCC, B: 0x65, A: 0xFF} }
func MaterialLightGreen500() color.RGBA  { return color.RGBA{R: 0x8B, G: 0xC3, B: 0x4A, A: 0xFF} }
func MaterialLightGreen600() color.RGBA  { return color.RGBA{R: 0x7C, G: 0xB3, B: 0x42, A: 0xFF} }
func MaterialLightGreen700() color.RGBA  { return color.RGBA{R: 0x68, G: 0x9F, B: 0x38, A: 0xFF} }
func MaterialLightGreen800() color.RGBA  { return color.RGBA{R: 0x55, G: 0x8B, B: 0x2F, A: 0xFF} }
func MaterialLightGreen900() color.RGBA  { return color.RGBA{R: 0x33, G: 0x69, B: 0x1E, A: 0xFF} }
func MaterialLightGreenA100() color.RGBA { return color.RGBA{R: 0xCC, G: 0xFF, B: 0x90, A: 0xFF} }
func MaterialLightGreenA200() color.RGBA { return color.RGBA{R: 0xB2, G: 0xFF, B: 0x59, A: 0xFF} }
func MaterialLightGreenA400() color.RGBA { return color.RGBA{R: 0x76, G: 0xFF, B: 0x03, A: 0xFF} }
func MaterialLightGreenA700() color.RGBA { return color.RGBA{R: 0x64, G: 0xDD, B: 0x17, A: 0xFF} }
func MaterialLightGreen() color.RGBA     { return MaterialLightGreen500() }

// Material Lime.
func MaterialLime50() color.RGBA   { return color.RGBA{R: 0xF9, G: 0xFB, B: 0xE7, A: 0xFF} }
func MaterialLime100() color.RGBA  { return color.RGBA{R: 0xF0, G: 0xF4, B: 0xC3, A: 0xFF} }
func MaterialLime200() color.RGBA  { return color.RGBA{R: 0xE6, G: 0xEE, B: 0x9C, A: 0xFF} }
func MaterialLime300() color.RGBA  { return color.RGBA{R: 0xDC, G: 0xE7, B: 0x75, A: 0xFF} }
func MaterialLime400() color.RGBA  { return color.RGBA{R: 0xD4, G: 0xE1, B: 0x57, A: 0xFF} }
func MaterialLime500() color.RGBA  { return color.RGBA{R: 0xCD, G: 0xDC, B: 0x39, A: 0xFF} }
func MaterialLime600() color.RGBA  { return color.RGBA{R: 0xC0, G: 0xCA, B: 0x33, A: 0xFF} }
func MaterialLime700() color.RGBA  { return color.RGBA{R: 0xAF, G: 0xB4, B: 0x2B, A: 0xFF} }
func MaterialLime800() color.RGBA  { return color.RGBA{R: 0x9E, G: 0x9D, B: 0x24, A: 0xFF} }
func MaterialLime900() color.RGBA  { return color.RGBA{R: 0x82, G: 0x77, B: 0x17, A: 0xFF} }
func MaterialLimeA100() color.RGBA { return color.RGBA{R: 0xF4, G: 0xFF, B: 0x81, A: 0xFF} }
func MaterialLimeA200() color.RGBA { return color.RGBA{R: 0xEE, G: 0xFF, B: 0x41, A: 0xFF} }
func MaterialLimeA400() color.RGBA { return color.RGBA{R: 0xC6, G: 0xFF, B: 0x00, A: 0xFF} }
func MaterialLimeA700() color.RGBA { return color.RGBA{R: 0xAE, G: 0xEA, B: 0x00, A: 0xFF} }
func MaterialLime() color.RGBA     { return MaterialLime500() }

// Material Yellow.
func MaterialYellow50() color.RGBA   { return color.RGBA{R: 0xFF, G: 0xFD, B: 0xE7, A: 0xFF} }
func MaterialYellow100() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xF9, B: 0xC4, A: 0xFF} }
func MaterialYellow200() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xF5, B: 0x9D, A: 0xFF} }
func MaterialYellow300() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xF1, B: 0x76, A: 0xFF} }
func MaterialYellow400() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xEE, B: 0x58, A: 0xFF} }
func MaterialYellow500() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xEB, B: 0x3B, A: 0xFF} }
func MaterialYellow600() color.RGBA  { return color.RGBA{R: 0xFD, G: 0xD8, B: 0x35, A: 0xFF} }
func MaterialYellow700() color.RGBA  { return color.RGBA{R: 0xFB, G: 0xC0, B: 0x2D, A: 0xFF} }
func MaterialYellow800() color.RGBA  { return color.RGBA{R: 0xF9, G: 0xA8, B: 0x25, A: 0xFF} }
func MaterialYellow900() color.RGBA  { return color.RGBA{R: 0xF5, G: 0x7F, B: 0x17, A: 0xFF} }
func MaterialYellowA100() color.RGBA { return color.RGBA{R: 0xFF, G: 0xFF, B: 0x8D, A: 0xFF} }
func MaterialYellowA200() color.RGBA { return color.RGBA{R: 0xFF, G: 0xFF, B: 0x00, A: 0xFF} }
func MaterialYellowA400() color.RGBA { return color.RGBA{R: 0xFF, G: 0xEA, B: 0x00, A: 0xFF} }
func MaterialYellowA700() color.RGBA { return color.RGBA{R: 0xFF, G: 0xD6, B: 0x00, A: 0xFF} }
func MaterialYellow() color.RGBA     { return MaterialYellow500() }

// Material Amber.
func MaterialAmber50() color.RGBA   { return color.RGBA{R: 0xFF, G: 0xF8, B: 0xE1, A: 0xFF} }
func MaterialAmber100() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xEC, B: 0xB3, A: 0xFF} }
func MaterialAmber200() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xE0, B: 0x82, A: 0xFF} }
func MaterialAmber300() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xD5, B: 0x4F, A: 0xFF} }
func MaterialAmber400() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xCA, B: 0x28, A: 0xFF} }
func MaterialAmber500() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xC1, B: 0x07, A: 0xFF} }
func MaterialAmber600() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xB3, B: 0x00, A: 0xFF} }
func MaterialAmber700() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xA0, B: 0x00, A: 0xFF} }
func MaterialAmber800() color.RGBA  { return color.RGBA{R: 0xFF, G: 0x8F, B: 0x00, A: 0xFF} }
func MaterialAmber900() color.RGBA  { return color.RGBA{R: 0xFF, G: 0x6F, B: 0x00, A: 0xFF} }
func MaterialAmberA100() color.RGBA { return color.RGBA{R: 0xFF, G: 0xE5, B: 0x7F, A: 0xFF} }
func MaterialAmberA200() color.RGBA { return color.RGBA{R: 0xFF, G: 0xD7, B: 0x40, A: 0xFF} }
func MaterialAmberA400() color.RGBA { return color.RGBA{R: 0xFF, G: 0xC4, B: 0x00, A: 0xFF} }
func MaterialAmberA700() color.RGBA { return color.RGBA{R: 0xFF, G: 0xAB, B: 0x00, A: 0xFF} }
func MaterialAmber() color.RGBA     { return MaterialAmber500() }

// Material Orange.
func MaterialOrange50() color.RGBA   { return color.RGBA{R: 0xFF, G: 0xF3, B: 0xE0, A: 0xFF} }
func MaterialOrange100() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xE0, B: 0xB2, A: 0xFF} }
func MaterialOrange200() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xCC, B: 0x80, A: 0xFF} }
func MaterialOrange300() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xB7, B: 0x4D, A: 0xFF} }
func MaterialOrange400() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xA7, B: 0x26, A: 0xFF} }
func MaterialOrange500() color.RGBA  { return color.RGBA{R: 0xFF, G: 0x98, B: 0x00, A: 0xFF} }
func MaterialOrange600() color.RGBA  { return color.RGBA{R: 0xFB, G: 0x8C, B: 0x00, A: 0xFF} }
func MaterialOrange700() color.RGBA  { return color.RGBA{R: 0xF5, G: 0x7C, B: 0x00, A: 0xFF} }
func MaterialOrange800() color.RGBA  { return color.RGBA{R: 0xEF, G: 0x6C, B: 0x00, A: 0xFF} }
func MaterialOrange900() color.RGBA  { return color.RGBA{R: 0xE6, G: 0x51, B: 0x00, A: 0xFF} }
func MaterialOrangeA100() color.RGBA { return color.RGBA{R: 0xFF, G: 0xD1, B: 0x80, A: 0xFF} }
func MaterialOrangeA200() color.RGBA { return color.RGBA{R: 0xFF, G: 0xAB, B: 0x40, A: 0xFF} }
func MaterialOrangeA400() color.RGBA { return color.RGBA{R: 0xFF, G: 0x91, B: 0x00, A: 0xFF} }
func MaterialOrangeA700() color.RGBA { return color.RGBA{R: 0xFF, G: 0x6D, B: 0x00, A: 0xFF} }
func MaterialOrange() color.RGBA     { return MaterialOrange500() }

// Material Deep Orange.
func MaterialDeepOrange50() color.RGBA   { return color.RGBA{R: 0xFB, G: 0xE9, B: 0xE7, A: 0xFF} }
func MaterialDeepOrange100() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xCC, B: 0xBC, A: 0xFF} }
func MaterialDeepOrange200() color.RGBA  { return color.RGBA{R: 0xFF, G: 0xAB, B: 0x91, A: 0xFF} }
func MaterialDeepOrange300() color.RGBA  { return color.RGBA{R: 0xFF, G: 0x8A, B: 0x65, A: 0xFF} }
func MaterialDeepOrange400() color.RGBA  { return color.RGBA{R: 0xFF, G: 0x70, B: 0x43, A: 0xFF} }
func MaterialDeepOrange500() color.RGBA  { return color.RGBA{R: 0xFF, G: 0x57, B: 0x22, A: 0xFF} }
func MaterialDeepOrange600() color.RGBA  { return color.RGBA{R: 0xF4, G: 0x51, B: 0x1E, A: 0xFF} }
func MaterialDeepOrange700() color.RGBA  { return color.RGBA{R: 0xE6, G: 0x4A, B: 0x19, A: 0xFF} }
func MaterialDeepOrange800() color.RGBA  { return color.RGBA{R: 0xD8, G: 0x43, B: 0x15, A: 0xFF} }
func MaterialDeepOrange900() color.RGBA  { return color.RGBA{R: 0xBF, G: 0x36, B: 0x0C, A: 0xFF} }
func MaterialDeepOrangeA100() color.RGBA { return color.RGBA{R: 0xFF, G: 0x9E, B: 0x80, A: 0xFF} }
func MaterialDeepOrangeA200() color.RGBA { return color.RGBA{R: 0xFF, G: 0x6E, B: 0x40, A: 0xFF} }
func MaterialDeepOrangeA400() color.RGBA { return color.RGBA{R: 0xFF, G: 0x3D, B: 0x00, A: 0xFF} }
func MaterialDeepOrangeA700() color.RGBA { return color.RGBA{R: 0xDD, G: 0x2C, B: 0x00, A: 0xFF} }
func MaterialDeepOrange() color.RGBA     { return MaterialDeepOrange500() }

// Material Brown.
func MaterialBrown50() color.RGBA  { return color.RGBA{R: 0xEF, G: 0xEB, B: 0xE9, A: 0xFF} }
func MaterialBrown100() color.RGBA { return color.RGBA{R: 0xD7, G: 0xCC, B: 0xC8, A: 0xFF} }
func MaterialBrown200() color.RGBA { return color.RGBA{R: 0xBC, G: 0xAA, B: 0xA4, A: 0xFF} }
func MaterialBrown300() color.RGBA { return color.RGBA{R: 0xA1, G: 0x88, B: 0x7F, A: 0xFF} }
func MaterialBrown400() color.RGBA { return color.RGBA{R: 0x8D, G: 0x6E, B: 0x63, A: 0xFF} }
func MaterialBrown500() color.RGBA { return color.RGBA{R: 0x79, G: 0x55, B: 0x48, A: 0xFF} }
func MaterialBrown600() color.RGBA { return color.RGBA{R: 0x6D, G: 0x4C, B: 0x41, A: 0xFF} }
func MaterialBrown700() color.RGBA { return color.RGBA{R: 0x5D, G: 0x40, B: 0x37, A: 0xFF} }
func MaterialBrown800() color.RGBA { return color.RGBA{R: 0x4E, G: 0x34, B: 0x2E, A: 0xFF} }
func MaterialBrown900() color.RGBA { return color.RGBA{R: 0x3E, G: 0x27, B: 0x23, A: 0xFF} }
func MaterialBrown() color.RGBA    { return MaterialBrown500() }

// Material Grey.
func MaterialGrey50() color.RGBA  { return color.RGBA{R: 0xFA, G: 0xFA, B: 0xFA, A: 0xFF} }
func MaterialGrey100() color.RGBA { return color.RGBA{R: 0xF5, G: 0xF5, B: 0xF5, A: 0xFF} }
func MaterialGrey200() color.RGBA { return color.RGBA{R: 0xEE, G: 0xEE, B: 0xEE, A: 0xFF} }
func MaterialGrey300() color.RGBA { return color.RGBA{R: 0xE0, G: 0xE0, B: 0xE0, A: 0xFF} }
func MaterialGrey400() color.RGBA { return color.RGBA{R: 0xBD, G: 0xBD, B: 0xBD, A: 0xFF} }
func MaterialGrey500() color.RGBA { return color.RGBA{R: 0x9E, G: 0x9E, B: 0x9E, A: 0xFF} }
func MaterialGrey600() color.RGBA { return color.RGBA{R: 0x75, G: 0x75, B: 0x75, A: 0xFF} }
func MaterialGrey700() color.RGBA { return color.RGBA{R: 0x61, G: 0x61, B: 0x61, A: 0xFF} }
func MaterialGrey800() color.RGBA { return color.RGBA{R: 0x42, G: 0x42, B: 0x42, A: 0xFF} }
func MaterialGrey900() color.RGBA { return color.RGBA{R: 0x21, G: 0x21, B: 0x21, A: 0xFF} }
func MaterialGrey() color.RGBA    { return MaterialGrey500() }

// Material Blue Grey.
func MaterialBlueGrey50() color.RGBA  { return color.RGBA{R: 0xEC, G: 0xEF, B: 0xF1, A: 0xFF} }
func MaterialBlueGrey100() color.RGBA { return color.RGBA{R: 0xCF, G: 0xD8, B: 0xDC, A: 0xFF} }
func MaterialBlueGrey200() color.RGBA { return color.RGBA{R: 0xB0, G: 0xBE, B: 0xC5, A: 0xFF} }
func MaterialBlueGrey300() color.RGBA { return color.RGBA{R: 0x90, G: 0xA4, B: 0xAE, A: 0xFF} }
func MaterialBlueGrey400() color.RGBA { return color.RGBA{R: 0x78, G: 0x90, B: 0x9C, A: 0xFF} }
func MaterialBlueGrey500() color.RGBA { return color.RGBA{R: 0x60, G: 0x7D, B: 0x8B, A: 0xFF} }
func MaterialBlueGrey600() color.RGBA { return color.RGBA{R: 0x54, G: 0x6E, B: 0x7A, A: 0xFF} }
func MaterialBlueGrey700() color.RGBA { return color.RGBA{R: 0x45, G: 0x5A, B: 0x64, A: 0xFF} }
func MaterialBlueGrey800() color.RGBA { return color.RGBA{R: 0x37, G: 0x47, B: 0x4F, A: 0xFF} }
func MaterialBlueGrey900() color.RGBA { return color.RGBA{R: 0x26, G: 0x32, B: 0x38, A: 0xFF} }
func MaterialBlueGrey() color.RGBA    { return MaterialBlueGrey500() }

// Project colours with no close Material match.
//
// black is a true black backdrop (Material has no 500 black swatch). frameMustard
// is the dark olive-yellow used for the Tyres frame; the nearest Material yellows
// are far brighter, so it stays a tuned literal derived from RGB565 31,53,0.
// dashFlashRed is the very dark red the dashboard flashes as a shift/alert cue;
// it is darker than Material's deepest red (900), so it stays a tuned literal.
func blackColor() color.RGBA   { return color.RGBA{R: 0, G: 0, B: 0, A: 255} }
func frameMustard() color.RGBA { return color.RGBA{R: 206, G: 170, B: 0, A: 255} }
func dashFlashRed() color.RGBA { return color.RGBA{R: 130, G: 0, B: 0, A: 255} }

// Per-view frame colours: the rounded border and header box of each framed live
// view (Delta, Tyres, Lap, Fuel).
func frameViolet() color.RGBA { return MaterialPurple300() }
func frameYellow() color.RGBA { return frameMustard() }
func frameBlue() color.RGBA   { return MaterialBlue600() }
func frameGreen() color.RGBA  { return MaterialGreen() }
func frameGray() color.RGBA   { return MaterialGrey600() }

// Body content colours.
func liveBestLapColor() color.RGBA { return MaterialDeepPurpleA400() } // best-lap time
func liveDarkText() color.RGBA     { return MaterialGrey900() }        // text on a light fill
func liveFuelPitBG() color.RGBA    { return MaterialYellow700() }      // "pit this lap" fill
func liveFuelLowBG() color.RGBA    { return MaterialRed800() }         // "insufficient fuel" fill

// Tyre temperature ramp endpoints (blue cold -> grey optimal -> red hot).
func tyreColdColor() color.RGBA    { return MaterialBlue600() }
func tyreOptimalColor() color.RGBA { return MaterialGrey400() }
func tyreHotColor() color.RGBA     { return MaterialRed700() }

// Dashboard colours. The rev-arc ramp runs blue (below the rev lights) through
// yellow to red (rev limit); the brake and throttle bars use a bright shade for
// the output and a darker shade for the input-beyond-output delta. Alpha is
// ignored by the panel, so each shade must differ in RGB, not transparency.
func dashColorBlue() color.RGBA   { return MaterialBlue600() }
func dashColorYellow() color.RGBA { return MaterialYellow600() }
func dashColorRed() color.RGBA    { return MaterialRed600() }

func dashColorBrake() color.RGBA         { return MaterialRed300() }
func dashColorBrakeDelta() color.RGBA    { return MaterialRed800() }
func dashColorThrottle() color.RGBA      { return MaterialGreenA200() }
func dashColorThrottleDelta() color.RGBA { return MaterialGreen600() }

func dashColorBackground() color.RGBA      { return blackColor() }
func dashColorBackgroundFlash() color.RGBA { return dashFlashRed() }

// Dashboard text: bright for primary readouts, dim mid-grey for secondary, dark
// grey for the dimmed "ready" skeleton. All fully opaque so they composite over
// the coloured flash background.
func dashColorText() color.RGBA    { return MaterialGrey200() }
func dashColorReady() color.RGBA   { return MaterialGrey800() }
func dashColorTextDim() color.RGBA { return MaterialGrey() }
