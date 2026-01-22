package main

// A CLI tool for rendering GUI screens and saving them to disk as PNG files.
// This is useful for generating documentation screenshots and testing screen
// layouts without requiring physical hardware.

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"

	"github.com/skip2/go-qrcode"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/display"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/virtual"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

const (
	displayWidth  = 240
	displayHeight = 240
	displayDPI    = 72.0
)

// generateSetupModeQRCode generates a QR code image for the setup mode screen.
func generateSetupModeQRCode(value string) (image.Image, error) {
	// Build WIFI QR code string format
	// WIFI:S:<SSID>;T:<WPA|WEP|>;P:<password>;H:<true|false>;;
	networkDef := "WIFI:S:" + value + ";T:WPA2;P:password123;H:false;"

	qrCode, err := qrcode.New(networkDef, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("generate QR code: %w", err)
	}

	qrCode.BackgroundColor = image.Black
	qrCode.ForegroundColor = color.Gray{Y: 180}

	return qrCode.Image(displayWidth), nil
}

func main() {
	var (
		outputDir string
		screen    string
		value     string
	)

	flag.StringVar(&outputDir, "output", ".", "Output directory for PNG files")
	flag.StringVar(&screen, "screen", "qrcode", "Screen to render (qrcode)")
	flag.StringVar(&value, "value", "", "Value to render on the screen")
	flag.Parse()

	// Create output directory if it doesn't exist
	err := os.MkdirAll(outputDir, 0o755)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	virtualDisplay := virtual.NewDisplay(displayWidth, displayHeight, displayDPI)

	switch screen {
	case "qrcode":
		err = renderQRCodeScreen(virtualDisplay, value)
	default:
		fmt.Fprintf(os.Stderr, "Unknown screen type: %s\n", screen)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error rendering screen: %v\n", err)
		os.Exit(1)
	}

	// Save the screenshot
	outputPath := filepath.Join(outputDir, screen+".png")

	err = virtualDisplay.SavePNG(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving PNG: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "Screenshot saved to: %s\n", outputPath)
}

func renderQRCodeScreen(virtualDisplay *virtual.Display, value string) error {
	if value == "" {
		value = "simtezilo-a1b2c3"
	}

	// Generate the QR code image
	qrImage, err := generateSetupModeQRCode(value)
	if err != nil {
		return fmt.Errorf("generate QR code: %w", err)
	}

	// Convert to RGBA and write to display
	canvas := gui.ImageToRGBA(qrImage)
	content := &display.Content{
		Canvas: canvas,
	}

	err = virtualDisplay.Write(content)
	if err != nil {
		return fmt.Errorf("write to display: %w", err)
	}

	return nil
}
