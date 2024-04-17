package internal

import (
	"fmt"
	"image"
	"os"
)

type spriteSet struct {
	image  *image.RGBA
	sprite map[string]image.Rectangle
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
		// Row 1
		"splash": image.Rect(0*240, 0*240, 1*240, 1*240),
		"error":  image.Rect(1*240, 0*240, 2*240, 1*240),
		"gear1":  image.Rect(2*240, 0*240, 3*240, 1*240),
		"gear2":  image.Rect(3*240, 0*240, 4*240, 1*240),
		"gear3":  image.Rect(4*240, 0*240, 5*240, 1*240),
		"gear4":  image.Rect(5*240, 0*240, 6*240, 1*240),
		// Row 2
		"gearN": image.Rect(0*240, 1*240, 1*240, 2*240),
		"gearR": image.Rect(1*240, 1*240, 2*240, 2*240),
		"gear5": image.Rect(2*240, 1*240, 3*240, 2*240),
		"gear6": image.Rect(3*240, 1*240, 4*240, 2*240),
		"gear7": image.Rect(4*240, 1*240, 5*240, 2*240),
		"gear8": image.Rect(5*240, 1*240, 6*240, 2*240),
	}

	fmt.Printf("Sprite image bounds: %+v\n", img.Bounds())

	return &spriteSet{
		image:  img.(*image.RGBA),
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
