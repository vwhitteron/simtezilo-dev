package font

import (
	"embed"
	"fmt"
	"sync"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

// Font represents a font with its associated scale.
type Font struct {
	Font  *truetype.Font
	Scale float64
}

//go:embed fonts/*
var staticFiles embed.FS

// Parsed fonts are cached process-wide and shared. The embedded font files are
// immutable and *truetype.Font is read-only once parsed (truetype.NewFace only
// reads from it), so handing every caller the same pointer is safe.
var (
	fontCacheMu sync.Mutex                        //nolint:gochecknoglobals // process-wide parsed-font cache
	fontCache   = make(map[string]*truetype.Font) //nolint:gochecknoglobals // process-wide parsed-font cache
)

// GetFont retrieves and parses a TrueType font by name from the embedded
// filesystem. Results are cached, so repeated calls return the same stable
// *truetype.Font pointer rather than re-reading and re-parsing the file each
// time. Returning a stable pointer is essential: downstream face caches key on
// the font pointer, so a fresh pointer per call would both waste CPU and cause
// unbounded face-cache growth.
func GetFont(name string) (*truetype.Font, error) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()

	if f, ok := fontCache[name]; ok {
		return f, nil
	}

	filename := "fonts/" + name

	fontData, err := staticFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read font file %q: %w", filename, err)
	}

	parsed, err := parseTrueTypeFont(fontData)
	if err != nil {
		return nil, err
	}

	fontCache[name] = parsed

	return parsed, nil
}

// parseTrueTypeFont parses the provided byte slice into a TrueType font.
func parseTrueTypeFont(fontBytes []byte) (*truetype.Font, error) {
	freetypeFont, err := freetype.ParseFont(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	return freetypeFont, nil
}
