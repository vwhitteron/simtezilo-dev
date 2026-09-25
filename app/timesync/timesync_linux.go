//go:build linux

package timesync

import "golang.org/x/sys/unix"

// platformStatus reports the kernel clock synchronisation state using
// adjtimex(2).
func platformStatus() State {
	var buf unix.Timex

	state, err := unix.Adjtimex(&buf)
	if err != nil {
		return StateUnknown
	}

	if state == unix.TIME_ERROR {
		return StateUnsynced
	}

	// STA_UNSYNC is cleared by systemd-timesyncd once the clock has been
	// disciplined by NTP.
	if buf.Status&int32(unix.STA_UNSYNC) != 0 {
		return StateUnsynced
	}

	return StateSynced
}
