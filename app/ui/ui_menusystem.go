package ui

import "github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"

const (
	setupModeCountdownStart = 5
	menuNodeRoot            = languagedb.Key("root")
)

// PageContext defines when a menu page should be visible.
type PageContext string

const (
	PageContextAlways   PageContext = "always"
	PageContextDevTools PageContext = "devtools"
)

// NodeType defines whether a menu node is a branch or leaf.
type NodeType int

const (
	NodeTypeBranch NodeType = iota
	NodeTypeLeaf
)

// NodeKind categorises a leaf for layout and navigation. It is independent of
// nodeType: an info page and a setting are both leaves but render differently
// and navigate differently. Branches ignore it.
type NodeKind int

const (
	KindSetting NodeKind = iota // editable leaf -> LayoutSetting (zero value)
	KindInfo                    // read-only info leaf -> LayoutInfo
	KindLive                    // live-view leaf
)

// MenuNode represents a node in the hierarchical menu tree.
type MenuNode struct {
	name     languagedb.Key
	nodeType NodeType
	kind     NodeKind
	context  PageContext
	children []*MenuNode
	parent   *MenuNode

	// noReturn, set on a branch during declaration, tells buildMenu not to inject
	// a Return child. Info pages use it because their leaves exit to the parent on
	// up/down rather than via a Return item.
	noReturn bool
}

type MenuSystem struct {
	root               *MenuNode
	currentNode        *MenuNode
	setupModeCountdown int
	devToolsEnabled    func() bool
}

// NewMenuSystem builds the menu tree (declared in newMenuTree) and starts the
// cursor on the live view leaf.
func NewMenuSystem() *MenuSystem {
	root := newMenuTree()

	return &MenuSystem{
		root:               root,
		currentNode:        find(root, languagedb.UIMenuLiveView), // Start on the live view leaf
		setupModeCountdown: setupModeCountdownStart,
	}
}

// NavigateLeft moves to the previous sibling or is used for value adjustment on leaves.
func (m *MenuSystem) NavigateLeft() (*MenuNode, string) {
	// All nodes navigate left to previous sibling
	return m.previousSibling(), "navigate"
}

// NavigateRight moves to the next sibling.
func (m *MenuSystem) NavigateRight() (*MenuNode, string) {
	// All nodes navigate right to next sibling
	return m.nextSibling(), "navigate"
}

// NavigateDown enters a branch node or toggles adjust mode on leaf nodes.
func (m *MenuSystem) NavigateDown() (*MenuNode, string) {
	// Special case: on "return" node, navigate up to parent (same as up button)
	if m.currentNode.name == languagedb.UIMenuReturn {
		if m.currentNode.parent != nil && m.currentNode.parent.name != menuNodeRoot {
			m.currentNode = m.currentNode.parent

			return m.currentNode, actionExit
		}
	}

	if m.currentNode.nodeType == NodeTypeBranch {
		// Enter the branch (move to first visible child)
		if len(m.currentNode.children) > 0 {
			firstVisible := m.getFirstVisibleChild(m.currentNode)
			if firstVisible != nil {
				m.currentNode = firstVisible

				return m.currentNode, actionEnter
			}
		}
	}

	// For leaves, decrease value (except info and live nodes which return to parent)
	if m.currentNode.nodeType == NodeTypeLeaf {
		if m.currentNode.parent != nil && (m.currentNode.kind == KindInfo || m.currentNode.kind == KindLive) {
			m.currentNode = m.currentNode.parent

			return m.currentNode, actionExit
		}

		return m.currentNode, actionDecrease
	}

	return m.currentNode, actionNone
}

// NavigateUp exits to parent on return node or increases value on regular leaves.
func (m *MenuSystem) NavigateUp() (*MenuNode, string) {
	// Special case: on "return" node, navigate up to parent
	if m.currentNode.name == languagedb.UIMenuReturn {
		if m.currentNode.parent != nil && m.currentNode.parent.name != menuNodeRoot {
			m.currentNode = m.currentNode.parent

			return m.currentNode, actionExit
		}
	}

	// Special case: on Live branch, enter the live view (same as down)
	if m.currentNode.name == languagedb.UIMenuLive && m.currentNode.nodeType == NodeTypeBranch {
		if len(m.currentNode.children) > 0 {
			firstVisible := m.getFirstVisibleChild(m.currentNode)
			if firstVisible != nil {
				m.currentNode = firstVisible

				return m.currentNode, actionEnter
			}
		}
	}

	// For leaves, increase value (except info and live nodes which return to parent)
	if m.currentNode.nodeType == NodeTypeLeaf {
		if m.currentNode.parent != nil && (m.currentNode.kind == KindInfo || m.currentNode.kind == KindLive) {
			m.currentNode = m.currentNode.parent

			return m.currentNode, actionExit
		}

		return m.currentNode, actionIncrease
	}

	// For branches, do nothing
	return m.currentNode, actionNone
}

