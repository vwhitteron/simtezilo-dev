package ui

type MenuSystem struct {
	currentPage int
	pages       []string
}

func NewMenuSystem() *MenuSystem {
	return &MenuSystem{
		currentPage: 0,
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
		},
	}
}

func (m *MenuSystem) NextMenuPage() string {
	m.currentPage = (m.currentPage + 1) % len(m.pages)

	return m.pages[m.currentPage]
}

func (m *MenuSystem) PreviousMenuPage() string {
	m.currentPage = (m.currentPage - 1 + len(m.pages)) % len(m.pages)

	return m.pages[m.currentPage]
}

func (m *MenuSystem) GetCurrentMenuPage() string {
	m.currentPage %= len(m.pages)

	return m.pages[m.currentPage]
}
