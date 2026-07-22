package app

// channelValueAt returns values[index], or 0 when index is out of range.
// Per-channel telemetry slices may be empty (e.g. chassis muted, so the write
// path never sized them). The chassis and texture generators themselves now live
// in app/haptics; this helper stays here because the web-telemetry and haptic
// capture paths read the per-channel telemetry the generator populates.
func channelValueAt(values []float64, index int) float64 {
	if index < 0 || index >= len(values) {
		return 0
	}

	return values[index]
}