// GetCurrentNode returns the current menu node.
func (m *MenuSystem) GetCurrentNode() *MenuNode {
	return m.currentNode
}

// GetCurrentMenuPage returns the name of the current menu node (for compatibility).
func (m *MenuSystem) GetCurrentMenuPage() languagedb.Key {
	return m.currentNode.name
}

// GetBreadcrumb returns the navigation path from root to current node.
func (m *MenuSystem) GetBreadcrumb() []string {
	path := make([]string, 0)
	node := m.currentNode

	for node != nil && node.name != menuNodeRoot {
		path = append([]string{string(node.name)}, path...)
		node = node.parent
	}

	return path
}

// IsCurrentNodeLeaf returns true if the current node is a leaf.
func (m *MenuSystem) IsCurrentNodeLeaf() bool {
	return m.currentNode.nodeType == NodeTypeLeaf
}

// IsCurrentNodeBranch returns true if the current node is a branch.
func (m *MenuSystem) IsCurrentNodeBranch() bool {
	return m.currentNode.nodeType == NodeTypeBranch
}

// IsCurrentNodeInfo reports whether the current node is a read-only info leaf.
func (m *MenuSystem) IsCurrentNodeInfo() bool {
	return m.currentNode.nodeType == NodeTypeLeaf && m.currentNode.kind == KindInfo
}

// IsCurrentNodeLive reports whether the current node is a live-view leaf.
func (m *MenuSystem) IsCurrentNodeLive() bool {
	return m.currentNode.nodeType == NodeTypeLeaf && m.currentNode.kind == KindLive
}

// NextMenuPage navigates right (for compatibility with existing code).
func (m *MenuSystem) NextMenuPage() languagedb.Key {
	node, _ := m.NavigateRight()

	return node.name
}

// PreviousMenuPage navigates left (for compatibility with existing code).
func (m *MenuSystem) PreviousMenuPage() languagedb.Key {
	node, _ := m.NavigateLeft()

	return node.name
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

// previousSibling navigates to the previous visible sibling with wrapping.
func (m *MenuSystem) previousSibling() *MenuNode {
	if m.currentNode.parent == nil {
		return m.currentNode
	}

	siblings := m.getVisibleChildren(m.currentNode.parent)
	if len(siblings) <= 1 {
		return m.currentNode
	}

	// Find current index
	currentIndex := -1

	for i, sibling := range siblings {
		if sibling == m.currentNode {
			currentIndex = i

			break
		}
	}

	if currentIndex == -1 {
		return m.currentNode
	}

	// Move to previous with wrapping
	prevIndex := (currentIndex - 1 + len(siblings)) % len(siblings)
	m.currentNode = siblings[prevIndex]

	return m.currentNode
}

// nextSibling navigates to the next visible sibling with wrapping.
func (m *MenuSystem) nextSibling() *MenuNode {
	if m.currentNode.parent == nil {
		return m.currentNode
	}

	siblings := m.getVisibleChildren(m.currentNode.parent)
	if len(siblings) <= 1 {
		return m.currentNode
	}

	// Find current index
	currentIndex := -1

	for i, sibling := range siblings {
		if sibling == m.currentNode {
			currentIndex = i

			break
		}
	}

	if currentIndex == -1 {
		return m.currentNode
	}

	// Move to next with wrapping
	nextIndex := (currentIndex + 1) % len(siblings)
	m.currentNode = siblings[nextIndex]

	return m.currentNode
}

// getVisibleChildren returns only the children that should be visible based on context.
func (m *MenuSystem) getVisibleChildren(node *MenuNode) []*MenuNode {
	visible := make([]*MenuNode, 0)

	for _, child := range node.children {
		if m.isNodeVisible(child) {
			visible = append(visible, child)
		}
	}

	return visible
}

// getFirstVisibleChild returns the first visible child of a node.
func (m *MenuSystem) getFirstVisibleChild(node *MenuNode) *MenuNode {
	for _, child := range node.children {
		if m.isNodeVisible(child) {
			return child
		}
	}

	return nil
}

// isNodeVisible checks if a node should be visible based on its context.
func (m *MenuSystem) isNodeVisible(node *MenuNode) bool {
	switch node.context {
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
