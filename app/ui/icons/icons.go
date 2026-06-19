// Package icons is the single source of the application's SVG icons. The assets
// are embedded here and shared by the web UI (which serves the raw SVG) and the
// device gui (which rasterises them). Storing them once avoids duplicate copies
// or symlinks that go:embed cannot follow.
package icons

import (
	"embed"
	"fmt"
	"image"
	"math"
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
// non-anti-aliased alpha mask of the given pixel size, scaled by the viewBox
// width. The mask is colour-neutral: callers tint it at draw time via
// draw.DrawMask. Rasterising is cheap, but the result is intended to be rendered
// once and cached by the caller.
func Render(name string, size int) (*image.Alpha, error) {
	box, pathData, err := loadSVG(name)
	if err != nil {
		return nil, err
	}

	scale := float32(size) / box.w

	mask, err := rasterize(name, pathData, scale, -box.minX*scale, -box.minY*scale, size, size)
	if err != nil {
		return nil, err
	}

	threshold(mask)

	return mask, nil
}

// RenderFit rasterises the named icon scaled to fit within a w×h mask while
// preserving the viewBox aspect ratio, with the icon centred and any spare space
// left transparent. The returned mask is exactly w×h, so callers get a uniform
// canvas regardless of the icon's native aspect (e.g. a wide glyph and a square
// glyph both yield the same size). Unlike Render, the mask keeps the rasteriser's
// anti-aliased edge coverage (0-255) so callers that target a colour display can
// blend smoothly; pack it with fancontroller.AlphaToRGB565LE.
func RenderFit(name string, width, height int) (*image.Alpha, error) {
	box, pathData, err := loadSVG(name)
	if err != nil {
		return nil, err
	}

	scale := float32(width) / box.w
	if s := float32(height) / box.h; s < scale {
		scale = s
	}

	// Centre the scaled art within the canvas.
	tx := (float32(width)-box.w*scale)/2 - box.minX*scale
	ty := (float32(height)-box.h*scale)/2 - box.minY*scale

	return rasterize(name, pathData, scale, tx, ty, width, height)
}

// rasterize builds the path under the given affine (uniform scale then x/y
// translate) into a width×height alpha mask holding the rasteriser's raw,
// anti-aliased coverage (0-255). Callers that want a hard 1-bit edge apply
// threshold.
func rasterize(name, pathData string, scale, tx, ty float32, width, height int) (*image.Alpha, error) {
	rasterizer := vector.NewRasterizer(width, height)

	err := buildPath(rasterizer, pathData, scale, tx, ty)
	if err != nil {
		return nil, fmt.Errorf("build icon %q: %w", name, err)
	}

	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	rasterizer.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})

	return mask, nil
}

// threshold collapses a coverage mask to a hard 1-bit edge in place: any pixel
// with at least half coverage becomes fully set, the rest fully clear. It is used
// by Render for the monochrome device gui, which tints a 1-bit mask.
func threshold(mask *image.Alpha) {
	for i, coverage := range mask.Pix {
		if coverage >= 128 {
			mask.Pix[i] = 255
		} else {
			mask.Pix[i] = 0
		}
	}
}

// viewBox holds the four parsed viewBox values: top-left origin and dimensions.
type viewBox struct {
	minX, minY, w, h float32
}

// loadSVG reads the named icon and extracts its viewBox and first path's data.
func loadSVG(name string) (viewBox, string, error) {
	data, err := files.ReadFile(name + ".svg")
	if err != nil {
		return viewBox{}, "", fmt.Errorf("read icon %q: %w", name, err)
	}

	box, pathData, err := parseSVG(string(data))
	if err != nil {
		return viewBox{}, "", fmt.Errorf("parse icon %q: %w", name, err)
	}

	return box, pathData, nil
}

// parseSVG extracts the full viewBox (origin and dimensions) and the first
// path's data.
func parseSVG(svg string) (viewBox, string, error) {
	box, err := attribute(svg, "viewBox")
	if err != nil {
		return viewBox{}, "", err
	}

	fields := strings.Fields(box)
	if len(fields) != 4 {
		return viewBox{}, "", fmt.Errorf("unexpected viewBox %q", box)
	}

	var values [4]float32

	for idx, field := range fields {
		v, parseErr := strconv.ParseFloat(field, 32)
		if parseErr != nil {
			return viewBox{}, "", fmt.Errorf("parse viewBox field %d: %w", idx, parseErr)
		}

		values[idx] = float32(v)
	}

	pathData, err := attribute(svg, "d")
	if err != nil {
		return viewBox{}, "", err
	}

	return viewBox{minX: values[0], minY: values[1], w: values[2], h: values[3]}, pathData, nil
}

