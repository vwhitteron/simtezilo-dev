package main

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/pirateaudio"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
)

// SineWave represents a sine wave generator
type SineWave struct {
	Freq   float64 // frequency in Hz
	phase  float64 // current phase
	sr     beep.SampleRate
	Volume float64
}

// NewSineWave creates a new sine wave generator
func NewSineWave(sampleRate beep.SampleRate, frequency, volume float64) *SineWave {
	return &SineWave{
		Freq:   frequency,
		phase:  0,
		sr:     sampleRate,
		Volume: volume,
	}
}

// Stream generates the sine wave samples
func (s *SineWave) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {

		// Calculate the sine wave value
		sample := volumeToGain(s.Volume) * math.Sin(s.phase)

		// Output to both left and right channels (stereo)
		samples[i][0] = sample
		samples[i][1] = sample

		// Increment phase for next sample
		// phase increment = 2π * frequency / sample_rate
		s.phase += 2 * math.Pi * s.Freq / float64(s.sr)

		// Keep phase in reasonable range to avoid floating point precision issues
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}
	}

	return len(samples), true
}

// Err returns any error (none for infinite sine wave)
func (s *SineWave) Err() error {
	return nil
}

// SetFrequency changes the frequency of the sine wave
func (s *SineWave) SetFrequency(freq float64) {
	if freq < 5 {
		freq = 5 // Minimum frequency to avoid inaudible sound
	}
	if freq > 160 {
		freq = 160 // Maximum frequency to avoid exceeding human hearing range
	}
	s.Freq = freq
}

// SetVolume changes the volume (0.0 to 1.0)
func (s *SineWave) SetVolume(vol float64) {
	if vol >= 0 {
		vol = 0
	}

	s.Volume = vol
}

func (s *SineWave) KeyboardInput(done chan bool) {
	keyboard.Listen(func(key keys.Key) (stop bool, err error) {
		switch key.Code {
		case keys.CtrlC, keys.Escape:
			done <- true

			return true, nil // Return true to stop listener
		case keys.RuneKey:
			if key.String() == "q" {
				done <- true

				return true, nil // Return true to stop listener
			}
		case keys.Up:
			s.SetVolume(s.Volume + 0.25)
		case keys.Down:
			s.SetVolume(s.Volume - 0.25)
		case keys.Left:
			s.SetFrequency(s.Freq - 5)
		case keys.Right:
			s.SetFrequency(s.Freq + 5)
		}

		return false, nil // Return false to continue listening
	})
}

func volumeToGain(volume float64) float64 {
	return math.Pow(10, (volume / 10))
}

func main() {
	// Create a channel to receive OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create a done channel to signal goroutines to stop
	done := make(chan bool)

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger().Level(zerolog.InfoLevel)

	hasDisplay := true

	// Audio parameters
	sampleRate := beep.SampleRate(8000)
	display, err := pirateaudio.NewDisplay(pirateaudio.DisplayOptions{
		Orientation: 0,
	})
	if err != nil {
		fmt.Printf("Failed to initialize display: %v\n", err)
		hasDisplay = false
	}

	i18n := i18n.NewLanguage(
		"en",
		logger.With().Str("component", "i18n").Logger(),
	)

	renderer, err := gui.NewScreen(&gui.Config{
		DisplayDevice: display,
		I18n:          i18n,
	})
	if err != nil {
		logger.Error().
			Err(err).
			Str("component", "gui").
			Str("sub-component", "renderer").
			Str("result", "failure").
			Msg("init")
	}

	// Initialize the speaker
	err = speaker.Init(sampleRate, sampleRate.N(time.Second/10))
	if err != nil {
		fmt.Printf("Failed to initialize speaker: %v\n", err)
		return
	}

	fmt.Println("Sine Wave Generator")
	fmt.Println("==================")
	fmt.Println("Press Ctrl+C to stop")

	// Create a sine wave generator
	sineWave := NewSineWave(sampleRate, 30.0, -21.0)

	// Play the sine wave
	speaker.Play(sineWave)
	go sineWave.KeyboardInput(done)

	// Demonstrate frequency changes
	go func() {
		lastFreq := 0
		lastVolume := 0.0

		for {
			select {
			case <-done:
				return
			default:
				if lastFreq != int(sineWave.Freq) || lastVolume != sineWave.Volume {
					fmt.Printf("%.0f Hz  %2.2f dB  %0.04f\n", sineWave.Freq, sineWave.Volume, volumeToGain(sineWave.Volume))

					if hasDisplay {
						value := fmt.Sprintf("%0.0f Hz\n%2.2f dB", sineWave.Freq, sineWave.Volume)
						renderer.RenderLiveScreen(value)
					}
				}
				lastFreq = int(sineWave.Freq)
				lastVolume = sineWave.Volume
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	select {
	case <-done:
		logger.Info().Str("signal", "done").Msg("stopping")
		break
	case <-sigChan:
		logger.Info().Str("signal", "interrupt").Msg("stopping")
		break
	}

	// Signal goroutines to stop
	close(done)

	// Graceful shutdown
	speaker.Clear()
	if hasDisplay {
		// Show shutdown message clear and power off the display
		renderer.RenderLiveScreen("Goodbye!")
		time.Sleep(500 * time.Millisecond) // Brief pause to show message
		display.Clear()
		display.PowerOff()
	}

	logger.Info().Msg("Goodbye!")
	os.Exit(0)
}
