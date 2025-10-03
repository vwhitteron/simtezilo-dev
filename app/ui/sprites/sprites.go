package sprites

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
)

type SpriteName string

const (
	SplashSprite SpriteName = "splash"
	ErrorSprite  SpriteName = "error"
)

type SpriteSet struct {
	image  *image.RGBA
	sprite map[SpriteName]image.Rectangle
}

//go:embed sprites.png
var spritesImage []byte

func NewSpriteSet() (*SpriteSet, error) {
	data := bytes.NewReader(spritesImage)

	img, _, err := image.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	return &SpriteSet{
		image:  img.(*image.RGBA),
		sprite: spriteMap(),
	}, nil
}

func (s *SpriteSet) GetSprite(name SpriteName) image.Image {
	if _, ok := s.sprite[name]; !ok {
		name = ErrorSprite
	}

	rect := s.sprite[name]
	subImage := s.image.SubImage(rect).(*image.RGBA)

	// Create a new image with bounds starting at (0,0)
	repositionedImage := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))

	// Copy the pixels from the subimage to the new image
	for y := range rect.Dy() {
		for x := range rect.Dx() {
			repositionedImage.Set(x, y, subImage.At(rect.Min.X+x, rect.Min.Y+y))
		}
	}

	return repositionedImage
}

func spriteMap() map[SpriteName]image.Rectangle {
	return map[SpriteName]image.Rectangle{
		SplashSprite: image.Rect(0*240, 0*240, 1*240, 1*240),
		ErrorSprite:  image.Rect(1*240, 0*240, 2*240, 1*240),
	}
}