// attribute returns the value of the first name="..." attribute. The match
// requires a word boundary before name so that, e.g., looking up "d" does not
// match the "d=" inside an unrelated attribute such as id="...".
func attribute(svg, name string) (string, error) {
	marker := name + `="`

	search := 0

	var start int

	for {
		idx := strings.Index(svg[search:], marker)
		if idx < 0 {
			return "", fmt.Errorf("missing %q attribute", name)
		}

		start = search + idx

		// Accept only when the marker starts an attribute name: preceded by
		// whitespace or the element's opening "<tag" boundary, not by a letter.
		if start == 0 || !isNameByte(svg[start-1]) {
			break
		}

		search = start + len(marker)
	}

	start += len(marker)

	end := strings.IndexByte(svg[start:], '"')
	if end < 0 {
		return "", fmt.Errorf("unterminated %q attribute", name)
	}

	return svg[start : start+end], nil
}

// isNameByte reports whether b can appear inside an XML attribute/element name,
// used to enforce a word boundary before an attribute marker.
func isNameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '-' || b == '_' || b == ':'
}

// buildPath feeds an SVG path's commands to the rasteriser under an affine
// transform: each coordinate is multiplied by scale, then absolute coordinates
// are shifted by (tx, ty). It supports M L H V C S Q T A and Z (absolute and
// relative); arcs are flattened to cubics and quadratics use QuadTo. It errors on
// anything else rather than rendering it wrong.
//
//nolint:cyclop,varnamelen,maintidx // a one-switch SVG command dispatch is inherently branchy; x1/y1/cx style names are the standard for path coordinates.
func buildPath(rasterizer *vector.Rasterizer, pathData string, scale, tx, ty float32) error {
	tokens := tokenizePath(pathData)

	var current, start, ctrl [2]float32

	// ctrlKind records the curve type that set ctrl, so S/T can reflect the right
	// previous control point ('C' = cubic, 'Q' = quadratic, 0 = none).
	var ctrlKind byte

	index := 0
	// raw returns the next token unscaled (for arc radii rotation/flags).
	raw := func() float64 {
		value, _ := strconv.ParseFloat(tokens[index], 32)
		index++

		return value
	}
	// delta returns the next token scaled (no translate) — for relative offsets.
	delta := func() float32 { return float32(raw()) * scale }
	// absX/absY return the next token scaled and translated — for absolute coords.
	absX := func() float32 { return delta() + tx }
	absY := func() float32 { return delta() + ty }

	// reflect returns the control point for a smooth curve: the reflection of the
	// previous control point about the current point when the previous command was
	// the matching curve type, else the current point itself.
	reflect := func(kind byte) (float32, float32) {
		if ctrlKind == kind {
			return 2*current[0] - ctrl[0], 2*current[1] - ctrl[1]
		}

		return current[0], current[1]
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
			current[0], current[1] = absX(), absY()
			start = current
			rasterizer.MoveTo(current[0], current[1])

			command = "L" // subsequent coordinate pairs are implicit line-tos
		case "m":
			current[0] += delta()
			current[1] += delta()
			start = current
			rasterizer.MoveTo(current[0], current[1])

			command = "l"
		case "L":
			current[0], current[1] = absX(), absY()
			rasterizer.LineTo(current[0], current[1])
		case "l":
			current[0] += delta()
			current[1] += delta()
			rasterizer.LineTo(current[0], current[1])
		case "H":
			current[0] = absX()
			rasterizer.LineTo(current[0], current[1])
		case "h":
			current[0] += delta()
			rasterizer.LineTo(current[0], current[1])
		case "V":
			current[1] = absY()
			rasterizer.LineTo(current[0], current[1])
		case "v":
			current[1] += delta()
			rasterizer.LineTo(current[0], current[1])
		case "C":
			x1, y1 := absX(), absY()
			x2, y2 := absX(), absY()
			current[0], current[1] = absX(), absY()
			rasterizer.CubeTo(x1, y1, x2, y2, current[0], current[1])
			ctrl, ctrlKind = [2]float32{x2, y2}, 'C'
		case "c":
			x1, y1 := current[0]+delta(), current[1]+delta()
			x2, y2 := current[0]+delta(), current[1]+delta()
			next0, next1 := current[0]+delta(), current[1]+delta()
			rasterizer.CubeTo(x1, y1, x2, y2, next0, next1)
			current[0], current[1] = next0, next1
			ctrl, ctrlKind = [2]float32{x2, y2}, 'C'
		case "S":
			x1, y1 := reflect('C')
			x2, y2 := absX(), absY()
			current[0], current[1] = absX(), absY()
			rasterizer.CubeTo(x1, y1, x2, y2, current[0], current[1])
			ctrl, ctrlKind = [2]float32{x2, y2}, 'C'
		case "s":
			x1, y1 := reflect('C')
			x2, y2 := current[0]+delta(), current[1]+delta()
			next0, next1 := current[0]+delta(), current[1]+delta()
			rasterizer.CubeTo(x1, y1, x2, y2, next0, next1)
			current[0], current[1] = next0, next1
			ctrl, ctrlKind = [2]float32{x2, y2}, 'C'
		case "Q":
			cx, cy := absX(), absY()
			current[0], current[1] = absX(), absY()
			rasterizer.QuadTo(cx, cy, current[0], current[1])
			ctrl, ctrlKind = [2]float32{cx, cy}, 'Q'
		case "q":
			cx, cy := current[0]+delta(), current[1]+delta()
			next0, next1 := current[0]+delta(), current[1]+delta()
			rasterizer.QuadTo(cx, cy, next0, next1)
			current[0], current[1] = next0, next1
			ctrl, ctrlKind = [2]float32{cx, cy}, 'Q'
		case "T":
			cx, cy := reflect('Q')
			current[0], current[1] = absX(), absY()
			rasterizer.QuadTo(cx, cy, current[0], current[1])
			ctrl, ctrlKind = [2]float32{cx, cy}, 'Q'
		case "t":
			cx, cy := reflect('Q')
			next0, next1 := current[0]+delta(), current[1]+delta()
			rasterizer.QuadTo(cx, cy, next0, next1)
			current[0], current[1] = next0, next1
			ctrl, ctrlKind = [2]float32{cx, cy}, 'Q'
		case "A", "a":
			rx, ry := float32(raw())*scale, float32(raw())*scale
			phi := float32(raw())
			largeArc, sweep := raw() != 0, raw() != 0

			var ex, ey float32
			if command == "A" {
				ex, ey = absX(), absY()
			} else {
				ex, ey = current[0]+delta(), current[1]+delta()
			}

			segs := arcToCubics(current[0], current[1], rx, ry, phi, largeArc, sweep, ex, ey)
			if len(segs) == 0 {
				rasterizer.LineTo(ex, ey)
			}

			for _, s := range segs {
				rasterizer.CubeTo(s[0], s[1], s[2], s[3], s[4], s[5])
			}

			current[0], current[1] = ex, ey
			ctrlKind = 0
		case "Z", "z":
			rasterizer.ClosePath()

			current = start
			ctrlKind = 0
		default:
			return fmt.Errorf("unsupported path command %q", command)
		}

		if command == "M" || command == "m" || command == "L" || command == "l" ||
			command == "H" || command == "h" || command == "V" || command == "v" {
			ctrlKind = 0
		}
	}

	return nil
}

