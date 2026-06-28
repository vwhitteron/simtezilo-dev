//go:build !darwin

package app

// primeAudioOutput is a no-op on non-darwin platforms.
func (a *App) primeAudioOutput() {
	return
}
