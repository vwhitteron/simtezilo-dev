package ui

const setupModeCountdownStart = 5

// PageContext defines when a menu page should be visible.
type PageContext string

const (
	PageContextAlways   PageContext = "always"
	PageContextDevTools PageContext = "devtools"
)

// MenuPage represents a menu page with its visibility context.
type MenuPage struct {
	name    string
	context PageContext
}

type MenuSystem struct {
	currentPage        int
	pages              []MenuPage
	setupModeCountdown int
	devToolsEnabled    func() bool
}

func NewMenuSystem() *MenuSystem {
	return &MenuSystem{
		currentPage:        0,
		setupModeCountdown: setupModeCountdownStart,
		pages: []MenuPage{
			{name: "vol", context: PageContextAlways},
			{name: "cVol", context: PageContextAlways},
			{name: "tVol", context: PageContextAlways},
			{name: "eVol", context: PageContextAlways},
			{name: "eq", context: PageContextAlways},
			{name: "vCurve", context: PageContextAlways},
			{name: "vSat", context: PageContextAlways},
			{name: "fCurve", context: PageContextAlways},
			{name: "fSat", context: PageContextAlways},
			{name: "fMin", context: PageContextAlways},
			{name: "fMax", context: PageContextAlways},
			{name: "tCurve", context: PageContextAlways},
			{name: "tSat", context: PageContextAlways},
			{name: "ePrimary", context: PageContextAlways},
			{name: "eSecondary", context: PageContextAlways},
			{name: "ePVol", context: PageContextAlways},
			{name: "ePScale", context: PageContextAlways},
			{name: "lang", context: PageContextAlways},
			{name: "record", context: PageContextDevTools},
			{name: "setupMode", context: PageContextAlways},
			{name: "info", context: PageContextAlways},
		},
	}
}

func (m *MenuSystem) NextMenuPage() string {
	m.currentPage = (m.currentPage + 1) % len(m.pages)

	// Skip pages that shouldn't be visible in current context
	if !m.isPageVisible(m.currentPage) {
		m.currentPage = (m.currentPage + 1) % len(m.pages)
	}

	if m.isSetupModePage() {
		_ = m.ResetSetupModeCountdown()
	}

	return m.pages[m.currentPage].name
}

func (m *MenuSystem) PreviousMenuPage() string {
	m.currentPage = (m.currentPage - 1 + len(m.pages)) % len(m.pages)

	// Skip pages that shouldn't be visible in current context
	if !m.isPageVisible(m.currentPage) {
		m.currentPage = (m.currentPage - 1 + len(m.pages)) % len(m.pages)
	}

	if m.isSetupModePage() {
		_ = m.ResetSetupModeCountdown()
	}

	return m.pages[m.currentPage].name
}

func (m *MenuSystem) GetCurrentMenuPage() string {
	m.currentPage %= len(m.pages)

	return m.pages[m.currentPage].name
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

// SetDevToolsEnabledCallback sets the callback function to check if dev tools are enabled.
func (m *MenuSystem) SetDevToolsEnabledCallback(callback func() bool) {
	m.devToolsEnabled = callback
}

// isSetupModePage returns true if the current page is the setup page.
func (m *MenuSystem) isSetupModePage() bool {
	return m.pages[m.currentPage].name == "setupMode"
}

// isPageVisible checks if a page at the given index should be visible based on its context.
func (m *MenuSystem) isPageVisible(pageIndex int) bool {
	page := m.pages[pageIndex]

	switch page.context {
	case PageContextAlways:
		return true
	case PageContextDevTools:
		return m.isDevToolsEnabled()
	default:
		return true
	}
}

// isDevToolsEnabled returns true if dev tools are enabled.
func (m *MenuSystem) isDevToolsEnabled() bool {
	if m.devToolsEnabled == nil {
		return false
	}

	return m.devToolsEnabled()
}