// arcToCubics flattens one SVG elliptical-arc segment into cubic Béziers using
// the endpoint-to-centre parameterisation from the SVG 1.1 implementation notes
// (appendix F.6). It returns {x1,y1,x2,y2,x,y} segments in the same coordinate
// space as its inputs, or nil for a degenerate (zero-radius) arc, which the
// caller draws as a straight line.
//
//nolint:cyclop,varnamelen // follows the SVG 1.1 F.6 arc maths verbatim, whose short coordinate/radius names (x0/rx/cx) aid cross-checking against the spec.
func arcToCubics(x0f, y0f, rxf, ryf, phiDeg float32, largeArc, sweep bool, xf, yf float32) [][6]float32 {
	x0, y0 := float64(x0f), float64(y0f)
	x1, y1 := float64(xf), float64(yf)

	rx, ry := math.Abs(float64(rxf)), math.Abs(float64(ryf))
	if rx == 0 || ry == 0 {
		return nil
	}

	phi := float64(phiDeg) * math.Pi / 180
	cosP, sinP := math.Cos(phi), math.Sin(phi)

	// Step 1: midpoint in the rotated frame.
	dx, dy := (x0-x1)/2, (y0-y1)/2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy

	// Scale radii up if they are too small to span the chord.
	if lambda := (x1p*x1p)/(rx*rx) + (y1p*y1p)/(ry*ry); lambda > 1 {
		scale := math.Sqrt(lambda)
		rx, ry = rx*scale, ry*scale
	}

	// Step 2: centre in the rotated frame.
	rxsq, rysq := rx*rx, ry*ry
	num := rxsq*rysq - rxsq*y1p*y1p - rysq*x1p*x1p
	den := rxsq*y1p*y1p + rysq*x1p*x1p

	factor := 0.0
	if den != 0 {
		factor = math.Sqrt(math.Max(0, num/den))
	}

	if largeArc == sweep {
		factor = -factor
	}

	cxp := factor * rx * y1p / ry
	cyp := factor * -ry * x1p / rx

	// Step 3: centre in the original frame.
	cx := cosP*cxp - sinP*cyp + (x0+x1)/2
	cy := sinP*cxp + cosP*cyp + (y0+y1)/2

	// Step 4: start angle and sweep.
	theta1 := vectorAngle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	dtheta := vectorAngle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)

	switch {
	case !sweep && dtheta > 0:
		dtheta -= 2 * math.Pi
	case sweep && dtheta < 0:
		dtheta += 2 * math.Pi
	}

	// Split into <=90° arcs, each approximated by one cubic.
	segments := int(math.Ceil(math.Abs(dtheta) / (math.Pi / 2)))
	if segments == 0 {
		segments = 1
	}

	step := dtheta / float64(segments)
	tan := 4.0 / 3.0 * math.Tan(step/4)

	toReal := func(ux, uy float64) (float32, float32) {
		sx, sy := ux*rx, uy*ry

		return float32(cosP*sx - sinP*sy + cx), float32(sinP*sx + cosP*sy + cy)
	}

	out := make([][6]float32, 0, segments)
	theta := theta1

	for range segments {
		next := theta + step
		cosA, sinA := math.Cos(theta), math.Sin(theta)
		cosB, sinB := math.Cos(next), math.Sin(next)

		c1x, c1y := toReal(cosA-tan*sinA, sinA+tan*cosA)
		c2x, c2y := toReal(cosB+tan*sinB, sinB-tan*cosB)
		ex, ey := toReal(cosB, sinB)

		out = append(out, [6]float32{c1x, c1y, c2x, c2y, ex, ey})
		theta = next
	}

	return out
}

// vectorAngle returns the signed angle (radians) from vector (ux,uy) to (vx,vy).
func vectorAngle(ux, uy, vx, vy float64) float64 {
	dot := ux*vx + uy*vy
	mag := math.Sqrt((ux*ux + uy*uy) * (vx*vx + vy*vy))

	angle := math.Acos(math.Max(-1, math.Min(1, dot/mag)))
	if ux*vy-uy*vx < 0 {
		return -angle
	}

	return angle
}

// tokenizePath splits SVG path data into command letters and number tokens.
//
//nolint:gocognit,cyclop,varnamelen // a character-level scanner is naturally a dense state machine with short cursor names (i/j/c).
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

			seenDot := false

			for j < len(pathData) {
				c := pathData[j]
				switch {
				case c >= '0' && c <= '9':
					j++
				case c == '.':
					// A second '.' starts a new number (SVG shorthand, e.g.
					// "-214.213.275" is two coordinates), so stop this token.
					if seenDot {
						goto done
					}

					seenDot = true
					j++
				case c == 'e' || c == 'E':
					j++
					if j < len(pathData) && (pathData[j] == '+' || pathData[j] == '-') {
						j++
					}

					seenDot = true // an exponent cannot be followed by another '.'
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
