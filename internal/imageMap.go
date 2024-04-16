package internal

import (
	"fmt"
	"image"
	"os"
)

type spriteSet struct {
	image     *image.Paletted
	testImage *image.Paletted
	sprite    map[string]image.Rectangle
}

func NewSpriteSet(assetDir string) (*spriteSet, error) {
	path := assetDir + "/image/sprites.png"
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
		"splash": image.Rect(0*240, 0, 1*240, 240),
		"error":  image.Rect(1*240, 0, 2*240, 240),
		"gear1":  image.Rect(2*240, 0, 3*240, 240),
		"gear2":  image.Rect(3*240, 0, 4*240, 240),
		"gear3":  image.Rect(4*240, 0, 5*240, 240),
		"gear4":  image.Rect(5*240, 0, 6*240, 240),
		"gear5":  image.Rect(6*240, 0, 7*240, 240),
		"gear6":  image.Rect(7*240, 0, 8*240, 240),
		"gear7":  image.Rect(8*240, 0, 9*240, 240),
		"gear8":  image.Rect(9*240, 0, 10*240, 240),
	}

	fmt.Printf("Sprite image bounds: %+v\n", img.Bounds())

	return &spriteSet{
		image:  img.(*image.Paletted),
		sprite: collection,
	}, nil
}

func (s *spriteSet) GetSprite(name string) image.Image {
	if _, ok := s.sprite[name]; !ok {
		name = "error"
	}

	img := s.image.SubImage(s.sprite[name])

	fmt.Printf("%q image bounds: %+v\n", name, img.Bounds())

	return img
}
