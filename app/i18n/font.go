package i18n

import (
	"embed"
	"fmt"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

//go:embed fonts/*
var staticFiles embed.FS

func GetFont(name string) (*truetype.Font, error) {
	filename := "fonts/" + name

	fontData, err := staticFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("get font regular %q: %w", filename, err)
	}

	return parseFontData(fontData)
}

func parseFontData(fontBytes []byte) (*truetype.Font, error) {
	freetypeFont, err := freetype.ParseFont(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	return freetypeFont, nil
}
