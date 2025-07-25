package ui

import (
	_ "embed"
	"fmt"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

//go:embed fonts/LeagueGothic-Regular.ttf
var regularFont []byte

func GetRegularFont() (*truetype.Font, error) {
	return getFont(regularFont)
}

//go:embed fonts/LeagueGothic-Regular.ttf
var italicFont []byte

func GetItalicFont() (*truetype.Font, error) {
	return getFont(italicFont)
}

func getFont(fontBytes []byte) (*truetype.Font, error) {
	freetypeFont, err := freetype.ParseFont(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	return freetypeFont, nil
}
