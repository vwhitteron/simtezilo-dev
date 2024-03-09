package main

import (
	"fmt"
	"image/png"
	"log"
	"os"
	"time"

	ST7789 "github.com/manx98/go-st7789"
	"github.com/stianeikeland/go-rpio/v4"
)

// displayPNG
//
//	@Description: 显示GIF图片
//	@param ctx
//	@param canvas 画布
//	@param filePath GIF路径
func displayPNG(canvas *ST7789.Canvas, filePath string) {
	f, err := os.OpenFile(filePath, os.O_RDONLY, os.ModePerm)
	if err != nil {
		log.Fatalf("failed to open: %v", err)
	}
	defer func() {
		if err = f.Close(); err != nil {
			log.Fatalf("failed to close: %v", err)
		}
	}()
	all, err := png.Decode(f)
	if err != nil {
		log.Fatalf("failed to decode: %v", err)
	}

	fmt.Println("Displaying image...")
	canvas.DrawImage(all)
	canvas.Flush()
	fmt.Println("Image displayed!")
}

type MyPin struct {
	rpio.Pin
}

func (m *MyPin) SetOutput() {
	m.Mode(rpio.Output)
}

type MySpi struct {
}

func (m *MySpi) SpiSpeed(speed uint32) {
	rpio.SpiSpeed(int(speed))
}

func (m *MySpi) SetSpiMode3() {
	rpio.SpiMode(1, 1)
}

func (m *MySpi) SpiTransmit(data []byte) {
	rpio.SpiTransmit(data...)
}

func main() {
	if err := rpio.Open(); err != nil {
		log.Fatalf("failed to open rpio: %v", err)
	}
	defer func() {
		if err := rpio.Close(); err != nil {
			log.Fatalf("failed to close gpio: %v", err)
		}
	}()

	fmt.Println("Initialized, waiting for button press")

	// buttonA := rpio.Pin(5)
	// lastResA := buttonA.Read()
	// for {
	// 	resA := buttonA.Read()

	// 	if resA == lastResA {
	// 		continue
	// 	}

	// 	lastResA = resA

	// 	fmt.Printf("Button A: %+v\n", resA)
	// 	time.Sleep(5 * time.Millisecond)
	// }

	err := rpio.SpiBegin(rpio.Spi0)
	if err != nil {
		log.Fatalf("failed to begin gpio: %v", err)
	}
	device := ST7789.NewST7789(
		&MySpi{},
		&MyPin{rpio.Pin(25)},
		&MyPin{rpio.Pin(27)},
		&MyPin{rpio.Pin(24)},
		ST7789.Screen240X240,
	)
	canvas := device.GetFullScreenCanvas()
	displayPNG(canvas, "./assets/pictures/testimage.png")

	time.Sleep(5 * time.Second)
	fmt.Println("Cleaning up")

	canvas.Clear()
	canvas.Flush()
}
