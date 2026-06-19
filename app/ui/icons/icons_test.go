package icons_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/ui/icons"
)

// TestRenderFitFanModeIcons guards the fan-mode icons: each must parse with the
// limited path subset, render to a uniform 50×50 square, and produce a non-empty
// mask.
func TestRenderFitFanModeIcons(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"fan2", "wind-auto", "wind-all"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mask, err := icons.RenderFit(name, 50, 50)
			require.NoError(t, err)

			bounds := mask.Bounds()
			require.Equal(t, 50, bounds.Dx(), "icon should be a 50px square")
			require.Equal(t, 50, bounds.Dy(), "icon should be a 50px square")

			var lit int

			for _, coverage := range mask.Pix {
				if coverage > 0 {
					lit++
				}
			}

			require.Positive(t, lit, "icon should have some set pixels")
		})
	}
}

// TestRenderSquareBackCompat checks the square Render still works for the menu
// icons (full-box Font Awesome glyphs).
func TestRenderSquareBackCompat(t *testing.T) {
	t.Parallel()

	mask, err := icons.Render("wind", 64)
	require.NoError(t, err)
	require.Equal(t, 64, mask.Bounds().Dx())
	require.Equal(t, 64, mask.Bounds().Dy())
}
