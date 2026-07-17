package audio

// ResolveOutputDevice picks the native device ID to open for a saved selection,
// keying off the human-readable device Name — the only identifier common to all
// backends and stable across both a backend switch and portaudio's positional
// index reshuffling — with the native ID as a tiebreaker for duplicate names.
// It returns "" to mean "use the backend default device".
//
// Resolution order:
//   - name matches exactly one device      -> that device's ID
//   - name matches several devices          -> the one whose ID == savedID, else the first
//   - name matches none, savedID still valid -> savedID
//   - otherwise                              -> "" (default device)
//
// If the device list cannot be read it falls back to savedID (best effort).
func ResolveOutputDevice(b Backend, name, savedID string) string {
	if name == "" && savedID == "" {
		return ""
	}

	devices, err := b.ListDevices()
	if err != nil {
		return savedID
	}

	var named []Device

	if name != "" {
		for _, d := range devices {
			if d.Name == name {
				named = append(named, d)
			}
		}
	}

	switch len(named) {
	case 1:
		return named[0].ID
	case 0:
		// Name gone (or unset): honour the stored ID only if it still exists.
		for _, d := range devices {
			if savedID != "" && d.ID == savedID {
				return savedID
			}
		}

		return ""
	default:
		// Duplicate names: disambiguate with the stored native ID when possible.
		for _, d := range named {
			if d.ID == savedID {
				return savedID
			}
		}

		return named[0].ID
	}
}

// DefaultOutputDevice returns the backend's system default output device so
// callers can inspect its DefaultSampleRate. Returns the zero Device and false
// when the backend reports no default or the device list cannot be read.
func DefaultOutputDevice(b Backend) (Device, bool) {
	devices, err := b.ListDevices()
	if err != nil {
		return Device{}, false
	}

	for _, d := range devices {
		if d.IsDefault {
			return d, true
		}
	}

	return Device{}, false
}

// FindOutputDevice resolves the same selection as ResolveOutputDevice but
// returns the matching Device so callers can inspect its DefaultSampleRate.
// Returns the zero Device and false when no device matches or the device list
// cannot be read.
func FindOutputDevice(b Backend, name, savedID string) (Device, bool) {
	devices, err := b.ListDevices()
	if err != nil {
		return Device{}, false
	}

	var named []Device

	if name != "" {
		for _, d := range devices {
			if d.Name == name {
				named = append(named, d)
			}
		}
	}

	switch len(named) {
	case 1:
		return named[0], true
	case 0:
		for _, d := range devices {
			if savedID != "" && d.ID == savedID {
				return d, true
			}
		}

		return Device{}, false
	default:
		for _, d := range named {
			if d.ID == savedID {
				return d, true
			}
		}

		return named[0], true
	}
}
