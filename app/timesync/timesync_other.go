//go:build !linux

package timesync

// platformStatus always reports StateUnknown on non-Linux platforms. Only
// Linux exposes adjtimex, so other platforms do not gate on clock sync.
func platformStatus() State {
	return StateUnknown
}
