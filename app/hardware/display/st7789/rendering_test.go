package st7789_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/display/st7789"
)

// referencePackRGB565 is the DrawRAW byte-packing logic reproduced
// independently using image.Image.At() (interface-based, boxes to color.Color):
// output column c is a horizontal mirror of the source. It is the golden
// reference: packRGB565 must produce byte-for-byte identical output.
func referencePackRGB565(src image.Image, cols, rows int) []byte {
	out := make([]byte, cols*rows*2)
	idx := 0

	for c := range cols {
		srcX := src.Bounds().Min.X + cols - 1 - c
		for r := range rows {
			srcY := src.Bounds().Min.Y + r

			var red, green, blue uint16

			if srcX >= src.Bounds().Min.X && srcX < src.Bounds().Max.X &&
				srcY >= src.Bounds().Min.Y && srcY < src.Bounds().Max.Y {
				col := src.At(srcX, srcY)
				r8, g8, b8, _ := col.RGBA()
				red = uint16(r8>>8) >> 3   //nolint:gosec // r8>>8 is an 8-bit sample, always ≤255
				green = uint16(g8>>8) >> 2 //nolint:gosec // g8>>8 is an 8-bit sample, always ≤255
				blue = uint16(b8>>8) >> 3  //nolint:gosec // b8>>8 is an 8-bit sample, always ≤255
			}
			// Out of bounds → red/green/blue remain 0 (black, 0x0000).

			c565 := (red << 11) | (green << 5) | blue
			out[idx] = byte(c565 >> 8)
			out[idx+1] = byte(c565)
			idx += 2
		}
	}

	return out
}

// makeGradientRGBA returns a cols×rows *image.RGBA filled with a deterministic
// gradient so every pixel is distinct and non-trivial.
func makeGradientRGBA(cols, rows int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, cols, rows))

	for y := range rows {
		for x := range cols {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / (cols - 1)),              //nolint:gosec // small pixel coord
				G: uint8(y * 255 / (rows - 1)),              //nolint:gosec // small pixel coord
				B: uint8((x + y) * 255 / (cols + rows - 2)), //nolint:gosec // small pixel coord
				A: 255,
			})
		}
	}

	return img
}

// TestPackRGB565Golden asserts that packRGB565 produces byte-for-byte identical
// output to the original .At()-based reference for two image sizes.
func TestPackRGB565Golden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cols int
		rows int
	}{
		{"8x8", 8, 8},
		{"240x240", 240, 240},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			src := makeGradientRGBA(testCase.cols, testCase.rows)

			want := referencePackRGB565(src, testCase.cols, testCase.rows)
			got := st7789.PackRGB565(src, testCase.cols, testCase.rows, nil)

			if len(got) != len(want) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
			}

			for i := range want {
				if got[i] != want[i] {
					t.Errorf("byte[%d] mismatch: got 0x%02x, want 0x%02x", i, got[i], want[i])

					if i > 20 {
						t.FailNow()
					}
				}
			}
		})
	}
}

// TestPackRGB565BufferReuse verifies that passing a sufficiently large existing
// buffer avoids reallocation and still returns correct data.
func TestPackRGB565BufferReuse(t *testing.T) {
	t.Parallel()

	src := makeGradientRGBA(8, 8)
	buf := make([]byte, 8*8*2+64) // larger than needed
	orig := buf                   // same backing array

	got := st7789.PackRGB565(src, 8, 8, buf)
	want := referencePackRGB565(src, 8, 8)

	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("byte[%d] mismatch: got 0x%02x, want 0x%02x", i, got[i], want[i])
		}
	}

	// The returned slice should share the same backing array as orig (no realloc).
	if &got[:1][0] != &orig[:1][0] {
		t.Error("expected buffer to be reused, but a new allocation was made")
	}
}

// Benchmark_packRGB565 measures packing throughput for a 240×240 image with
// buffer reuse (the steady-state path on device).
func Benchmark_packRGB565(b *testing.B) {
	src := makeGradientRGBA(240, 240)
	buf := make([]byte, 240*240*2)

	for b.Loop() {
		buf = st7789.PackRGB565(src, 240, 240, buf)
	}
}
