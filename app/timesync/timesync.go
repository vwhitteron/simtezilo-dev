// Package timesync reports whether the kernel clock is disciplined by NTP.
//
// On hardware without a real-time clock (such as a Raspberry Pi), the
// system clock is stale at boot until a time-sync daemon such as
// systemd-timesyncd corrects it. Callers that depend on an accurate clock
// (for example, TLS certificate validation) can use this package to defer
// work until the kernel reports the clock as synchronised.
package timesync

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// State represents the synchronisation state of the system clock.
type State int

const (
	// StateUnknown means the synchronisation state could not be determined.
	StateUnknown State = iota

	// StateUnsynced means the kernel clock is not disciplined by NTP.
	StateUnsynced

	// StateSynced means the kernel clock is disciplined by NTP.
	StateSynced
)

// String returns a human-readable representation of the state.
func (s State) String() string {
	switch s {
	case StateUnknown:
		return "unknown"
	case StateUnsynced:
		return "unsynced"
	case StateSynced:
		return "synced"
	default:
		return "unknown"
	}
}

// statusFn is the platform status lookup, overridable in tests.
//
//nolint:gochecknoglobals // deliberately package-level so tests can inject a fake status
var statusFn = platformStatus

// Status returns the current synchronisation state of the system clock.
func Status() State {
	return statusFn()
}

// Wait blocks until the system clock is synchronised, the timeout elapses,
// or ctx is cancelled, whichever happens first. It returns the resulting
// state.
func Wait(ctx context.Context, timeout, interval time.Duration, log zerolog.Logger) State {
	state := Status()
	if state == StateSynced || state == StateUnknown {
		return state
	}

	if timeout <= 0 || interval <= 0 {
		return state
	}

	log.Info().Dur("timeout", timeout).Msg("System clock is not synchronised, waiting for NTP")

	start := time.Now()

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if Status() == StateSynced {
				log.Info().Dur("waited", time.Since(start)).Msg("System clock synchronised")

				return StateSynced
			}
		case <-timeoutTimer.C:
			log.Warn().Dur("waited", time.Since(start)).Msg("Timed out waiting for clock synchronisation")

			return StateUnsynced
		case <-ctx.Done():
			return StateUnsynced
		}
	}
}
