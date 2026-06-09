// Package icons is the single source of the application's SVG icons. The assets
// are embedded here and shared by the web UI (which serves the raw SVG) and the
// device gui (which rasterises them). Storing them once avoids duplicate copies
// or symlinks that go:embed cannot follow.
package icons

import (
	"embed"
	"fmt"
	"image"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

//go:embed *.svg
var files embed.FS

// FS returns the embedded icon filesystem. Filenames are "<name>.svg".
func FS() embed.FS {
	return files
}

// ReadFile returns the raw SVG bytes for the named file (e.g. "fan.svg").
func ReadFile(filename string) ([]byte, error) {
	return files.ReadFile(filename)
}

// Render rasterises the named icon (without the ".svg" extension) to a square,
// non-anti-aliased alpha mask of the given pixel size. The mask is colour-neutral:
// callers tint it at draw time via draw.DrawMask. Rasterising is cheap, but the
// result is intended to be rendered once and cached by the caller.
func Render(name string, size int) (*image.Alpha, error) {
	data, err := files.ReadFile(name + ".svg")
	if err != nil {
		return nil, fmt.Errorf("read icon %q: %w", name, err)
	}

	viewBox, pathData, err := parseSVG(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse icon %q: %w", name, err)
	}

	scale := float32(size) / viewBox

	rasterizer := vector.NewRasterizer(size, size)
	if err := buildPath(rasterizer, pathData, scale); err != nil {
		return nil, fmt.Errorf("build icon %q: %w", name, err)
	}

	mask := image.NewAlpha(image.Rect(0, 0, size, size))
	rasterizer.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})

	// Threshold the coverage to a hard edge (no anti-aliasing).
	for i, coverage := range mask.Pix {
		if coverage >= 128 {
			mask.Pix[i] = 255
		} else {
			mask.Pix[i] = 0
		}
	}

	return mask, nil
}

// parseSVG extracts the (square) viewBox dimension and the first path's data.
func parseSVG(svg string) (viewBox float32, pathData string, err error) {
	box, err := attribute(svg, "viewBox")
	if err != nil {
		return 0, "", err
	}

	fields := strings.Fields(box)
	if len(fields) != 4 {
		return 0, "", fmt.Errorf("unexpected viewBox %q", box)
	}

	width, err := strconv.ParseFloat(fields[2], 32)
	if err != nil {
		return 0, "", fmt.Errorf("parse viewBox width: %w", err)
	}

	pathData, err = attribute(svg, "d")
	if err != nil {
		return 0, "", err
	}

	return float32(width), pathData, nil
}

// attribute returns the value of the first name="..." attribute.
func attribute(svg, name string) (string, error) {
	marker := name + `="`

	start := strings.Index(svg, marker)
	if start < 0 {
		return "", fmt.Errorf("missing %q attribute", name)
	}

	start += len(marker)

	end := strings.IndexByte(svg[start:], '"')
	if end < 0 {
		return "", fmt.Errorf("unterminated %q attribute", name)
	}

	return svg[start : start+end], nil
}

// buildPath feeds an SVG path's commands to the rasteriser, scaling coordinates.
// It supports the Font Awesome subset (M L H V C and Z, absolute and relative)
// and errors on anything else rather than rendering it wrong.
func buildPath(rasterizer *vector.Rasterizer, pathData string, scale float32) error {
	tokens := tokenizePath(pathData)

	var current, start [2]float32

	index := 0
	number := func() float32 {
		value, _ := strconv.ParseFloat(tokens[index], 32)
		index++

		return float32(value) * scale
	}

	isNumber := func() bool {
		if index >= len(tokens) {
			return false
		}

		first := tokens[index][0]

		return !((first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z'))
	}

	command := ""

	for index < len(tokens) {
		if !isNumber() {
			command = tokens[index]
			index++
		}

		switch command {
		case "M":
			current[0], current[1] = number(), number()
			start = current
			rasterizer.MoveTo(current[0], current[1])

			command = "L" // subsequent coordinate pairs are implicit line-tos
		case "m":
			current[0] += number()
			current[1] += number()
			start = current
			rasterizer.MoveTo(current[0], current[1])

			command = "l"
		case "L":
			current[0], current[1] = number(), number()
			rasterizer.LineTo(current[0], current[1])
		case "l":
			current[0] += number()
			current[1] += number()
			rasterizer.LineTo(current[0], current[1])
		case "H":
			current[0] = number()
			rasterizer.LineTo(current[0], current[1])
		case "h":
			current[0] += number()
			rasterizer.LineTo(current[0], current[1])
		case "V":
			current[1] = number()
			rasterizer.LineTo(current[0], current[1])
		case "v":
			current[1] += number()
			rasterizer.LineTo(current[0], current[1])
		case "C":
			x1, y1 := number(), number()
			x2, y2 := number(), number()
			current[0], current[1] = number(), number()
			rasterizer.CubeTo(x1, y1, x2, y2, current[0], current[1])
		case "c":
			x1, y1 := current[0]+number(), current[1]+number()
			x2, y2 := current[0]+number(), current[1]+number()
			next0, next1 := current[0]+number(), current[1]+number()
			rasterizer.CubeTo(x1, y1, x2, y2, next0, next1)
			current[0], current[1] = next0, next1
		case "Z", "z":
			rasterizer.ClosePath()

			current = start
		default:
			return fmt.Errorf("unsupported path command %q", command)
		}
	}

	return nil
}

// tokenizePath splits SVG path data into command letters and number tokens.
func tokenizePath(pathData string) []string {
	var tokens []string

	i := 0
	for i < len(pathData) {
		char := pathData[i]

		switch {
		case char == ' ' || char == ',' || char == '\t' || char == '\n' || char == '\r':
			i++
		case (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z'):
			tokens = append(tokens, string(char))
			i++
		default:
			j := i
			if pathData[j] == '+' || pathData[j] == '-' {
				j++
			}

			for j < len(pathData) {
				c := pathData[j]
				switch {
				case (c >= '0' && c <= '9') || c == '.':
					j++
				case c == 'e' || c == 'E':
					j++
					if j < len(pathData) && (pathData[j] == '+' || pathData[j] == '-') {
						j++
					}
				default:
					goto done
				}
			}

		done:
			tokens = append(tokens, pathData[i:j])

			i = j
		}
	}

	return tokens
}
