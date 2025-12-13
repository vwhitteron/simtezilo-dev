package hardware

// This logic is expected to be replaced with embedded hardware identification from EEPROM or similar in future.

const (
	platformLocal     = "local"
	platformSimtezilo = "simtezilo"
)

// Platform represents the hardware platform the application is running on.
type Platform struct {
	name              string
	supportsSetupMode bool
}

// NewPlatform creates a new Platform instance based on the given name.
func NewPlatform(name string) *Platform {
	switch name {
	case platformLocal:
		return &Platform{
			name:              platformLocal,
			supportsSetupMode: true,
		}
	case platformSimtezilo:
		return &Platform{
			name:              platformSimtezilo,
			supportsSetupMode: true,
		}
	default:
		return &Platform{
			name:              name,
			supportsSetupMode: false,
		}
	}
}

// SupportsSetupMode returns true if the platform supports setup mode.
func (p *Platform) SupportsSetupMode() bool {
	return p.supportsSetupMode
}
