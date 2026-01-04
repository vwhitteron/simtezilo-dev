package ui

const setupModeCountdownStart = 5

type MenuSystem struct {
	currentPage        int
	pages              []string
	setupModeCountdown int
}

func NewMenuSystem() *MenuSystem {
	return &MenuSystem{
		currentPage:        0,
		setupModeCountdown: setupModeCountdownStart,
		pages: []string{
			"vol",
			"cVol",
			"tVol",
			"eVol",
			"vCurve",
			"vSat",
			"fCurve",
			"fSat",
			"fMin",
			"fMax",
			"tCurve",
			"tSat",
			"ePrimary",
			"eSecondary",
			"ePVol",
			"ePScale",
			"lang",
			"setupMode",
			"info",
		},
	}
}

func (m *MenuSystem) NextMenuPage() string {
	m.currentPage = (m.currentPage + 1) % len(m.pages)

	if m.isSetupModePage() {
		_ = m.ResetSetupModeCountdown()
	}

	return m.pages[m.currentPage]
}

func (m *MenuSystem) PreviousMenuPage() string {
	m.currentPage = (m.currentPage - 1 + len(m.pages)) % len(m.pages)

	if m.isSetupModePage() {
		_ = m.ResetSetupModeCountdown()
	}

	return m.pages[m.currentPage]
}

func (m *MenuSystem) GetCurrentMenuPage() string {
	m.currentPage %= len(m.pages)

	return m.pages[m.currentPage]
}

// GetSetupModeCountdown returns the current setup countdown value.
func (m *MenuSystem) GetSetupModeCountdown() int {
	return m.setupModeCountdown
}

// ResetSetupModeCountdown resets the setup countdown to 5.
func (m *MenuSystem) ResetSetupModeCountdown() int {
	m.setupModeCountdown = setupModeCountdownStart

	return m.setupModeCountdown
}

// DecrementSetupModeCountdown decrements the setup countdown by 1.
func (m *MenuSystem) DecrementSetupModeCountdown() int {
	if m.setupModeCountdown > 0 {
		m.setupModeCountdown--
	}

	return m.setupModeCountdown
}

// IsSetupModeCountdownZero returns true if the setup countdown is zero.
func (m *MenuSystem) IsSetupModeCountdownZero() bool {
	return m.setupModeCountdown == 0
}

// isSetupModePage returns true if the current page is the setup page.
func (m *MenuSystem) isSetupModePage() bool {
	return m.pages[m.currentPage] == "setupMode"
}
