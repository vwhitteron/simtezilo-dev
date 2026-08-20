//go:build !linux

package audio

// applyRealtime is a no-op on platforms without SCHED_FIFO control.
func applyRealtime(_ int) error {
	return errRealtimeUnsupported
}

// pinThread is a no-op on platforms without CPU affinity control.
func pinThread(_ int) error {
	return errRealtimeUnsupported
}
