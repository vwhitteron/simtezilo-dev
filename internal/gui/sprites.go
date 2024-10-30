package gui

import (
	"fmt"
	"image"
	"os"
)

type SpriteSet struct {
	image  *image.RGBA
	sprite map[string]image.Rectangle
}

type SpriteSetOpts struct {
	AssetDir string
}

func NewSpriteSet(opts SpriteSetOpts) (*SpriteSet, error) {
	path := opts.AssetDir + "/image/sprites.png"
	data, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("loading image %q: %e", path, err)
	}

	img, _, err := image.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decoding image %q: %e", path, err)
	}
	data.Close()

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
