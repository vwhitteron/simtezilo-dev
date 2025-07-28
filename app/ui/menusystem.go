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
			// "jerkProfile",
			"vCurve",
			"vMax",
			// "snapProfile",
			"fCurve",
			"fMax",
			"minHz",
			"maxHz",
			"gCurve",
			"gMax",
			"cVol",
			"gVol",
			"mix",
		},
	}
}

func (m *MenuSystem) NextMenuPage() string {
	m.currentPage++
	if m.currentPage >= len(m.pages) {
		m.currentPage = 0
	}

	return m.pages[m.currentPage]
}

func (m *MenuSystem) PreviousMenuPage() string {
	m.currentPage--
	if m.currentPage < 0 {
		m.currentPage = len(m.pages) - 1
	}

	return m.pages[m.currentPage]
}

func (m *MenuSystem) GetCurrentMenuPage() string {
	if m.currentPage < 0 {
		m.currentPage = 0
	}

	if m.currentPage >= len(m.pages) {
		m.currentPage = len(m.pages) - 1
	}

	return m.pages[m.currentPage]
}
