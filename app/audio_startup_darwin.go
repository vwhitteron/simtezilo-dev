//go:build darwin

package app

import (
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
)

const (
	audioStartupWarmup = 1000 * time.Millisecond // how long to run the first stream before re-opening
	audioStartupSettle = 300 * time.Millisecond  // how long to run the second stream before starting real haptics
)

// primeAudioOutput works around a macOS-specific defect where the first
// CoreAudio output stream opened produces audio artefacts which can be
// resolved bu re-opening the stream.
func (a *App) primeAudioOutput() {
	// Warm-up: let the just-opened (silent) first stream run so the device wakes.
	time.Sleep(audioStartupWarmup)

	a.log.Info().
		Str("action", "prime").
		Str("backend", audio.BackendPortAudio).
		Str("reason", "fix MacOS first stream artefacts").
		Msg("Audio output")

	a.restartAudioOutput()

	// Settle: let the clean second stream run before real playback starts on it.
	time.Sleep(audioStartupSettle)
}
