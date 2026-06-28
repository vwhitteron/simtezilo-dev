//go:build darwin

package app

import "time"

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

	a.log.Info().Msg("priming haptic audio output (macOS cold-start stream re-open)")

	a.restartAudioOutput()

	// Settle: let the clean second stream run before real playback starts on it.
	time.Sleep(audioStartupSettle)
}
