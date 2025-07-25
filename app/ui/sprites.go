package ui

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
)

type SpriteSet struct {
	image  *image.RGBA
	sprite map[string]image.Rectangle
}

//go:embed images/sprites.png
var spritesImage []byte

func NewSpriteSet() (*SpriteSet, error) {
	data := bytes.NewReader(spritesImage)

	img, _, err := image.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decoding image: %e", err)
	}

	collection := map[string]image.Rectangle{
		"splash": image.Rect(0*240, 0*240, 1*240, 1*240),
		"error":  image.Rect(1*240, 0*240, 2*240, 1*240),
	}

	return &SpriteSet{
		image:  img.(*image.RGBA),
		sprite: collection,
	}, nil
}

func (s *SpriteSet) GetSprite(name string) image.Image {
	if _, ok := s.sprite[name]; !ok {
		name = "error"
	}

	return s.image.SubImage(s.sprite[name])
}
